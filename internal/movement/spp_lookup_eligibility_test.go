package movement

import (
	"context"
	"reflect"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestSPPLookupReportsEligibilityWithoutBlockingIN(t *testing.T) {
	for _, test := range []struct {
		name           string
		direction      Direction
		category       string
		activeSPP      any
		activeCustomer any
		used           bool
		completed      bool
		want           []string
	}{
		{name: "Gantungan matching active SPP", direction: DirectionIn, category: InWaitingForDelivery, activeSPP: "SPP-001", activeCustomer: "CUST-1", used: true, want: []string{OutCustomer}},
		{name: "different active SPP", direction: DirectionIn, category: InWaitingForDelivery, activeSPP: "OTHER-SPP", activeCustomer: "CUST-1", want: []string{}},
		{name: "different customer", direction: DirectionIn, category: InWaitingForDelivery, activeSPP: "SPP-001", activeCustomer: "OTHER-CUSTOMER", want: []string{}},
		{name: "legacy cycle unknown", direction: DirectionIn, category: InWaitingForDelivery, want: []string{}},
		{name: "Kosongan", direction: DirectionIn, category: InWaitingForAssignment, want: []string{OutFillingShed}},
		{name: "Service", direction: DirectionIn, category: InServiceAndRepair, want: []string{OutFillingShed}},
		{name: "OUT document is still available for IN", direction: DirectionOut, used: true, want: []string{}},
		{name: "no history", want: []string{OutFillingShed}},
		{name: "used Filling Shed document", direction: DirectionIn, category: InWaitingForAssignment, used: true, want: []string{}},
		{name: "completed document", direction: DirectionIn, category: InWaitingForAssignment, completed: true, want: []string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var currentID any
			if test.direction != "" {
				currentID = int64(20)
			}
			mock.ExpectQuery(regexp.QuoteMeta(sppEligibilityQuery)).WithArgs("SAMT119", "SPP-001").
				WillReturnRows(sqlmock.NewRows(sppStateColumns).AddRow(7, "SAMT119", "DA 1234 AB", currentID, test.direction, test.category, test.activeSPP, test.activeCustomer, test.used, test.completed))
			result, err := NewService(sqlx.NewDb(db, "sqlmock"), nil, testDocuments{}).LookupSPP(context.Background(), " 123-example ")
			if err != nil {
				t.Fatal(err)
			}
			if result.SPPNumber != "123-example" || result.NoLambung != "SAMT119" || result.DriverName != "Driver" {
				t.Fatal("OUT eligibility changed the valid document lookup")
			}
			if !reflect.DeepEqual(result.AllowedOutDestinations, test.want) {
				t.Fatalf("destinations = %v, want %v", result.AllowedOutDestinations, test.want)
			}
			// Only the one read is allowed: no reservation or movement/status/cycle writes.
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

var sppStateColumns = []string{
	"lambung_unit_id", "no_lambung", "no_polisi", "current_transaction_id", "direction", "in_category",
	"active_spp", "active_customer_id", "filling_shed_used", "spp_completed",
}
