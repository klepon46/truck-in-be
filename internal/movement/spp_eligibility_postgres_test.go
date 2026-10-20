package movement

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jmoiron/sqlx"
)

func TestSPPEligibilityPostgres(t *testing.T) {
	dsn := os.Getenv("UNITLOG_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires an isolated local unitlog_eligibility_test PostgreSQL database")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") || parsed.Path != "/unitlog_eligibility_test" {
		t.Fatal("only the isolated local unitlog_eligibility_test database is allowed")
	}
	db, err := sqlx.Open("postgres", dsn)
	if err != nil {
		t.Fatal("open isolated test database")
	}
	defer db.Close()
	migrations, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	if err != nil || len(migrations) == 0 {
		t.Fatal("migration files not found")
	}
	for _, name := range []string{
		"no history", "Kosongan", "Service", "Gantungan", "different SPP", "different customer",
		"legacy Gantungan without cycle", "unrelated old SPP", "OUT after historical Gantungan",
		"Gantungan returning from Customer", "used Filling Shed SPP", "completed first IN SPP", "completed legacy return",
	} {
		t.Run(name, func(t *testing.T) {
			tx, err := db.BeginTxx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback() // Fixtures and schema never survive the test transaction.
			for _, path := range migrations {
				body, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(string(body)); err != nil {
					t.Fatalf("migration %s: %v", filepath.Base(path), err)
				}
			}
			if _, err := tx.Exec(`INSERT INTO unit_snapshot (lambung_unit_id, no_lambung, no_polisi, synced_at) VALUES (1, 'SAMT119', 'DA 1234 AB', NOW())`); err != nil {
				t.Fatal(err)
			}
			var currentID int64
			want := []string{}
			switch name {
			case "no history":
				want = []string{OutFillingShed}
			case "Kosongan", "Service":
				category := InWaitingForAssignment
				if name == "Service" {
					category = InServiceAndRepair
				}
				currentID = sppFixtureMovement(t, tx, DirectionIn, category, "", "", "")
				want = []string{OutFillingShed}
			case "Gantungan", "different SPP", "different customer", "OUT after historical Gantungan", "Gantungan returning from Customer":
				number, customer := "SPP-001", "C400"
				if name == "different SPP" {
					number = "OTHER-SPP"
				}
				if name == "different customer" {
					customer = "OTHER-CUSTOMER"
				}
				out := sppFixtureMovement(t, tx, DirectionOut, "", OutFillingShed, number, customer)
				currentID = sppFixtureMovement(t, tx, DirectionIn, InWaitingForDelivery, "", number, customer)
				sppFixtureCycle(t, tx, out, currentID)
				if name == "Gantungan" || name == "Gantungan returning from Customer" {
					want = []string{OutCustomer}
				}
				if name == "OUT after historical Gantungan" || name == "Gantungan returning from Customer" {
					out = sppFixtureMovement(t, tx, DirectionOut, "", OutCustomer, number, customer)
					currentID = out
					if name == "Gantungan returning from Customer" {
						currentID = sppFixtureMovement(t, tx, DirectionIn, InWaitingForDelivery, "", number, customer)
						sppFixtureCycle(t, tx, out, currentID)
					}
				}
			case "legacy Gantungan without cycle", "unrelated old SPP":
				sppFixtureMovement(t, tx, DirectionOut, "", OutFillingShed, "SPP-001", "C400")
				currentID = sppFixtureMovement(t, tx, DirectionIn, InWaitingForDelivery, "", "", "")
				if name == "unrelated old SPP" {
					out := sppFixtureMovement(t, tx, DirectionOut, "", OutOther, "", "")
					currentID = sppFixtureMovement(t, tx, DirectionIn, InWaitingForDelivery, "", "", "")
					sppFixtureCycle(t, tx, out, currentID)
				}
			case "used Filling Shed SPP", "completed legacy return":
				out := sppFixtureMovement(t, tx, DirectionOut, "", OutFillingShed, "SPP-001", "C400")
				currentID = sppFixtureMovement(t, tx, DirectionIn, InWaitingForAssignment, "", "", "")
				if name == "completed legacy return" {
					sppFixtureCycle(t, tx, out, currentID)
				}
			case "completed first IN SPP":
				currentID = sppFixtureMovement(t, tx, DirectionIn, InWaitingForAssignment, "", "SPP-001", "C400")
			}
			if currentID != 0 {
				if _, err := tx.Exec(`UPDATE unit_snapshot SET current_transaction_id = $1 WHERE lambung_unit_id = 1`, currentID); err != nil {
					t.Fatal(err)
				}
			}
			state, err := readSPPState(context.Background(), tx, "SAMT119", "SPP-001")
			if err != nil {
				t.Fatal(err)
			}
			if got := allowedSPPOutDestinations(state, "SPP-001", Document{CustomerID: "C400", CustomerName: "Display Name Changed"}); !reflect.DeepEqual(got, want) {
				t.Fatalf("destinations = %v, want %v", got, want)
			}
			if (name == "completed legacy return" || name == "completed first IN SPP") && !state.SPPCompleted {
				t.Fatal("completion was not detected from the matching SPP/cycle")
			}
		})
	}
}

func sppFixtureMovement(t *testing.T, tx *sqlx.Tx, direction Direction, category, destination, number, customer string) int64 {
	t.Helper()
	var driver, driverID, note any
	if direction == DirectionOut || number != "" {
		driver = "Test Driver"
	}
	if destination == OutOther {
		driverID, note = int64(42), "Test note"
	}
	var id int64
	err := tx.Get(&id, `INSERT INTO movement_transactions (
    idempotency_key, request_hash, lambung_unit_id, no_lambung_snapshot, no_polisi_snapshot,
    direction, in_category, out_destination, spp_number, customer_id, driver_name_snapshot, driver_id, note, actor_id, actor_name_snapshot
) VALUES (gen_random_uuid(), decode('00', 'hex'), 1, 'SAMT119', 'DA 1234 AB',
    $1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6, $7, $8, 'test', 'Test')
RETURNING transaction_id`, direction, category, destination, number, customer, driver, driverID, note)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func sppFixtureCycle(t *testing.T, tx *sqlx.Tx, out, in int64) {
	t.Helper()
	if _, err := tx.Exec(`INSERT INTO movement_cycles (lambung_unit_id, out_transaction_id, in_transaction_id, completed_at) VALUES (1, $1, $2, NOW())`, out, in); err != nil {
		t.Fatal(err)
	}
}
