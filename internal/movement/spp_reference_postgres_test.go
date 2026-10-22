package movement

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
)

type canonicalDocuments struct {
	testDocuments
	number string
}

func (d canonicalDocuments) SPP(ctx context.Context, _ string) (Document, error) {
	document, err := d.testDocuments.SPP(ctx, "")
	document.SPPNumber = d.number
	return document, err
}

// This test commits fixtures only inside a dedicated disposable local database.
func TestSPPReferencePostgresLifecycleAndReplay(t *testing.T) {
	dsn := os.Getenv("UNITLOG_REFERENCE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires a disposable local unitlog_reference_test PostgreSQL database")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") || parsed.Path != "/unitlog_reference_test" {
		t.Fatal("only the disposable local unitlog_reference_test database is allowed")
	}
	db, err := sqlx.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var exists bool
	if err := db.Get(&exists, `SELECT to_regclass('unit_snapshot') IS NOT NULL`); err != nil || exists {
		t.Fatal("reference lifecycle test requires a fresh empty disposable database")
	}
	paths, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	if err != nil || len(paths) == 0 {
		t.Fatal("migrations unavailable")
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO unit_snapshot (lambung_unit_id,no_lambung,no_polisi,synced_at) VALUES (7,'SAMT119','DA 1234 AB',NOW())`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	service := NewService(db, nil, testDocuments{})
	const legacyKey = "96942009-f8c4-4dbf-b7e0-6d5717651660"
	legacyInput := Input{NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: OutFillingShed, SPPNumber: "SPP-001"}
	legacyHash, err := requestHash(legacyInput)
	if err != nil {
		t.Fatal(err)
	}
	// Model a pre-cutover open departure, without modifying any existing audit row.
	var departureID, cycleID int64
	if err := db.Get(&departureID, `INSERT INTO movement_transactions (
	 idempotency_key,request_hash,lambung_unit_id,no_lambung_snapshot,no_polisi_snapshot,
	 direction,out_destination,spp_number,driver_name_snapshot,customer_id,actor_id,actor_name_snapshot
	) VALUES ($1,$2,7,'SAMT119','DA 1234 AB','OUT','FILLING_SHED_KUIN','SPP-001','Driver','CUST-1','actor','Satpam') RETURNING transaction_id`, legacyKey, legacyHash); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&cycleID, `INSERT INTO movement_cycles (lambung_unit_id,out_transaction_id) VALUES (7,$1) RETURNING cycle_id`, departureID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE unit_snapshot SET current_transaction_id=$1 WHERE lambung_unit_id=7`, departureID); err != nil {
		t.Fatal(err)
	}
	legacyReplay, err := NewService(db, nil, nil).Record(ctx, legacyKey, legacyInput, "actor", "Satpam")
	if err != nil || legacyReplay.ID != departureID {
		t.Fatalf("legacy replay must not depend on new reference parsing: %v", err)
	}
	wrong := NewService(db, nil, canonicalDocuments{number: "OTHER-SPP"})
	arrival := Input{NoLambung: "SAMT119", Direction: DirectionIn, InCategory: InWaitingForDelivery, SPPNumber: "123-example", Coordinates: "-3,114"}
	if _, err := wrong.Record(ctx, "96942009-f8c4-4dbf-b7e0-6d5717651661", arrival, "actor", "Satpam"); !errors.Is(err, ErrReturnDocumentMismatch) {
		t.Fatalf("different resolved SPP must not close the legacy cycle: %v", err)
	}
	firstIn, err := service.Record(ctx, "96942009-f8c4-4dbf-b7e0-6d5717651661", arrival, "actor", "Satpam")
	if err != nil || firstIn.CycleID == nil || *firstIn.CycleID != cycleID || firstIn.SPPNumber == nil || *firstIn.SPPNumber != "SPP-001" {
		t.Fatalf("reference must close the original canonical cycle: %v", err)
	}
	lookup, err := service.LookupSPP(ctx, "456-Another-Owner")
	if err != nil || lookup.SPPNumber != "456-Another-Owner" || len(lookup.AllowedOutDestinations) != 1 || lookup.AllowedOutDestinations[0] != OutCustomer {
		t.Fatalf("alias lookup must use canonical eligibility but echo reference: %v", err)
	}
	departure := Input{NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: OutCustomer, SPPNumber: "456-Another-Owner", Coordinates: "0,0"}
	out, err := service.Record(ctx, "96942009-f8c4-4dbf-b7e0-6d5717651662", departure, "actor", "Satpam")
	if err != nil || out.SPPNumber == nil || *out.SPPNumber != "SPP-001" {
		t.Fatalf("Customer departure must preserve canonical identity: %v", err)
	}
	var storedHash []byte
	if err := db.Get(&storedHash, `SELECT request_hash FROM movement_transactions WHERE transaction_id=$1`, out.ID); err != nil {
		t.Fatal(err)
	}
	wantHash, err := requestHash(departure)
	if err != nil || !bytes.Equal(storedHash, wantHash) || departure.SPPNumber != "456-Another-Owner" {
		t.Fatal("hash and caller payload must retain original reference")
	}
	replay, err := NewService(db, nil, nil).Record(ctx, "96942009-f8c4-4dbf-b7e0-6d5717651662", departure, "actor", "Satpam")
	if err != nil || replay.ID != out.ID || replay.Coordinates == nil || *replay.Coordinates != "0,0" {
		t.Fatalf("reference replay must preserve original coordinates without upstream: %v", err)
	}
	changed := departure
	changed.SPPNumber = "789-example"
	if _, err := service.Record(ctx, "96942009-f8c4-4dbf-b7e0-6d5717651662", changed, "actor", "Satpam"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("different reference with the same key must conflict even for the same canonical document: %v", err)
	}
	arrival.InCategory, arrival.SPPNumber = InWaitingForAssignment, "789-example"
	finalIn, err := service.Record(ctx, "96942009-f8c4-4dbf-b7e0-6d5717651663", arrival, "actor", "Satpam")
	if err != nil || finalIn.CycleID == nil || out.CycleID == nil || *finalIn.CycleID != *out.CycleID {
		t.Fatalf("final returning IN must close Customer cycle: %v", err)
	}
	lookup, err = service.LookupSPP(ctx, "999-example")
	if err != nil || len(lookup.AllowedOutDestinations) != 0 {
		t.Fatalf("another reference must not revive a completed SPP: %v", err)
	}
	departure.SPPNumber, departure.OutDestination = "999-example", OutFillingShed
	if _, err := service.Record(ctx, "96942009-f8c4-4dbf-b7e0-6d5717651664", departure, "actor", "Satpam"); !errors.Is(err, ErrSPPUsed) {
		t.Fatalf("alias must not bypass Filling Shed reuse/completion: %v", err)
	}
	var movements, durations, closed int
	if err := db.Get(&movements, `SELECT COUNT(*) FROM movement_transactions`); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&durations, `SELECT COUNT(*) FROM movement_status_durations`); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&closed, `SELECT COUNT(*) FROM movement_cycles WHERE in_transaction_id IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	if movements != 4 || durations != 3 || closed != 2 {
		t.Fatalf("failed attempts/replays must not add audit/status/cycle records: %d/%d/%d", movements, durations, closed)
	}
}
