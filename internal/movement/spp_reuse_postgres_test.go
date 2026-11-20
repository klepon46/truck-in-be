package movement

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
)

func applySPPReuseMigration(t *testing.T, tx *sqlx.Tx, direction string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000007_allow_same_unit_spp_reuse."+direction+".sql"))
	if err != nil {
		t.Fatal(err)
	}
	if direction == "up" {
		if _, err := tx.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
	}
	return string(body)
}

const cloneSPPReuseSQL = `INSERT INTO movement_transactions OVERRIDING SYSTEM VALUE
 SELECT (jsonb_populate_record(NULL::movement_transactions,
 to_jsonb(m) || $2::jsonb || jsonb_build_object('transaction_id',transaction_id+100,
 'idempotency_key',gen_random_uuid()))).* FROM movement_transactions m WHERE transaction_id=$1`

func TestReusableSPPMigrationPostgres(t *testing.T) {
	for _, category := range []string{InWaitingForAssignment, InWaitingForDelivery, InServiceAndRepair} {
		t.Run(category, func(t *testing.T) {
			tx := serviceSchemaTestTransaction(t)
			out := sppFixtureMovement(t, tx, DirectionOut, "", OutFillingShed, "SPP-A", "C400")
			in := sppFixtureMovement(t, tx, DirectionIn, category, "", "SPP-A", "C400")
			sppFixtureCycle(t, tx, out, in)
			// Version 6 rejects normal Filling Shed reuse.
			assertServiceSQLRejected(t, tx, "23505", cloneSPPReuseSQL, out, `{}`)
			if _, err := tx.Exec(`UPDATE unit_snapshot SET current_transaction_id=$1 WHERE lambung_unit_id=1`, in); err != nil {
				t.Fatal(err)
			}
			var before string
			if err := tx.Get(&before, `SELECT jsonb_agg(to_jsonb(m) ORDER BY transaction_id)::text FROM movement_transactions m`); err != nil {
				t.Fatal(err)
			}
			applySPPReuseMigration(t, tx, "up")
			var backfilled bool
			if err := tx.Get(&backfilled, `SELECT EXISTS(SELECT 1 FROM movement_transactions WHERE spp_departure_policy_version IS NOT NULL)`); err != nil || backfilled {
				t.Fatal("upgrade must not backfill policy audit versions")
			}
			var unchanged string
			if err := tx.Get(&unchanged, `SELECT jsonb_agg(to_jsonb(m)-'spp_departure_policy_version' ORDER BY transaction_id)::text FROM movement_transactions m`); err != nil || unchanged != before {
				t.Fatal("upgrade rewrote historical snapshots")
			}
			for _, destination := range []string{OutFillingShed, OutCustomer} {
				id := sppFixtureMovement(t, tx, DirectionOut, "", destination, "SPP-A", "NEW-CUSTOMER")
				var version int
				var source *int64
				if err := tx.QueryRowx(`SELECT spp_departure_policy_version,resumed_from_service_transaction_id FROM movement_transactions WHERE transaction_id=$1`, id).Scan(&version, &source); err != nil || version != 7 || source != nil {
					t.Fatal("new reuse needs derived policy version without fabricated Service provenance")
				}
			}
			assertServiceSQLRejected(t, tx, "P0001", applySPPReuseMigration(t, tx, "down"))
			if _, err := tx.Exec(`INSERT INTO unit_snapshot(lambung_unit_id,no_lambung,no_polisi,synced_at) VALUES(2,'OTHER','MASTER TWO',NOW())`); err != nil {
				t.Fatal(err)
			}
			assertServiceSQLRejected(t, tx, "23514", cloneSPPReuseSQL, out, `{"lambung_unit_id":2,"no_lambung_snapshot":"OTHER"}`)
			assertServiceSQLRejected(t, tx, "23514", cloneSPPReuseSQL, in, `{"spp_departure_policy_version":7}`)
			assertServiceSQLRejected(t, tx, "23514", cloneSPPReuseSQL, out, `{"spp_departure_policy_version":6}`)
		})
	}
}

func TestReusableSPPMigrationEmptyDowngradePostgres(t *testing.T) {
	tx := serviceSchemaTestTransaction(t, 7)
	body := applySPPReuseMigration(t, tx, "down")
	if _, err := tx.Exec(body); err != nil {
		t.Fatal(err)
	}
	out := sppFixtureMovement(t, tx, DirectionOut, "", OutFillingShed, "SPP-A", "C400")
	assertServiceSQLRejected(t, tx, "23505", cloneSPPReuseSQL, out, `{}`)
	in := sppFixtureMovement(t, tx, DirectionIn, InServiceAndRepair, "", "SPP-A", "C400")
	sppFixtureCycle(t, tx, out, in)
	if _, err := tx.Exec(`UPDATE unit_snapshot SET current_transaction_id=$1 WHERE lambung_unit_id=1`, in); err != nil {
		t.Fatal(err)
	}
	assertServiceSQLRejected(t, tx, "23514", cloneSPPReuseSQL, out, `{"customer_id":"OTHER"}`)
	resumed := sppFixtureMovement(t, tx, DirectionOut, "", OutFillingShed, "SPP-A", "C400")
	var source int64
	if err := tx.Get(&source, `SELECT resumed_from_service_transaction_id FROM movement_transactions WHERE transaction_id=$1`, resumed); err != nil || source != in {
		t.Fatal("empty downgrade did not restore version-6 Service provenance derivation")
	}
}

func TestReusableSPPMigrationPreservesLegacyServiceLinksPostgres(t *testing.T) {
	tx := serviceSchemaTestTransaction(t)
	out := sppFixtureMovement(t, tx, DirectionOut, "", OutFillingShed, "SPP-A", "C400")
	in := sppFixtureMovement(t, tx, DirectionIn, InServiceAndRepair, "", "SPP-A", "C400")
	sppFixtureCycle(t, tx, out, in)
	if _, err := tx.Exec(`UPDATE unit_snapshot SET current_transaction_id=$1 WHERE lambung_unit_id=1`, in); err != nil {
		t.Fatal(err)
	}
	resumed := sppFixtureMovement(t, tx, DirectionOut, "", OutFillingShed, "SPP-A", "C400")
	applySPPReuseMigration(t, tx, "up")
	var intact bool
	if err := tx.Get(&intact, `SELECT resumed_from_service_transaction_id=$1 AND spp_departure_policy_version IS NULL FROM movement_transactions WHERE transaction_id=$2`, in, resumed); err != nil || !intact {
		t.Fatal("upgrade changed an existing Service audit link")
	}
	assertServiceSQLRejected(t, tx, "23505", cloneSPPReuseSQL, resumed, `{}`)
	assertServiceSQLRejected(t, tx, "23514", cloneSPPReuseSQL, out, `{"resumed_from_service_transaction_id":999}`)
	assertServiceSQLRejected(t, tx, "P0001", `DELETE FROM movement_transactions WHERE transaction_id=$1`, out)
	assertServiceSQLRejected(t, tx, "P0001", `DELETE FROM movement_cycles WHERE in_transaction_id=$1`, in)
	// Only old version-6 links exist, so policy downgrade must retain them.
	if _, err := tx.Exec(applySPPReuseMigration(t, tx, "down")); err != nil {
		t.Fatal(err)
	}
	var source int64
	if err := tx.Get(&source, `SELECT resumed_from_service_transaction_id FROM movement_transactions WHERE transaction_id=$1`, resumed); err != nil || source != in {
		t.Fatal("empty policy downgrade discarded existing Service provenance")
	}
}
