package movement

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

var transactionColumns = []string{
	"transaction_id", "request_hash", "lambung_unit_id", "no_lambung_snapshot", "no_polisi_snapshot",
	"direction", "in_category", "out_destination", "driver_id", "driver_name_snapshot",
	"work_order_number", "spp_number", "customer_id", "customer_name_snapshot", "note", "actor_id",
	"actor_name_snapshot", "occurred_at",
}

func TestFindByKeyReturnsStoredTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	now := time.Date(2026, time.October, 5, 4, 21, 43, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT transaction_id, request_hash, lambung_unit_id, no_lambung_snapshot,
       no_polisi_snapshot, direction, in_category, out_destination, driver_id, driver_name_snapshot,
       work_order_number, spp_number, customer_id, customer_name_snapshot, note, actor_id,
       actor_name_snapshot, occurred_at
FROM movement_transactions WHERE idempotency_key = $1`)).
		WithArgs("96942009-f8c4-4dbf-b7e0-6d571765166a").
		WillReturnRows(sqlmock.NewRows(transactionColumns).AddRow(
			int64(19), []byte("request-hash"), int64(7), "SAMT119", "DA 1234 AB",
			"OUT", nil, "BENGKEL_LUAR", nil, "Driver", "WO-FKS/2026/10/0002", nil, nil, nil, nil,
			"12", "superadmin", now,
		))

	service := NewService(sqlx.NewDb(db, "sqlmock"), nil, nil)
	transaction, found, err := service.findByKey(context.Background(), service.db, "96942009-f8c4-4dbf-b7e0-6d571765166a")
	if err != nil {
		t.Fatalf("findByKey() error = %v", err)
	}
	if !found || transaction.ID != 19 || transaction.NoLambung != "SAMT119" {
		t.Fatalf("findByKey() = %#v, found=%t", transaction, found)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestInsertTransactionReturnsMappedFields(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	database := sqlx.NewDb(db, "sqlmock")
	mock.ExpectBegin()
	tx, err := database.BeginTxx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTxx() error = %v", err)
	}

	now := time.Date(2026, time.October, 5, 4, 21, 43, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO movement_transactions (
    idempotency_key, request_hash, lambung_unit_id, no_lambung_snapshot, no_polisi_snapshot,
    direction, in_category, out_destination, driver_id, driver_name_snapshot,
    work_order_number, spp_number, customer_id, customer_name_snapshot, note,
    actor_id, actor_name_snapshot
) VALUES (
    $1, $2, $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, 0), NULLIF($10, ''),
    NULLIF($11, ''), NULLIF($12, ''), NULLIF($13, ''), NULLIF($14, ''), NULLIF($15, ''), $16, $17
) RETURNING transaction_id, request_hash, lambung_unit_id, no_lambung_snapshot,
    no_polisi_snapshot, direction, in_category, out_destination, driver_id, driver_name_snapshot,
    work_order_number, spp_number, customer_id, customer_name_snapshot, note, actor_id,
    actor_name_snapshot, occurred_at`)).
		WithArgs(
			"96942009-f8c4-4dbf-b7e0-6d571765166a", []byte("request-hash"), int64(7), "SAMT119", "DA 1234 AB",
			DirectionOut, "", OutWorkshop, int64(0), "Driver", "WO-FKS/2026/10/0002", "", "", "", "", "12", "superadmin",
		).
		WillReturnRows(sqlmock.NewRows(transactionColumns).AddRow(
			int64(19), []byte("request-hash"), int64(7), "SAMT119", "DA 1234 AB",
			"OUT", nil, "BENGKEL_LUAR", nil, "Driver", "WO-FKS/2026/10/0002", nil, nil, nil, nil,
			"12", "superadmin", now,
		))
	mock.ExpectRollback()

	transaction, err := insertTransaction(context.Background(), tx, "96942009-f8c4-4dbf-b7e0-6d571765166a", []byte("request-hash"), snapshot{
		ID: 7, NoLambung: "SAMT119", NoPolisi: "DA 1234 AB",
	}, Input{
		Direction: DirectionOut, OutDestination: OutWorkshop, WorkOrderNumber: "WO-FKS/2026/10/0002",
	}, Document{DriverName: "Driver"}, "12", "superadmin")
	if err != nil {
		t.Fatalf("insertTransaction() error = %v", err)
	}
	if transaction.ID != 19 || transaction.NoLambung != "SAMT119" {
		t.Fatalf("insertTransaction() = %#v", transaction)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
