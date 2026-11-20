package movement

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jmoiron/sqlx"
)

type returningDocuments struct {
	testDocuments
	spp, unit, customer string
}

func (d returningDocuments) SPP(ctx context.Context, number string) (Document, error) {
	doc, err := (canonicalDocuments{number: d.spp}).SPP(ctx, number)
	if d.unit != "" {
		doc.NoLambung = d.unit
	}
	if d.customer != "" {
		doc.CustomerID = d.customer
	}
	return doc, err
}

func (d returningDocuments) Workshop(ctx context.Context, number string) (Document, error) {
	doc, err := d.testDocuments.Workshop(ctx, number)
	doc.NoPolisi = "MASTER PLATE"
	if d.unit != "" {
		doc.NoLambung = d.unit
	}
	return doc, err
}

func returnTestDeparture(t *testing.T, db *sqlx.DB, destination string) Transaction {
	t.Helper()
	s := NewService(db, nil, returningDocuments{spp: "SPP-A"})
	input := Input{NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: destination}
	if destination == OutWorkshop {
		input.WorkOrderNumber = "WO-A"
	} else {
		input.SPPNumber = "100-Owner"
	}
	if destination == OutCustomer {
		first := input
		first.OutDestination = OutFillingShed
		serviceTestRecord(t, s, "96942009-f8c4-4dbf-b7e0-000000000001", first)
		serviceTestRecord(t, s, "96942009-f8c4-4dbf-b7e0-000000000002", Input{NoLambung: "SAMT119", Direction: DirectionIn, InCategory: InWaitingForDelivery, SPPNumber: "100-Owner"})
	}
	return serviceTestRecord(t, s, "96942009-f8c4-4dbf-b7e0-000000000003", input)
}

func TestReturningDocumentPostgresDifferentNumbers(t *testing.T) {
	for _, destination := range []string{OutWorkshop, OutFillingShed, OutCustomer} {
		for _, category := range []string{InWaitingForAssignment, InWaitingForDelivery, InServiceAndRepair} {
			t.Run(destination+"/"+category, func(t *testing.T) {
				db := serviceLifecycleDB(t)
				out := returnTestDeparture(t, db, destination)
				var before string
				if err := db.Get(&before, `SELECT jsonb_agg(to_jsonb(m) ORDER BY transaction_id)::text FROM movement_transactions m`); err != nil {
					t.Fatal(err)
				}
				input := Input{NoLambung: "SAMT119", Direction: DirectionIn, InCategory: category, Coordinates: "-3,114"}
				if destination == OutWorkshop {
					input.WorkOrderNumber = "WO-B"
				} else {
					input.SPPNumber = "200-Owner"
				}
				s := NewService(db, nil, returningDocuments{spp: "SPP-B"})
				const key = "96942009-f8c4-4dbf-b7e0-000000000004"
				in := serviceTestRecord(t, s, key, input)
				if in.CycleID == nil || out.CycleID == nil || *in.CycleID != *out.CycleID ||
					in.DepartureTransactionID == nil || *in.DepartureTransactionID != out.ID {
					t.Fatal("different-number IN must close the original same-unit cycle")
				}
				if destination == OutWorkshop {
					if in.WorkOrderNumber == nil || *in.WorkOrderNumber != "WO-B" || in.DepartureWorkOrderNumber == nil || *in.DepartureWorkOrderNumber != "WO-A" {
						t.Fatal("IN WO and original OUT WO must be stored/displayed separately")
					}
				} else if in.SPPNumber == nil || *in.SPPNumber != "SPP-B" || in.DepartureSPPNumber == nil || *in.DepartureSPPNumber != "SPP-A" {
					t.Fatal("IN SPP and original OUT SPP must be stored/displayed separately")
				}
				var history string
				if err := db.Get(&history, `SELECT jsonb_agg(to_jsonb(m) ORDER BY transaction_id)::text FROM movement_transactions m WHERE transaction_id<$1`, in.ID); err != nil || history != before {
					t.Fatal("returning IN rewrote historical OUT snapshots")
				}
				replay, err := NewService(db, nil, nil).Record(context.Background(), key, input, "actor", "Satpam")
				if err != nil || !reflect.DeepEqual(replay, in) {
					t.Fatalf("different-number replay must retain both documents/coordinates without upstream: %v", err)
				}
				hash, _ := requestHash(input)
				if !bytes.Equal(in.RequestHash, hash) {
					t.Fatal("IN request hash lost original scanned reference/coordinates")
				}
				var duration int
				if err := db.Get(&duration, `SELECT COUNT(*) FROM movement_status_durations WHERE transaction_id=$1 AND duration_seconds>=0`, out.ID); err != nil || duration != 1 {
					t.Fatal("original OUT duration was not finalized exactly once")
				}
				if destination != OutWorkshop {
					for _, number := range []string{"SPP-A", "SPP-B"} {
						lookup, err := NewService(db, nil, returningDocuments{spp: number}).LookupSPP(context.Background(), "300-Owner")
						if err != nil || len(lookup.AllowedOutDestinations) != 2 {
							t.Fatalf("same-unit SPP %s remains eligible after a changed-number return: %v", number, err)
						}
					}
				}
				changed := input
				changed.Coordinates = "0,0"
				if _, err := s.Record(context.Background(), key, changed, "actor", "Satpam"); !errors.Is(err, ErrIdempotencyConflict) {
					t.Fatalf("changed retry must still conflict: %v", err)
				}
			})
		}
	}
}

