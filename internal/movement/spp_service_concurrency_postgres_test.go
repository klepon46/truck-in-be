package movement

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func serviceTestRecord(t *testing.T, s *Service, key string, input Input) Transaction {
	t.Helper()
	row, err := s.Record(context.Background(), key, input, "actor", "Satpam")
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func serviceTestArrival(t *testing.T, db *sqlx.DB) (*Service, Transaction) {
	t.Helper()
	s := NewService(db, nil, canonicalDocuments{number: "SPP-001"})
	serviceTestRecord(t, s, "96942009-f8c4-4dbf-b7e0-000000000001", Input{NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: OutFillingShed, SPPNumber: "100-Owner"})
	arrival := serviceTestRecord(t, s, "96942009-f8c4-4dbf-b7e0-000000000002", Input{NoLambung: "SAMT119", Direction: DirectionIn, InCategory: InServiceAndRepair, SPPNumber: "100-Owner"})
	return s, arrival
}

func TestSPPServicePostgresConcurrentUnitDepartures(t *testing.T) {
	for _, sameKey := range []bool{false, true} {
		t.Run(map[bool]string{false: "different keys", true: "same key replay"}[sameKey], func(t *testing.T) {
			db := serviceLifecycleDB(t)
			s, arrival := serviceTestArrival(t, db)
			input := Input{NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: OutFillingShed, SPPNumber: "200-Owner", Coordinates: "0,0"}
			type result struct {
				row Transaction
				err error
			}
			start := make(chan struct{})
			results := make(chan result, 2)
			for _, key := range []string{"96942009-f8c4-4dbf-b7e0-000000000003", "96942009-f8c4-4dbf-b7e0-000000000004"} {
				if sameKey {
					key = "96942009-f8c4-4dbf-b7e0-000000000003"
				}
				go func(key string) {
					<-start
					row, err := s.Record(context.Background(), key, input, "actor", "Satpam")
					results <- result{row, err}
				}(key)
			}
			close(start)
			a, b := <-results, <-results
			if sameKey {
				if a.err != nil || b.err != nil || a.row.ID != b.row.ID {
					t.Fatalf("same-key concurrency must replay one row: %v / %v", a.err, b.err)
				}
			} else if !((a.err == nil && errors.Is(b.err, ErrInvalidTransition)) || (b.err == nil && errors.Is(a.err, ErrInvalidTransition))) {
				t.Fatalf("exactly one departure should win: %v / %v", a.err, b.err)
			}
			var consumed int
			if err := db.Get(&consumed, `SELECT COUNT(*) FROM movement_transactions WHERE resumed_from_service_transaction_id=$1`, arrival.ID); err != nil || consumed != 1 {
				t.Fatal("concurrent requests consumed one Service source more than once")
			}
		})
	}
}

func waitForSPPLock(t *testing.T, db *sqlx.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		var waiting bool
		err := db.GetContext(ctx, &waiting, `SELECT EXISTS (
		 SELECT 1 FROM pg_stat_activity WHERE datname=current_database()
		 AND wait_event='advisory' AND query='SELECT pg_advisory_xact_lock(8105, hashtext($1))')`)
		if err != nil {
			t.Fatalf("submit did not wait on the canonical-SPP lock before eligibility: %v", err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("submit never waited on the canonical-SPP lock")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestSPPReusePostgresForeignUnitWhileWaiting(t *testing.T) {
	db := serviceLifecycleDB(t)
	s := NewService(db, nil, canonicalDocuments{number: "SPP-001"})
	terminal, err := db.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Rollback()
	// Another unit's first use remains uncommitted while this request waits.
	if _, err := terminal.Exec(`SELECT 1 FROM unit_snapshot WHERE lambung_unit_id=8 FOR UPDATE;
		SELECT pg_advisory_xact_lock(8105,hashtext('SPP-001'))`); err != nil {
		t.Fatal(err)
	}
	var departure, arrival int64
	if err := terminal.Get(&departure, `INSERT INTO movement_transactions
		(idempotency_key,request_hash,lambung_unit_id,no_lambung_snapshot,no_polisi_snapshot,direction,out_destination,spp_number,customer_id,driver_name_snapshot,actor_id,actor_name_snapshot)
		VALUES(gen_random_uuid(),decode('00','hex'),8,'SAMT120','MASTER TWO','OUT','CUSTOMER','SPP-001','CUST-1','Driver','actor','Satpam') RETURNING transaction_id`); err != nil {
		t.Fatal(err)
	}
	if err := terminal.Get(&arrival, `INSERT INTO movement_transactions
		(idempotency_key,request_hash,lambung_unit_id,no_lambung_snapshot,no_polisi_snapshot,direction,in_category,spp_number,customer_id,driver_name_snapshot,actor_id,actor_name_snapshot)
		VALUES(gen_random_uuid(),decode('00','hex'),8,'SAMT120','MASTER TWO','IN','WAITING_FOR_ASSIGNMENT','SPP-001','CUST-1','Driver','actor','Satpam') RETURNING transaction_id`); err != nil {
		t.Fatal(err)
	}
	if _, err := terminal.Exec(`INSERT INTO movement_cycles(lambung_unit_id,out_transaction_id,in_transaction_id,completed_at) VALUES(8,$1,$2,NOW())`, departure, arrival); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := s.Record(ctx, "96942009-f8c4-4dbf-b7e0-000000000003", Input{NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: OutFillingShed, SPPNumber: "200-Owner"}, "actor", "Satpam")
		done <- err
	}()
	waitForSPPLock(t, db)
	if err := terminal.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrSPPUsed) {
		t.Fatalf("fresh policy must see SPP history committed by another unit: %v", err)
	}
	var count int
	if err := db.Get(&count, `SELECT COUNT(*) FROM movement_transactions WHERE resumed_from_service_transaction_id IS NOT NULL`); err != nil || count != 0 {
		t.Fatal("terminal race created a continuation")
	}
}

func TestSPPServicePostgresReturnAlsoLocksCanonicalSPP(t *testing.T) {
	db := serviceLifecycleDB(t)
	s := NewService(db, nil, canonicalDocuments{number: "SPP-001"})
	serviceTestRecord(t, s, "96942009-f8c4-4dbf-b7e0-000000000001", Input{NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: OutFillingShed, SPPNumber: "100-Owner"})
	holder, err := db.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback()
	if _, err := holder.Exec(`SELECT pg_advisory_xact_lock(8105,hashtext('SPP-001'))`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := s.Record(ctx, "96942009-f8c4-4dbf-b7e0-000000000002", Input{NoLambung: "SAMT119", Direction: DirectionIn, InCategory: InServiceAndRepair, SPPNumber: "200-Owner"}, "actor", "Satpam")
		done <- err
	}()
	waitForSPPLock(t, db)
	if err := holder.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("return failed after canonical lock released: %v", err)
	}
}

func TestSPPReusePostgresDowngradeWaitsForCustomerAudit(t *testing.T) {
	db := serviceLifecycleDB(t, 6)
	source := legacyServiceArrival(t, db)
	up, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000007_allow_same_unit_spp_reuse.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(up)); err != nil {
		t.Fatal(err)
	}
	writer, err := db.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback()
	var link int64
	if err := writer.Get(&link, `INSERT INTO movement_transactions
	 (idempotency_key,request_hash,lambung_unit_id,no_lambung_snapshot,no_polisi_snapshot,direction,out_destination,spp_number,customer_id,driver_name_snapshot,actor_id,actor_name_snapshot)
	 VALUES(gen_random_uuid(),decode('00','hex'),7,'SAMT119','MASTER PLATE','OUT','CUSTOMER','SPP-001','CUST-1','Driver','actor','Satpam')
	 RETURNING resumed_from_service_transaction_id`); err != nil || link != source {
		t.Fatalf("Customer resumption did not derive source: %v", err)
	}
	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000007_allow_same_unit_spp_reuse.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		tx, err := db.BeginTxx(ctx, nil)
		if err != nil {
			done <- err
			return
		}
		defer tx.Rollback()
		_, err = tx.ExecContext(ctx, string(body))
		done <- err
	}()
	for {
		var waiting bool
		if err := db.GetContext(ctx, &waiting, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE relation='movement_transactions'::regclass AND mode='AccessExclusiveLock' AND NOT granted)`); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("downgrade did not wait for writer")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if err := writer.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("downgrade discarded a Customer continuation committed while it waited")
	}
	var retained int64
	if err := db.Get(&retained, `SELECT resumed_from_service_transaction_id FROM movement_transactions WHERE resumed_from_service_transaction_id=$1`, source); err != nil || retained != source {
		t.Fatal("failed downgrade must retain audit metadata")
	}
}
