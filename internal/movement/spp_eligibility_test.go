package movement

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestSPPOutEligibilityPolicy(t *testing.T) {
	for _, test := range []struct {
		name   string
		adjust func(*sppEligibilityState, *Document)
		want   []string
	}{
		{name: "Gantungan matching active SPP and customer", want: []string{OutCustomer}},
		{name: "customer display name changes", adjust: func(_ *sppEligibilityState, d *Document) { d.CustomerName = "Renamed Customer" }, want: []string{OutCustomer}},
		{name: "different active SPP", adjust: func(s *sppEligibilityState, _ *Document) { s.ActiveSPP = pointer("OTHER-SPP") }, want: []string{}},
		{name: "different customer code", adjust: func(_ *sppEligibilityState, d *Document) { d.CustomerID = "OTHER-CUSTOMER" }, want: []string{}},
		{name: "missing customer code", adjust: func(s *sppEligibilityState, _ *Document) { s.ActiveCustomerID = nil }, want: []string{}},
		{name: "legacy Gantungan without trusted cycle", adjust: func(s *sppEligibilityState, _ *Document) { s.ActiveSPP = nil }, want: []string{}},
		{name: "Kosongan unused SPP", adjust: func(s *sppEligibilityState, _ *Document) { s.InCategory = InWaitingForAssignment }, want: []string{OutFillingShed}},
		{name: "Service unused SPP", adjust: func(s *sppEligibilityState, _ *Document) { s.InCategory = InServiceAndRepair }, want: []string{OutFillingShed}},
		{name: "OUT remains usable for IN lookup", adjust: func(s *sppEligibilityState, _ *Document) { s.Direction = DirectionOut; s.InCategory = "" }, want: []string{}},
		{name: "no history", adjust: func(s *sppEligibilityState, _ *Document) {
			s.CurrentTransactionID = nil
			s.Direction = ""
			s.InCategory = ""
		}, want: []string{OutFillingShed}},
		{name: "Filling Shed SPP already used", adjust: func(s *sppEligibilityState, _ *Document) {
			s.InCategory = InWaitingForAssignment
			s.FillingShedUsed = true
		}, want: []string{}},
		{name: "completed SPP cannot depart for Filling Shed", adjust: func(s *sppEligibilityState, _ *Document) { s.InCategory = InServiceAndRepair; s.SPPCompleted = true }, want: []string{}},
		{name: "completed SPP cannot depart for Customer", adjust: func(s *sppEligibilityState, _ *Document) { s.SPPCompleted = true }, want: []string{}},
		{name: "dangling current pointer is not no history", adjust: func(s *sppEligibilityState, _ *Document) { s.Direction = ""; s.InCategory = "" }, want: []string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			currentID := int64(20)
			state := sppEligibilityState{CurrentTransactionID: &currentID, Direction: DirectionIn, InCategory: InWaitingForDelivery, ActiveSPP: pointer("SPP-001"), ActiveCustomerID: pointer("C400")}
			document := Document{CustomerID: "C400", CustomerName: "Customer"}
			if test.adjust != nil {
				test.adjust(&state, &document)
			}
			if got := allowedSPPOutDestinations(state, "SPP-001", document); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("allowed destinations = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSPPDepartureValidationPreservesDomainErrors(t *testing.T) {
	state := sppEligibilityState{Direction: DirectionIn, InCategory: InWaitingForDelivery, ActiveSPP: pointer("SPP-001"), ActiveCustomerID: pointer("C400")}
	if err := validateSPPDeparture(state, OutCustomer, "OTHER-SPP", Document{CustomerID: "C400"}); !errors.Is(err, ErrSPPMismatch) {
		t.Fatalf("different SPP error = %v", err)
	}
	if err := validateSPPDeparture(state, OutCustomer, "SPP-001", Document{CustomerID: "OTHER"}); !errors.Is(err, ErrCustomerMismatch) {
		t.Fatalf("different customer error = %v", err)
	}
	state.InCategory = InWaitingForAssignment
	state.FillingShedUsed = true
	if err := validateSPPDeparture(state, OutFillingShed, "SPP-001", Document{}); !errors.Is(err, ErrSPPUsed) {
		t.Fatalf("used SPP error = %v", err)
	}
}

func TestReadSPPStatePreservesDatabaseFailures(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	database := sqlx.NewDb(db, "sqlmock")
	failure := errors.New("database unavailable")
	mock.ExpectQuery(regexp.QuoteMeta(sppEligibilityQuery)).WithArgs("SAMT119", "SPP-001").WillReturnError(failure)
	_, err = readSPPState(context.Background(), database, "SAMT119", "SPP-001")
	if !errors.Is(err, failure) || errors.Is(err, ErrSPPMismatch) || errors.Is(err, ErrUnitNotFound) {
		t.Fatalf("database failure was masked as an eligibility decision: %v", err)
	}
	mock.ExpectQuery(regexp.QuoteMeta(sppEligibilityQuery)).WithArgs("SAMT119", "SPP-001").
		WillReturnRows(sqlmock.NewRows([]string{"lambung_unit_id"}))
	_, err = readSPPState(context.Background(), database, "SAMT119", "SPP-001")
	if !errors.Is(err, ErrUnitNotFound) {
		t.Fatalf("missing active unit error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
