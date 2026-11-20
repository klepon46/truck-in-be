package movement

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

func serviceSchemaTestTransaction(t *testing.T, versions ...int) *sqlx.Tx {
	t.Helper()
	dsn := os.Getenv("UNITLOG_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires disposable local unitlog_eligibility_test database")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") || parsed.Path != "/unitlog_eligibility_test" {
		t.Fatal("only disposable local unitlog_eligibility_test is allowed")
	}
	db, err := sqlx.Open("postgres", dsn)
	if err != nil {
		t.Fatal("open disposable PostgreSQL")
	}
	t.Cleanup(func() { db.Close() })
	tx, err := db.BeginTxx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tx.Rollback() })
	paths, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	if err != nil || len(paths) == 0 {
		t.Fatal("migrations unavailable")
	}
	version := 6 // Keep the version-6 schema/audit regressions testing that contract.
	if len(versions) > 0 {
		version = versions[0]
	}
	for _, path := range paths {
		number, err := strconv.Atoi(filepath.Base(path)[:6])
		if err != nil {
			t.Fatal(err)
		}
		if number > version {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO unit_snapshot (lambung_unit_id,no_lambung,no_polisi,synced_at) VALUES (1,'SAMT119','DA 1234 AB',NOW())`); err != nil {
		t.Fatal(err)
	}
	return tx
}

func TestSPPServiceMigrationPostgres(t *testing.T) {
	tx := serviceSchemaTestTransaction(t)
	out := sppFixtureMovement(t, tx, DirectionOut, "", OutFillingShed, "SPP-001", "C400")
	arrival := sppFixtureMovement(t, tx, DirectionIn, InServiceAndRepair, "", "SPP-001", "C400")
	sppFixtureCycle(t, tx, out, arrival)
	if _, err := tx.Exec(`UPDATE unit_snapshot SET current_transaction_id=$1 WHERE lambung_unit_id=1`, arrival); err != nil {
		t.Fatal(err)
	}
	resumed := sppFixtureMovement(t, tx, DirectionOut, "", OutFillingShed, "SPP-001", "C400")
	var source int64
	if err := tx.Get(&source, `SELECT resumed_from_service_transaction_id FROM movement_transactions WHERE transaction_id=$1`, resumed); err != nil || source != arrival {
		t.Fatalf("resumption must reference the actual Service return: %v", err)
	}
	var category string
	if err := tx.Get(&category, `SELECT in_category FROM movement_transactions WHERE transaction_id=$1`, arrival); err != nil || category != InServiceAndRepair {
		t.Fatal("must preserve historical Service category")
	}
	assertServiceSQLRejected(t, tx, "23505", `INSERT INTO movement_transactions OVERRIDING SYSTEM VALUE
	 SELECT (jsonb_populate_record(NULL::movement_transactions,
	     to_jsonb(m) || jsonb_build_object('transaction_id', transaction_id+100,
	     'idempotency_key',gen_random_uuid()))).* FROM movement_transactions m WHERE transaction_id=$1`, resumed)
	assertServiceSQLRejected(t, tx, "P0001", `UPDATE movement_cycles SET completed_at=NOW() WHERE in_transaction_id=$1`, arrival)
	assertServiceSQLRejected(t, tx, "P0001", `DELETE FROM movement_cycles WHERE in_transaction_id=$1`, arrival)
	assertServiceSQLRejected(t, tx, "P0001", `UPDATE movement_transactions SET in_category='WAITING_FOR_ASSIGNMENT' WHERE transaction_id=$1`, arrival)
	down, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000006_add_spp_service_continuations.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	assertServiceSQLRejected(t, tx, "P0001", string(down))
}

func assertServiceSQLRejected(t *testing.T, tx *sqlx.Tx, code string, query string, args ...any) {
	t.Helper()
	if _, err := tx.Exec(`SAVEPOINT rejected_statement`); err != nil {
		t.Fatal(err)
	}
	_, err := tx.Exec(query, args...)
	pgErr, ok := err.(*pq.Error)
	if !ok || string(pgErr.Code) != code {
		t.Fatalf("expected PostgreSQL %s, got %v", code, err)
	}
	if _, err := tx.Exec(`ROLLBACK TO SAVEPOINT rejected_statement`); err != nil {
		t.Fatal(err)
	}
}

func TestSPPServiceMigrationRejectsForgedSourcesPostgres(t *testing.T) {
	tx := serviceSchemaTestTransaction(t)
	if _, err := tx.Exec(`INSERT INTO unit_snapshot(lambung_unit_id,no_lambung,no_polisi,synced_at) VALUES(2,'SAMT120','MASTER TWO',NOW())`); err != nil {
		t.Fatal(err)
	}
	out := sppFixtureMovement(t, tx, DirectionOut, "", OutFillingShed, "SPP-001", "C400")
	arrival := sppFixtureMovement(t, tx, DirectionIn, InServiceAndRepair, "", "SPP-001", "C400")
	sppFixtureCycle(t, tx, out, arrival)
	if _, err := tx.Exec(`UPDATE unit_snapshot SET current_transaction_id=$1 WHERE lambung_unit_id=1`, arrival); err != nil {
		t.Fatal(err)
	}
	// Clone insert shapes without bypassing immutable history or the trigger.
	const clone = `INSERT INTO movement_transactions OVERRIDING SYSTEM VALUE
	 SELECT (jsonb_populate_record(NULL::movement_transactions,
	   to_jsonb(m) || $2::jsonb || jsonb_build_object('transaction_id',transaction_id+100,
	   'idempotency_key',gen_random_uuid(),'resumed_from_service_transaction_id',$3::bigint))).*
	 FROM movement_transactions m WHERE transaction_id=$1`
	for _, shape := range []string{
		`{"direction":"IN","in_category":"SERVICE_AND_REPAIR_MAINTENANCE","out_destination":null}`,
		`{"direction":"OUT","out_destination":"BENGKEL_LUAR","spp_number":null,"work_order_number":"WO-1"}`,
		`{"direction":"OUT","out_destination":"OTHER","spp_number":null,"driver_id":42,"note":"test"}`,
		`{"customer_id":"OTHER"}`,
		`{"spp_number":"OTHER"}`,
		`{"driver_id":42}`,
		`{"lambung_unit_id":2,"no_lambung_snapshot":"SAMT120"}`,
	} {
		assertServiceSQLRejected(t, tx, "23514", clone, out, shape, arrival)
	}
	assertServiceSQLRejected(t, tx, "23514", clone, out, `{}`, out)
	// No current Service correlation: the original normal uniqueness still applies.
	if _, err := tx.Exec(`UPDATE unit_snapshot SET current_transaction_id=NULL WHERE lambung_unit_id=1`); err != nil {
		t.Fatal(err)
	}
	assertServiceSQLRejected(t, tx, "23505", clone, out, `{}`, nil)
	assertServiceSQLRejected(t, tx, "23514", clone, out, `{}`, arrival)
}

func TestSPPServiceMigrationEmptyDowngradePostgres(t *testing.T) {
	tx := serviceSchemaTestTransaction(t)
	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000006_add_spp_service_continuations.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	out := sppFixtureMovement(t, tx, DirectionOut, "", OutFillingShed, "SPP-001", "C400")
	assertServiceSQLRejected(t, tx, "23505", `INSERT INTO movement_transactions OVERRIDING SYSTEM VALUE
	 SELECT (jsonb_populate_record(NULL::movement_transactions,
	     to_jsonb(m) || jsonb_build_object('transaction_id', transaction_id+100,
	     'idempotency_key',gen_random_uuid()))).* FROM movement_transactions m WHERE transaction_id=$1`, out)
}

func TestSPPServiceMigrationPreservesExistingServicePostgres(t *testing.T) {
	tx := serviceSchemaTestTransaction(t)
	down, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000006_add_spp_service_continuations.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(string(down)); err != nil {
		t.Fatal(err)
	}
	out := sppFixtureMovement(t, tx, DirectionOut, "", OutFillingShed, "SPP-001", "C400")
	arrival := sppFixtureMovement(t, tx, DirectionIn, InServiceAndRepair, "", "", "")
	sppFixtureCycle(t, tx, out, arrival)
	if _, err := tx.Exec(`UPDATE unit_snapshot SET current_transaction_id=$1 WHERE lambung_unit_id=1`, arrival); err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000006_add_spp_service_continuations.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(string(up)); err != nil {
		t.Fatal(err)
	}
	var unchanged bool
	if err := tx.Get(&unchanged, `SELECT COUNT(*)=2 AND bool_and(resumed_from_service_transaction_id IS NULL) FROM movement_transactions`); err != nil || !unchanged {
		t.Fatal("upgrade must not backfill or rewrite existing history")
	}
	resumed := sppFixtureMovement(t, tx, DirectionOut, "", OutCustomer, "SPP-001", "C400")
	var source int64
	if err := tx.Get(&source, `SELECT resumed_from_service_transaction_id FROM movement_transactions WHERE transaction_id=$1`, resumed); err != nil || source != arrival {
		t.Fatal("trustworthy pre-migration Service must remain resumable")
	}
	assertServiceSQLRejected(t, tx, "P0001", string(down))
}
