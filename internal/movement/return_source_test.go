package movement

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestReturningSourceMatchesTypeRatherThanNumber(t *testing.T) {
	for _, test := range []struct {
		name, destination string
		workOrder, spp    any
		input             Input
		missing           bool
		wantError         bool
	}{
		{name: "same WO", destination: OutWorkshop, workOrder: "WO-A", input: Input{WorkOrderNumber: "WO-A"}},
		{name: "different WO same unit", destination: OutWorkshop, workOrder: "WO-A", input: Input{WorkOrderNumber: "WO-B"}},
		{name: "same SPP", destination: OutFillingShed, spp: "SPP-A", input: Input{SPPNumber: "SPP-A"}},
		{name: "different SPP from Filling Shed", destination: OutFillingShed, spp: "SPP-A", input: Input{SPPNumber: "SPP-B"}},
		{name: "different SPP from Customer", destination: OutCustomer, spp: "SPP-A", input: Input{SPPNumber: "SPP-B"}},
		{name: "WO cannot return with SPP", destination: OutWorkshop, workOrder: "WO-A", input: Input{SPPNumber: "SPP-B"}, wantError: true},
		{name: "SPP cannot return with WO", destination: OutFillingShed, spp: "SPP-A", input: Input{WorkOrderNumber: "WO-B"}, wantError: true},
		{name: "SPP cannot return with OTHER", destination: OutCustomer, spp: "SPP-A", input: Input{DriverID: 42, DriverName: "Driver", Note: "Note"}, wantError: true},
		{name: "WO cannot return with OTHER", destination: OutWorkshop, workOrder: "WO-A", input: Input{DriverID: 42, DriverName: "Driver", Note: "Note"}, wantError: true},
		{name: "OTHER remains OTHER", destination: OutOther, input: Input{DriverID: 42, DriverName: "Driver", Note: "Note"}},
		{name: "OTHER cannot return with WO", destination: OutOther, input: Input{WorkOrderNumber: "WO-B"}, wantError: true},
		{name: "OTHER cannot return with SPP", destination: OutOther, input: Input{SPPNumber: "SPP-B"}, wantError: true},
		{name: "missing WO source fails closed", destination: OutWorkshop, input: Input{WorkOrderNumber: "WO-B"}, wantError: true},
		{name: "missing SPP source fails closed", destination: OutCustomer, input: Input{SPPNumber: "SPP-B"}, wantError: true},
		{name: "blank WO source fails closed", destination: OutWorkshop, workOrder: " ", input: Input{WorkOrderNumber: "WO-B"}, wantError: true},
		{name: "blank SPP source fails closed", destination: OutCustomer, spp: " ", input: Input{SPPNumber: "SPP-B"}, wantError: true},
		{name: "missing departure fails closed", missing: true, input: Input{SPPNumber: "SPP-B"}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectBegin()
			tx, err := sqlx.NewDb(db, "sqlmock").Beginx()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			query := mock.ExpectQuery(`WHERE transaction_id = \$1 AND lambung_unit_id = \$2 AND direction = 'OUT'`).WithArgs(int64(19), int64(7))
			if test.missing {
				query.WillReturnError(sql.ErrNoRows)
			} else {
				query.WillReturnRows(sqlmock.NewRows([]string{"out_destination", "work_order_number", "spp_number"}).
					AddRow(test.destination, test.workOrder, test.spp))
			}
			mock.ExpectRollback()
			test.input.Direction, test.input.NoLambung = DirectionIn, "SAMT119"
			source, err := validateReturningSource(context.Background(), tx, 19, 7, test.input)
			if test.wantError {
				if !errors.Is(err, ErrReturnDocumentMismatch) {
					t.Fatalf("expected document-type rejection, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("same-type return must not require the same document number: %v", err)
			}
			if !test.wantError && test.spp != nil && source != test.spp {
				t.Fatal("SPP returns must retain the original canonical SPP for locking")
			}
			tx.Rollback()
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
