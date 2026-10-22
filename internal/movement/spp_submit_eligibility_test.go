package movement

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestSPPSubmitRejectsStatusChangedAfterLookup(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(sqlx.NewDb(db, "sqlmock"), nil, testDocuments{})
	mock.ExpectQuery(regexp.QuoteMeta(sppEligibilityQuery)).WithArgs("SAMT119", "SPP-001").
		WillReturnRows(sqlmock.NewRows(sppStateColumns).AddRow(7, "SAMT119", "DA 1234 AB", 20, "IN", InWaitingForDelivery, "SPP-001", "CUST-1", true, false))
	lookup, err := service.LookupSPP(context.Background(), "SPP-001")
	if err != nil || len(lookup.AllowedOutDestinations) != 1 || lookup.AllowedOutDestinations[0] != OutCustomer {
		t.Fatalf("initial Customer lookup failed: %v", err)
	}
	mock.ExpectQuery("SELECT m.transaction_id").WillReturnRows(sqlmock.NewRows(transactionCycleColumns))
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT u.lambung_unit_id").WithArgs("SAMT119").
		WillReturnRows(sqlmock.NewRows([]string{"lambung_unit_id", "no_lambung", "no_polisi", "current_transaction_id", "direction", "in_category"}).
			AddRow(7, "SAMT119", "DA 1234 AB", 21, "OUT", nil))
	mock.ExpectQuery("SELECT m.transaction_id").WillReturnRows(sqlmock.NewRows(transactionCycleColumns))
	mock.ExpectRollback()
	_, err = service.Record(context.Background(), "96942009-f8c4-4dbf-b7e0-6d571765166a", Input{
		NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: OutCustomer, SPPNumber: "SPP-001",
	}, "12", "Satpam")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("stale lookup must not authorize a new departure: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSPPSubmitAppliesSharedEligibilityUnderLock(t *testing.T) {
	databaseFailure := errors.New("test database unavailable")
	for _, test := range []struct {
		name, destination, category string
		activeSPP, activeCustomer   any
		used, completed             bool
		readError                   error
		want                        error
	}{
		{name: "missing legacy cycle", destination: OutCustomer, category: InWaitingForDelivery, used: true, want: ErrSPPMismatch},
		{name: "different active SPP", destination: OutCustomer, category: InWaitingForDelivery, activeSPP: "OTHER-SPP", activeCustomer: "CUST-1", want: ErrSPPMismatch},
		{name: "different customer code", destination: OutCustomer, category: InWaitingForDelivery, activeSPP: "SPP-001", activeCustomer: "OTHER", want: ErrCustomerMismatch},
		{name: "completed SPP", destination: OutFillingShed, category: InWaitingForAssignment, completed: true, want: ErrSPPUsed},
		{name: "Filling Shed already used", destination: OutFillingShed, category: InServiceAndRepair, used: true, want: ErrSPPUsed},
		{name: "database failure is not a mismatch", destination: OutCustomer, category: InWaitingForDelivery, readError: databaseFailure, want: databaseFailure},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectQuery("SELECT m.transaction_id").WillReturnRows(sqlmock.NewRows(transactionCycleColumns))
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT u.lambung_unit_id").WithArgs("SAMT119").
				WillReturnRows(sqlmock.NewRows([]string{"lambung_unit_id", "no_lambung", "no_polisi", "current_transaction_id", "direction", "in_category"}).
					AddRow(7, "SAMT119", "DA 1234 AB", 20, "IN", test.category))
			mock.ExpectQuery("SELECT m.transaction_id").WillReturnRows(sqlmock.NewRows(transactionCycleColumns))
			expectation := mock.ExpectQuery(regexp.QuoteMeta(sppEligibilityQuery)).WithArgs("SAMT119", "SPP-001")
			if test.readError != nil {
				expectation.WillReturnError(test.readError)
			} else {
				expectation.WillReturnRows(sqlmock.NewRows(sppStateColumns).AddRow(7, "SAMT119", "DA 1234 AB", 20, "IN", test.category, test.activeSPP, test.activeCustomer, test.used, test.completed))
			}
			mock.ExpectRollback()
			_, err = NewService(sqlx.NewDb(db, "sqlmock"), nil, testDocuments{}).Record(context.Background(), "96942009-f8c4-4dbf-b7e0-6d571765166a", Input{
				NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: test.destination, SPPNumber: "123-example",
			}, "12", "Satpam")
			if !errors.Is(err, test.want) {
				t.Fatalf("submit eligibility error = %v, want %v", err, test.want)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSPPReplayDoesNotRecheckCurrentEligibility(t *testing.T) {
	for _, coordinates := range []string{"", "-3.319437,114.590889"} {
		t.Run(coordinates, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			input := Input{NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: OutCustomer, SPPNumber: "SPP-001", Coordinates: coordinates}
			hash, err := requestHash(input)
			if err != nil {
				t.Fatal(err)
			}
			mock.ExpectQuery("SELECT m.transaction_id").WithArgs("96942009-f8c4-4dbf-b7e0-6d571765166a").
				WillReturnRows(sqlmock.NewRows([]string{"transaction_id", "request_hash", "spp_number"}).AddRow(21, hash, "SPP-001"))
			result, err := NewService(sqlx.NewDb(db, "sqlmock"), nil, nil).Record(context.Background(), "96942009-f8c4-4dbf-b7e0-6d571765166a", input, "12", "Satpam")
			if err != nil || result.ID != 21 {
				t.Fatalf("stored replay must work without documents or eligibility reads: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSPPSubmitStillStartsCycleForEligibleCustomer(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	input := Input{NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: OutCustomer, SPPNumber: "123-example"}
	hash, err := requestHash(input)
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT m.transaction_id").WillReturnRows(sqlmock.NewRows(transactionCycleColumns))
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT u.lambung_unit_id").WithArgs("SAMT119").
		WillReturnRows(sqlmock.NewRows([]string{"lambung_unit_id", "no_lambung", "no_polisi", "current_transaction_id", "direction", "in_category"}).
			AddRow(7, "SAMT119", "DA 1234 AB", 20, "IN", InWaitingForDelivery))
	mock.ExpectQuery("SELECT m.transaction_id").WillReturnRows(sqlmock.NewRows(transactionCycleColumns))
	mock.ExpectQuery(regexp.QuoteMeta(sppEligibilityQuery)).WithArgs("SAMT119", "SPP-001").
		WillReturnRows(sqlmock.NewRows(sppStateColumns).AddRow(7, "SAMT119", "DA 1234 AB", 20, "IN", InWaitingForDelivery, "SPP-001", "CUST-1", true, false))
	mock.ExpectQuery("INSERT INTO movement_transactions").
		WithArgs("96942009-f8c4-4dbf-b7e0-6d571765166a", hash, int64(7), "SAMT119", "DA 1234 AB",
			DirectionOut, "", OutCustomer, int64(0), "Driver", "", "SPP-001", "CUST-1", "Customer One", "", "12", "Satpam", "").
		WillReturnRows(sqlmock.NewRows(transactionColumns).AddRow(21, []byte("stored-hash"), 7, "SAMT119", "DA 1234 AB", "OUT", nil, OutCustomer, nil, "Driver", nil, "SPP-001", "CUST-1", "Customer One", nil, "12", "Satpam", time.Now(), nil))
	mock.ExpectQuery("INSERT INTO movement_cycles").WithArgs(int64(7), int64(21)).
		WillReturnRows(sqlmock.NewRows([]string{"cycle_id"}).AddRow(92))
	mock.ExpectExec("INSERT INTO movement_status_durations").WithArgs(int64(20)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE unit_snapshot SET current_transaction_id").WithArgs(int64(21), int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	result, err := NewService(sqlx.NewDb(db, "sqlmock"), nil, testDocuments{}).Record(context.Background(), "96942009-f8c4-4dbf-b7e0-6d571765166a", input, "12", "Satpam")
	if err != nil || result.CycleID == nil || *result.CycleID != 92 || result.DepartureSPPNumber == nil || *result.DepartureSPPNumber != "SPP-001" {
		t.Fatalf("eligible Customer submit failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSPPSubmitStillAllowsFirstUnusedFillingShedDeparture(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT m.transaction_id").WillReturnRows(sqlmock.NewRows(transactionCycleColumns))
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT u.lambung_unit_id").WithArgs("SAMT119").
		WillReturnRows(sqlmock.NewRows([]string{"lambung_unit_id", "no_lambung", "no_polisi", "current_transaction_id", "direction", "in_category"}).
			AddRow(7, "SAMT119", "DA 1234 AB", nil, nil, nil))
	mock.ExpectQuery("SELECT m.transaction_id").WillReturnRows(sqlmock.NewRows(transactionCycleColumns))
	mock.ExpectQuery(regexp.QuoteMeta(sppEligibilityQuery)).WithArgs("SAMT119", "SPP-001").
		WillReturnRows(sqlmock.NewRows(sppStateColumns).AddRow(7, "SAMT119", "DA 1234 AB", nil, "", "", nil, nil, false, false))
	mock.ExpectQuery("INSERT INTO movement_transactions").
		WithArgs("96942009-f8c4-4dbf-b7e0-6d571765166a", sqlmock.AnyArg(), int64(7), "SAMT119", "DA 1234 AB",
			DirectionOut, "", OutFillingShed, int64(0), "Driver", "", "SPP-001", "CUST-1", "Customer One", "", "12", "Satpam", "").
		WillReturnRows(sqlmock.NewRows(transactionColumns).AddRow(21, []byte("stored-hash"), 7, "SAMT119", "DA 1234 AB", "OUT", nil, OutFillingShed, nil, "Driver", nil, "SPP-001", "CUST-1", "Customer One", nil, "12", "Satpam", time.Now(), nil))
	mock.ExpectQuery("INSERT INTO movement_cycles").WithArgs(int64(7), int64(21)).
		WillReturnRows(sqlmock.NewRows([]string{"cycle_id"}).AddRow(92))
	mock.ExpectExec("UPDATE unit_snapshot SET current_transaction_id").WithArgs(int64(21), int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	result, err := NewService(sqlx.NewDb(db, "sqlmock"), nil, testDocuments{}).Record(context.Background(), "96942009-f8c4-4dbf-b7e0-6d571765166a", Input{
		NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: OutFillingShed, SPPNumber: "SPP-001",
	}, "12", "Satpam")
	if err != nil || result.CycleID == nil || *result.CycleID != 92 {
		t.Fatalf("first eligible Filling Shed submit failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
