package movement

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func waitForChangedSPPReturnLock(t *testing.T, db *sqlx.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		var waiting bool
		if err := db.GetContext(ctx, &waiting, `SELECT EXISTS(
		 SELECT 1 FROM pg_stat_activity WHERE datname=current_database()
		 AND wait_event='advisory' AND query LIKE '%ORDER BY spp_key%')`); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("changed-SPP return did not lock both canonical identities before INSERT")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestReturningDocumentPostgresLocksIncomingAndOriginalSPP(t *testing.T) {
	for _, held := range []string{"SPP-A", "SPP-B"} {
		t.Run(held, func(t *testing.T) {
			db := serviceLifecycleDB(t)
			returnTestDeparture(t, db, OutFillingShed)
			holder, err := db.Beginx()
			if err != nil {
				t.Fatal(err)
			}
			defer holder.Rollback()
			if _, err := holder.Exec(`SELECT pg_advisory_xact_lock(8105,hashtext($1))`, held); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := NewService(db, nil, returningDocuments{spp: "SPP-B"}).Record(ctx,
					"96942009-f8c4-4dbf-b7e0-000000000004",
					Input{NoLambung: "SAMT119", Direction: DirectionIn, InCategory: InWaitingForAssignment, SPPNumber: "200-Owner"}, "actor", "Satpam")
				done <- err
			}()
			waitForChangedSPPReturnLock(t, db)
			if err := holder.Rollback(); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatalf("changed-SPP return failed after releasing %s: %v", held, err)
			}
		})
	}
}

func TestReturningDocumentPostgresReverseSPPReturnsDoNotDeadlock(t *testing.T) {
	db := serviceLifecycleDB(t)
	outA := NewService(db, nil, returningDocuments{spp: "SPP-A"})
	outB := NewService(db, nil, returningDocuments{spp: "SPP-B", unit: "SAMT120"})
	serviceTestRecord(t, outA, "96942009-f8c4-4dbf-b7e0-000000000001", Input{NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: OutFillingShed, SPPNumber: "100-Owner"})
	serviceTestRecord(t, outB, "96942009-f8c4-4dbf-b7e0-000000000002", Input{NoLambung: "SAMT120", Direction: DirectionOut, OutDestination: OutFillingShed, SPPNumber: "200-Owner"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	done := make(chan error, 2)
	for _, test := range []struct{ unit, number, key string }{
		{"SAMT119", "SPP-B", "96942009-f8c4-4dbf-b7e0-000000000003"},
		{"SAMT120", "SPP-A", "96942009-f8c4-4dbf-b7e0-000000000004"},
	} {
		go func(unit, number, key string) {
			<-start
			_, err := NewService(db, nil, returningDocuments{spp: number, unit: unit}).Record(ctx, key,
				Input{NoLambung: unit, Direction: DirectionIn, InCategory: InWaitingForAssignment, SPPNumber: "300-Owner"}, "actor", "Satpam")
			done <- err
		}(test.unit, test.number, test.key)
	}
	close(start)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatalf("reverse A→B / B→A returns failed: %v", err)
		}
	}
	var completed bool
	if err := db.Get(&completed, `SELECT
	 (SELECT COUNT(*)=4 FROM movement_transactions) AND
	 (SELECT COUNT(*)=2 FROM movement_cycles WHERE completed_at IS NOT NULL) AND
	 unitlog_spp_completed('SPP-A') AND unitlog_spp_completed('SPP-B')`); err != nil || !completed {
		t.Fatal("reverse returns did not complete the original/scanned SPPs atomically")
	}
}
