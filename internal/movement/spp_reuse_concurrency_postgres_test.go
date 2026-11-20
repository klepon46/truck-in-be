package movement

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

func legacyServiceArrival(t *testing.T, db *sqlx.DB) int64 {
	t.Helper()
	tx, err := db.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var out, in int64
	const insert = `INSERT INTO movement_transactions
	 (idempotency_key,request_hash,lambung_unit_id,no_lambung_snapshot,no_polisi_snapshot,direction,in_category,out_destination,spp_number,customer_id,driver_name_snapshot,actor_id,actor_name_snapshot)
	 VALUES(gen_random_uuid(),decode('00','hex'),7,'SAMT119','MASTER PLATE',$1,NULLIF($2,''),NULLIF($3,''),'SPP-001','CUST-1','Driver','actor','Satpam')
	 RETURNING transaction_id`
	if err := tx.Get(&out, insert, "OUT", "", OutFillingShed); err != nil {
		t.Fatal(err)
	}
	if err := tx.Get(&in, insert, "IN", InServiceAndRepair, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO movement_cycles(lambung_unit_id,out_transaction_id,in_transaction_id,completed_at) VALUES(7,$1,$2,NOW())`, out, in); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE unit_snapshot SET current_transaction_id=$1 WHERE lambung_unit_id=7`, in); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return in
}

func TestReusableSPPPostgresSQLClaimRaceRejectsOtherUnit(t *testing.T) {
	db := serviceLifecycleDB(t)
	first, err := db.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Rollback()
	const insert = `INSERT INTO movement_transactions
	 (idempotency_key,request_hash,lambung_unit_id,no_lambung_snapshot,no_polisi_snapshot,direction,out_destination,spp_number,customer_id,driver_name_snapshot,actor_id,actor_name_snapshot)
	 SELECT gen_random_uuid(),decode('00','hex'),lambung_unit_id,no_lambung,no_polisi,'OUT','FILLING_SHED_KUIN','SHARED-SPP','C400','Driver','actor','Satpam'
	 FROM unit_snapshot WHERE lambung_unit_id=$1 RETURNING transaction_id`
	var firstID int64
	if err := first.Get(&firstID, insert, 7); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		var secondID int64
		done <- db.GetContext(ctx, &secondID, insert, 8)
	}()
	for {
		var waiting bool
		if err := db.GetContext(ctx, &waiting, `SELECT EXISTS(
		 SELECT 1 FROM pg_stat_activity WHERE datname=current_database()
		 AND wait_event='advisory' AND query LIKE '%SHARED-SPP%')`); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("second unit's SQL INSERT did not wait on the SPP lock")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	err = <-done
	var pgErr *pq.Error
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.Constraint != "movement_transactions_spp_unit_check" {
		t.Fatalf("SQL trigger did not observe the other unit's committed claim after waiting: %v", err)
	}
	var count int
	if err := db.Get(&count, `SELECT COUNT(*) FROM movement_transactions WHERE spp_number='SHARED-SPP'`); err != nil || count != 1 {
		t.Fatal("SQL claim race created SPP history on two units")
	}
}

func TestReusableSPPPostgresStandaloneMigrationBatches(t *testing.T) {
	db := serviceLifecycleDB(t)
	read := func(direction string) string {
		body, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000007_allow_same_unit_spp_reuse."+direction+".sql"))
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	down, up := read("down"), read("up")
	// Match the runner's PostgreSQL multi-statement Exec boundary, without a
	// surrounding sqlx transaction supplied by the test.
	if _, err := db.Exec(down); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(up); err != nil {
		t.Fatal(err)
	}
	s := NewService(db, nil, returningDocuments{spp: "SPP-A"})
	out := serviceTestRecord(t, s, "96942009-f8c4-4dbf-b7e0-000000000001", Input{NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: OutFillingShed, SPPNumber: "100-Owner"})
	if _, err := db.Exec(down); err == nil {
		t.Fatal("standalone downgrade discarded policy audit")
	}
	var version int
	if err := db.Get(&version, `SELECT spp_departure_policy_version FROM movement_transactions WHERE transaction_id=$1`, out.ID); err != nil || version != 7 {
		t.Fatal("failed standalone downgrade did not retain its whole schema/audit")
	}
}
