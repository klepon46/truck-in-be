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
		{name: "Gantungan matching active SPP and customer", want: []string{OutCustomer, OutFillingShed}},
		{name: "customer display name changes", adjust: func(_ *sppEligibilityState, d *Document) { d.CustomerName = "Renamed Customer" }, want: []string{OutCustomer, OutFillingShed}},
		{name: "different active SPP is allowed on the same unit", adjust: func(s *sppEligibilityState, _ *Document) { s.ActiveSPP = pointer("OTHER-SPP") }, want: []string{OutCustomer, OutFillingShed}},
		{name: "different scanned customer is not an old-customer mismatch", adjust: func(_ *sppEligibilityState, d *Document) { d.CustomerID = "OTHER-CUSTOMER" }, want: []string{OutCustomer, OutFillingShed}},
		{name: "missing scanned customer still blocks Customer", adjust: func(_ *sppEligibilityState, d *Document) { d.CustomerID = "" }, want: []string{OutFillingShed}},
		{name: "blank scanned customer still blocks Customer", adjust: func(_ *sppEligibilityState, d *Document) { d.CustomerID = " " }, want: []string{OutFillingShed}},
		{name: "missing historical customer no longer blocks OUT", adjust: func(s *sppEligibilityState, _ *Document) { s.ActiveCustomerID = nil }, want: []string{OutCustomer, OutFillingShed}},
		{name: "legacy Gantungan without trusted cycle", adjust: func(s *sppEligibilityState, _ *Document) { s.ActiveSPP = nil }, want: []string{OutCustomer, OutFillingShed}},
		{name: "Kosongan unused SPP", adjust: func(s *sppEligibilityState, _ *Document) { s.InCategory = InWaitingForAssignment }, want: []string{OutCustomer, OutFillingShed}},
		{name: "Service unused SPP", adjust: func(s *sppEligibilityState, _ *Document) {
			s.InCategory = InServiceAndRepair
			s.ActiveSPP = nil
			s.ActiveCustomerID = nil
		}, want: []string{OutCustomer, OutFillingShed}},
		{name: "Service continues used SPP to either destination", adjust: func(s *sppEligibilityState, _ *Document) { s.InCategory = InServiceAndRepair; s.FillingShedUsed = true }, want: []string{OutCustomer, OutFillingShed}},
		{name: "Service continues matching SPP before first Filling Shed", adjust: func(s *sppEligibilityState, _ *Document) { s.InCategory = InServiceAndRepair }, want: []string{OutCustomer, OutFillingShed}},
		{name: "Service new SPP permits both", adjust: func(s *sppEligibilityState, _ *Document) {
			s.InCategory = InServiceAndRepair
			s.ActiveSPP = pointer("OTHER-SPP")
		}, want: []string{OutCustomer, OutFillingShed}},
		{name: "Service permits another previously used same-unit SPP", adjust: func(s *sppEligibilityState, _ *Document) {
			s.InCategory = InServiceAndRepair
			s.ActiveSPP = pointer("OTHER-SPP")
			s.FillingShedUsed = true
		}, want: []string{OutCustomer, OutFillingShed}},
		{name: "Service does not require the old customer", adjust: func(s *sppEligibilityState, d *Document) {
			s.InCategory = InServiceAndRepair
			s.FillingShedUsed = true
			d.CustomerID = "OTHER"
		}, want: []string{OutCustomer, OutFillingShed}},
		{name: "Service can reuse historical Customer SPP", adjust: func(s *sppEligibilityState, _ *Document) {
			s.InCategory = InServiceAndRepair
			s.ActiveSPP = nil
			s.PriorSPPUsed = true
		}, want: []string{OutCustomer, OutFillingShed}},
		{name: "Service uses current scanned customer", adjust: func(s *sppEligibilityState, d *Document) {
			s.InCategory = InServiceAndRepair
			d.CustomerID = "OTHER"
		}, want: []string{OutCustomer, OutFillingShed}},
		{name: "OUT remains usable for IN lookup", adjust: func(s *sppEligibilityState, _ *Document) { s.Direction = DirectionOut; s.InCategory = "" }, want: []string{}},
		{name: "no history", adjust: func(s *sppEligibilityState, _ *Document) {
			s.CurrentTransactionID = nil
			s.Direction = ""
			s.InCategory = ""
		}, want: []string{OutFillingShed}},
		{name: "no current pointer permits same-unit Filling Shed reuse", adjust: func(s *sppEligibilityState, _ *Document) {
			s.CurrentTransactionID = nil
			s.Direction = ""
			s.InCategory = ""
			s.FillingShedUsed = true
		}, want: []string{OutFillingShed}},
		{name: "Filling Shed SPP already used", adjust: func(s *sppEligibilityState, _ *Document) {
			s.InCategory = InWaitingForAssignment
			s.FillingShedUsed = true
		}, want: []string{OutCustomer, OutFillingShed}},
		{name: "old completed SPP can depart after Service", adjust: func(s *sppEligibilityState, _ *Document) { s.InCategory = InServiceAndRepair; s.SPPCompleted = true }, want: []string{OutCustomer, OutFillingShed}},
		{name: "old completed SPP can depart after Gantungan", adjust: func(s *sppEligibilityState, _ *Document) { s.SPPCompleted = true }, want: []string{OutCustomer, OutFillingShed}},
		{name: "SPP history on another unit fails closed", adjust: func(s *sppEligibilityState, _ *Document) { s.UsedByOtherUnit = true }, want: []string{}},
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
	state.UsedByOtherUnit = true
	if err := validateSPPDeparture(state, OutCustomer, "OTHER-SPP", Document{CustomerID: "C400"}); !errors.Is(err, ErrSPPUsed) {
		t.Fatalf("foreign-unit SPP error = %v", err)
	}
	state.UsedByOtherUnit = false
	if err := validateSPPDeparture(state, OutCustomer, "SPP-001", Document{}); !errors.Is(err, ErrCustomerMismatch) {
		t.Fatalf("missing scanned customer error = %v", err)
	}
	state.InCategory = InWaitingForAssignment
	state.UsedByOtherUnit = true
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