func TestReturningDocumentPostgresRejectsWrongTypeOrUnit(t *testing.T) {
	for _, test := range []struct {
		name, departure string
		input           Input
		unit            string
		want            error
	}{
		{name: "WO to SPP", departure: OutWorkshop, input: Input{SPPNumber: "200-Owner"}, want: ErrReturnDocumentMismatch},
		{name: "SPP to WO", departure: OutFillingShed, input: Input{WorkOrderNumber: "WO-B"}, want: ErrReturnDocumentMismatch},
		{name: "WO to OTHER", departure: OutWorkshop, input: Input{DriverID: 42, DriverName: "Driver", Note: "Note"}, want: ErrReturnDocumentMismatch},
		{name: "SPP to OTHER", departure: OutFillingShed, input: Input{DriverID: 42, DriverName: "Driver", Note: "Note"}, want: ErrReturnDocumentMismatch},
		{name: "WO resolves to wrong unit", departure: OutWorkshop, input: Input{WorkOrderNumber: "WO-B"}, unit: "SAMT120", want: ErrExternalDocument},
		{name: "SPP resolves to wrong unit", departure: OutFillingShed, input: Input{SPPNumber: "200-Owner"}, unit: "SAMT120", want: ErrExternalDocument},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := serviceLifecycleDB(t)
			out := returnTestDeparture(t, db, test.departure)
			test.input.NoLambung, test.input.Direction, test.input.InCategory = "SAMT119", DirectionIn, InWaitingForAssignment
			s := NewService(db, nil, returningDocuments{spp: "SPP-B", unit: test.unit})
			if _, err := s.Record(context.Background(), "96942009-f8c4-4dbf-b7e0-000000000004", test.input, "actor", "Satpam"); !errors.Is(err, test.want) {
				t.Fatalf("expected %v, got %v", test.want, err)
			}
			var unchanged bool
			if err := db.Get(&unchanged, `SELECT
			 (SELECT COUNT(*)=1 FROM movement_transactions) AND
			 (SELECT COUNT(*)=0 FROM movement_status_durations) AND
			 (SELECT in_transaction_id IS NULL FROM movement_cycles WHERE out_transaction_id=$1) AND
			 (SELECT current_transaction_id=$1 FROM unit_snapshot WHERE lambung_unit_id=7)`, out.ID); err != nil || !unchanged {
				t.Fatal("rejected return changed audit/cycle/duration/status")
			}
		})
	}
}

func TestReturningDocumentPostgresForeignCurrentPointerFailsClosed(t *testing.T) {
	db := serviceLifecycleDB(t)
	out := returnTestDeparture(t, db, OutFillingShed)
	if _, err := db.Exec(`UPDATE unit_snapshot SET current_transaction_id=$1 WHERE lambung_unit_id=8`, out.ID); err != nil {
		t.Fatal(err)
	}
	s := NewService(db, nil, returningDocuments{spp: "SPP-B", unit: "SAMT120"})
	_, err := s.Record(context.Background(), "96942009-f8c4-4dbf-b7e0-000000000004", Input{NoLambung: "SAMT120", Direction: DirectionIn, InCategory: InWaitingForAssignment, SPPNumber: "200-Owner"}, "actor", "Satpam")
	if !errors.Is(err, ErrReturnDocumentMismatch) {
		t.Fatalf("same-type document must not close another unit's OUT: %v", err)
	}
}

func TestReturningDocumentPostgresMissingCycleRollsBack(t *testing.T) {
	db := serviceLifecycleDB(t)
	out := returnTestDeparture(t, db, OutFillingShed)
	if _, err := db.Exec(`DELETE FROM movement_cycles WHERE out_transaction_id=$1`, out.ID); err != nil {
		t.Fatal(err)
	}
	s := NewService(db, nil, returningDocuments{spp: "SPP-B"})
	_, err := s.Record(context.Background(), "96942009-f8c4-4dbf-b7e0-000000000004", Input{NoLambung: "SAMT119", Direction: DirectionIn, InCategory: InWaitingForAssignment, SPPNumber: "200-Owner"}, "actor", "Satpam")
	if !errors.Is(err, errOpenCycleNotFound) {
		t.Fatalf("missing open cycle must fail after IN insert: %v", err)
	}
	var unchanged bool
	if err := db.Get(&unchanged, `SELECT
	 (SELECT COUNT(*)=1 FROM movement_transactions) AND
	 (SELECT COUNT(*)=0 FROM movement_status_durations) AND
	 (SELECT current_transaction_id=$1 FROM unit_snapshot WHERE lambung_unit_id=7)`, out.ID); err != nil || !unchanged {
		t.Fatal("cycle failure left a partial different-number return")
	}
}
