package movement

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestReusableSPPPostgresRepeatsAfterEveryCategory(t *testing.T) {
	for _, category := range []string{InWaitingForAssignment, InWaitingForDelivery, InServiceAndRepair} {
		for _, destination := range []string{OutFillingShed, OutCustomer} {
			t.Run(category+"/"+destination, func(t *testing.T) {
				db := serviceLifecycleDB(t)
				ctx := context.Background()
				inputs := []Input{
					{Direction: DirectionOut, OutDestination: OutFillingShed},
					{Direction: DirectionIn, InCategory: category},
					{Direction: DirectionOut, OutDestination: destination},
					{Direction: DirectionIn, InCategory: category},
					{Direction: DirectionOut, OutDestination: destination},
					{Direction: DirectionIn, InCategory: category},
					{Direction: DirectionOut, OutDestination: destination},
				}
				var history string
				var previous Transaction
				for i, input := range inputs {
					key := fmt.Sprintf("96942009-f8c4-4dbf-b7e0-%012d", i+100)
					input.NoLambung, input.SPPNumber, input.Coordinates = "SAMT119", fmt.Sprintf("%d-Owner", i+100), "-3,114"
					number, customer := "SPP-A", "CUST-1"
					if i == 1 {
						number = "SPP-B" // The latest IN need not match the next OUT SPP.
					}
					if i >= 2 {
						customer = "NEW-CUSTOMER" // No comparison with an old customer's code.
					}
					s := NewService(db, nil, returningDocuments{spp: number, customer: customer})
					if input.Direction == DirectionOut && i > 0 {
						lookup, err := s.LookupSPP(ctx, input.SPPNumber)
						if err != nil || !reflect.DeepEqual(lookup.AllowedOutDestinations, []string{OutCustomer, OutFillingShed}) {
							t.Fatalf("step %d should offer both destinations: %v", i, err)
						}
					}
					row := serviceTestRecord(t, s, key, input)
					if row.SPPNumber == nil || *row.SPPNumber != number {
						t.Fatal("stored number did not use canonical document identity")
					}
					if input.Direction == DirectionIn && (row.CycleID == nil || previous.CycleID == nil || *row.CycleID != *previous.CycleID) {
						t.Fatal("repeat-use cycle closure changed")
					}
					if input.Direction == DirectionOut {
						var version int
						var source *int64
						if err := db.QueryRowx(`SELECT spp_departure_policy_version,resumed_from_service_transaction_id FROM movement_transactions WHERE transaction_id=$1`, row.ID).Scan(&version, &source); err != nil || version != 7 {
							t.Fatal("SPP OUT lacks the derived policy audit version")
						}
						if category == InServiceAndRepair && (i == 4 || i == 6) {
							if source == nil || *source != previous.ID {
								t.Fatal("genuine Service provenance must still be retained")
							}
						} else if source != nil {
							t.Fatal("normal reuse fabricated Service provenance")
						}
					}
					replay, err := NewService(db, nil, nil).Record(ctx, key, input, "actor", "Satpam")
					if err != nil || !reflect.DeepEqual(replay, row) {
						t.Fatalf("repeat-use reference/coordinate replay changed: %v", err)
					}
					var before string
					if err := db.Get(&before, `SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY transaction_id),'[]'::jsonb)::text FROM movement_transactions m WHERE transaction_id<$1`, row.ID); err != nil || (i > 0 && before != history) {
						t.Fatal("repeat use rewrote earlier audit snapshots")
					}
					if err := db.Get(&history, `SELECT jsonb_agg(to_jsonb(m) ORDER BY transaction_id)::text FROM movement_transactions m`); err != nil {
						t.Fatal(err)
					}
					previous = row
				}
				duplicate := Input{NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: destination, SPPNumber: "999-Owner"}
				s := NewService(db, nil, returningDocuments{spp: "SPP-A"})
				if _, err := s.Record(ctx, "96942009-f8c4-4dbf-b7e0-999999999998", duplicate, "actor", "Satpam"); !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("repeat use must not allow OUT→OUT: %v", err)
				}
				duplicate.NoLambung, duplicate.OutDestination = "SAMT120", OutFillingShed
				other := NewService(db, nil, returningDocuments{spp: "SPP-A", unit: "SAMT120"})
				if _, err := other.Record(ctx, "96942009-f8c4-4dbf-b7e0-999999999999", duplicate, "actor", "Satpam"); !errors.Is(err, ErrSPPUsed) {
					t.Fatalf("same canonical SPP must not migrate to another unit: %v", err)
				}
				var complete bool
				if err := db.Get(&complete, `SELECT
				 (SELECT COUNT(*)=7 FROM movement_transactions) AND
				 (SELECT COUNT(*)=3 FROM movement_cycles WHERE completed_at IS NOT NULL) AND
				 (SELECT COUNT(*)=1 FROM movement_cycles WHERE completed_at IS NULL) AND
				 (SELECT COUNT(*)=6 FROM movement_status_durations WHERE duration_seconds>=0)`); err != nil || !complete {
					t.Fatal("rejected attempts/replays changed audit/cycle/duration counts")
				}
			})
		}
	}
}
