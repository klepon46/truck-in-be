package movement

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// Each test owns a generated schema, only within this explicitly disposable DB.
func serviceLifecycleDB(t *testing.T, versions ...int) *sqlx.DB {
	t.Helper()
	dsn := os.Getenv("UNITLOG_SERVICE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires disposable local unitlog_service_test database")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") || u.Path != "/unitlog_service_test" {
		t.Fatal("only disposable local unitlog_service_test is allowed")
	}
	admin, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("service_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE SCHEMA ` + pq.QuoteIdentifier(schema)); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sqlx.Connect("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, err := admin.Exec(`DROP SCHEMA ` + pq.QuoteIdentifier(schema) + ` CASCADE`)
		admin.Close()
		if err != nil {
			t.Error(err)
		}
	})
	paths, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	if err != nil || len(paths) == 0 {
		t.Fatal("migrations unavailable")
	}
	for _, path := range paths {
		if len(versions) > 0 && filepath.Base(path)[:6] > fmt.Sprintf("%06d", versions[0]) {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO unit_snapshot (lambung_unit_id,no_lambung,no_polisi,synced_at)
		VALUES (7,'SAMT119','MASTER PLATE',NOW()),(8,'SAMT120','MASTER TWO',NOW())`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSPPServicePostgresApprovedLifecycle(t *testing.T) {
	db := serviceLifecycleDB(t)
	ctx := context.Background()
	s := NewService(db, nil, canonicalDocuments{number: "SPP-001"})
	inputs := []Input{
		{Direction: DirectionOut, OutDestination: OutFillingShed},
		{Direction: DirectionIn, InCategory: InServiceAndRepair},
		{Direction: DirectionOut, OutDestination: OutFillingShed},
		{Direction: DirectionIn, InCategory: InServiceAndRepair},
		{Direction: DirectionOut, OutDestination: OutCustomer},
		{Direction: DirectionIn, InCategory: InServiceAndRepair},
		{Direction: DirectionOut, OutDestination: OutFillingShed},
		{Direction: DirectionIn, InCategory: InWaitingForDelivery},
		{Direction: DirectionOut, OutDestination: OutCustomer},
		{Direction: DirectionIn, InCategory: InWaitingForAssignment},
	}
	var priorRows string
	var previous Transaction
	for i, input := range inputs {
		key := fmt.Sprintf("96942009-f8c4-4dbf-b7e0-%012d", i)
		input.NoLambung, input.SPPNumber, input.Coordinates = "SAMT119", fmt.Sprintf("%d-Owner", i+100), "-3,114"
		lookup, err := s.LookupSPP(ctx, input.SPPNumber)
		if err != nil {
			t.Fatal(err)
		}
		if input.Direction == DirectionOut {
			want := []string{OutCustomer, OutFillingShed}
			if i == 0 {
				want = []string{OutFillingShed}
			}
			if !reflect.DeepEqual(lookup.AllowedOutDestinations, want) {
				t.Fatalf("step %d destinations %v, want %v", i, lookup.AllowedOutDestinations, want)
			}
		}
		row, err := s.Record(ctx, key, input, "actor", "Satpam")
		if err != nil || row.SPPNumber == nil || *row.SPPNumber != "SPP-001" || row.NoPolisi != "MASTER PLATE" {
			t.Fatalf("step %d failed: %v", i, err)
		}
		if input.Direction == DirectionIn && (row.CycleID == nil || previous.CycleID == nil || *row.CycleID != *previous.CycleID) {
			t.Fatalf("step %d did not close its own cycle", i)
		}
		var source *int64
		if err := db.Get(&source, `SELECT resumed_from_service_transaction_id FROM movement_transactions WHERE transaction_id=$1`, row.ID); err != nil {
			t.Fatal(err)
		}
		if i == 2 || i == 4 || i == 6 {
			if source == nil || *source != previous.ID {
				t.Fatalf("step %d must consume exactly its current Service arrival", i)
			}
		} else if source != nil {
			t.Fatalf("step %d must not have a Service marker", i)
		}
		var hash []byte
		wantHash, _ := requestHash(input)
		if err := db.Get(&hash, `SELECT request_hash FROM movement_transactions WHERE transaction_id=$1`, row.ID); err != nil || !bytes.Equal(hash, wantHash) {
			t.Fatal("must retain original reference/coordinates in request hash")
		}
		replay, err := NewService(db, nil, nil).Record(ctx, key, input, "actor", "Satpam")
		if err != nil || replay.ID != row.ID || replay.Coordinates == nil || *replay.Coordinates != "-3,114" {
			t.Fatalf("step %d replay failed: %v", i, err)
		}
		var unchanged string
		if err := db.Get(&unchanged, `SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY transaction_id),'[]'::jsonb)::text FROM movement_transactions m WHERE transaction_id<$1`, row.ID); err != nil || (i > 0 && unchanged != priorRows) {
			t.Fatal("earlier immutable transaction snapshots changed")
		}
		if err := db.Get(&priorRows, `SELECT jsonb_agg(to_jsonb(m) ORDER BY transaction_id)::text FROM movement_transactions m`); err != nil {
			t.Fatal(err)
		}
		previous = row
	}
	lookup, err := s.LookupSPP(ctx, "999-Owner")
	if err != nil || len(lookup.AllowedOutDestinations) != 2 {
		t.Fatalf("Kosongan must permit both same-unit SPP destinations: %v", err)
	}
	_, err = NewService(db, nil, returningDocuments{spp: "SPP-001", unit: "SAMT120"}).Record(ctx, "96942009-f8c4-4dbf-b7e0-999999999999", Input{NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: OutFillingShed, SPPNumber: "999-Owner"}, "actor", "Satpam")
	if !errors.Is(err, ErrExternalDocument) {
		t.Fatalf("reuse must still reject a different upstream unit: %v", err)
	}
	var cycles, durations, movements int
	db.Get(&cycles, `SELECT COUNT(*) FROM movement_cycles WHERE completed_at IS NOT NULL`)
	db.Get(&durations, `SELECT COUNT(*) FROM movement_status_durations WHERE duration_seconds>=0`)
	db.Get(&movements, `SELECT COUNT(*) FROM movement_transactions`)
	if cycles != 5 || durations != 9 || movements != 10 {
		t.Fatalf("incorrect audit counts %d/%d/%d", cycles, durations, movements)
	}
	fresh := NewService(db, nil, canonicalDocuments{number: "NEW-SPP"})
	if _, err := fresh.Record(ctx, "96942009-f8c4-4dbf-b7e0-999999999998", Input{NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: OutFillingShed, SPPNumber: "888-Owner"}, "actor", "Satpam"); err != nil {
		t.Fatalf("new unused SPP must still work: %v", err)
	}
}

func TestSPPServicePostgresFailureRollsBackConsumption(t *testing.T) {
	db := serviceLifecycleDB(t)
	s, source := serviceTestArrival(t, db)
	// Fail after the movement INSERT/derived marker, at the actual cycle boundary.
	if _, err := db.Exec(`CREATE FUNCTION fail_test_cycle() RETURNS TRIGGER LANGUAGE plpgsql AS $$
	 BEGIN RAISE EXCEPTION 'test cycle failure'; END; $$;
	 CREATE TRIGGER fail_test_cycle BEFORE INSERT ON movement_cycles FOR EACH ROW EXECUTE FUNCTION fail_test_cycle()`); err != nil {
		t.Fatal(err)
	}
	key := "96942009-f8c4-4dbf-b7e0-000000000003"
	input := Input{NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: OutFillingShed, SPPNumber: "200-Owner"}
	if _, err := s.Record(context.Background(), key, input, "actor", "Satpam"); err == nil {
		t.Fatal("cycle failure was not returned")
	}
	var movements, durations, consumed int
	var current int64
	if err := db.Get(&movements, `SELECT COUNT(*) FROM movement_transactions`); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&durations, `SELECT COUNT(*) FROM movement_status_durations`); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&consumed, `SELECT COUNT(*) FROM movement_transactions WHERE resumed_from_service_transaction_id=$1`, source.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&current, `SELECT current_transaction_id FROM unit_snapshot WHERE lambung_unit_id=7`); err != nil {
		t.Fatal(err)
	}
	if movements != 2 || durations != 1 || consumed != 0 || current != source.ID {
		t.Fatal("failed continuation partially consumed source/audit/duration/status")
	}
	if _, err := db.Exec(`DROP TRIGGER fail_test_cycle ON movement_cycles; DROP FUNCTION fail_test_cycle()`); err != nil {
		t.Fatal(err)
	}
	serviceTestRecord(t, s, key, input) // Same key can now commit the previously rolled-back attempt.
}
