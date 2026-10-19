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

var transactionColumns = []string{
	"transaction_id", "request_hash", "lambung_unit_id", "no_lambung_snapshot", "no_polisi_snapshot",
	"direction", "in_category", "out_destination", "driver_id", "driver_name_snapshot",
	"work_order_number", "spp_number", "customer_id", "customer_name_snapshot", "note", "actor_id",
	"actor_name_snapshot", "occurred_at",
}

var transactionCycleColumns = append(append([]string{}, transactionColumns...),
	"cycle_id", "departure_transaction_id", "departure_work_order_number", "departure_spp_number",
)

func TestFindByKeyReturnsStoredTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	now := time.Date(2026, time.October, 5, 4, 21, 43, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT m.transaction_id, m.request_hash, m.lambung_unit_id, m.no_lambung_snapshot,
       m.no_polisi_snapshot, m.direction, m.in_category, m.out_destination, m.driver_id, m.driver_name_snapshot,
       m.work_order_number, m.spp_number, m.customer_id, m.customer_name_snapshot, m.note, m.actor_id,
       m.actor_name_snapshot, m.occurred_at, c.cycle_id,
       c.out_transaction_id AS departure_transaction_id,
       departure.work_order_number AS departure_work_order_number,
       departure.spp_number AS departure_spp_number
FROM movement_transactions m
LEFT JOIN movement_cycles c ON m.transaction_id = c.out_transaction_id OR m.transaction_id = c.in_transaction_id
LEFT JOIN movement_transactions departure ON departure.transaction_id = c.out_transaction_id
WHERE m.idempotency_key = $1`)).
		WithArgs("96942009-f8c4-4dbf-b7e0-6d571765166a").
		WillReturnRows(sqlmock.NewRows(transactionCycleColumns).AddRow(
			int64(19), []byte("request-hash"), int64(7), "SAMT119", "DA 1234 AB",
			"OUT", nil, "BENGKEL_LUAR", nil, "Driver", "WO-FKS/2026/10/0002", nil, nil, nil, nil,
			"12", "superadmin", now, int64(91), int64(19), "WO-FKS/2026/10/0002", nil,
		))

	service := NewService(sqlx.NewDb(db, "sqlmock"), nil, nil)
	transaction, found, err := service.findByKey(context.Background(), service.db, "96942009-f8c4-4dbf-b7e0-6d571765166a")
	if err != nil {
		t.Fatalf("findByKey() error = %v", err)
	}
	if !found || transaction.ID != 19 || transaction.NoLambung != "SAMT119" || transaction.CycleID == nil || *transaction.CycleID != 91 {
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

func TestBeginCycleReturnsCycleAndDepartureReference(t *testing.T) {
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
	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO movement_cycles (lambung_unit_id, out_transaction_id)
VALUES ($1, $2)
RETURNING cycle_id`)).
		WithArgs(int64(7), int64(19)).
		WillReturnRows(sqlmock.NewRows([]string{"cycle_id"}).AddRow(int64(91)))
	mock.ExpectRollback()

	cycle, err := beginCycle(context.Background(), tx, 7, Transaction{ID: 19, WorkOrderNumber: pointer("WO-001")})
	if err != nil {
		t.Fatalf("beginCycle() error = %v", err)
	}
	if cycle.ID != 91 || cycle.DepartureTransactionID != 19 || cycle.DepartureWorkOrderNumber == nil || *cycle.DepartureWorkOrderNumber != "WO-001" {
		t.Fatalf("beginCycle() = %#v", cycle)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCloseCycleReturnsDepartureDocument(t *testing.T) {
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
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT c.cycle_id, c.out_transaction_id AS departure_transaction_id,
       departure.work_order_number AS departure_work_order_number,
       departure.spp_number AS departure_spp_number
FROM movement_cycles c
JOIN movement_transactions departure ON departure.transaction_id = c.out_transaction_id
WHERE c.out_transaction_id = $1 AND c.in_transaction_id IS NULL
FOR UPDATE`)).
		WithArgs(int64(19)).
		WillReturnRows(sqlmock.NewRows([]string{"cycle_id", "departure_transaction_id", "departure_work_order_number", "departure_spp_number"}).
			AddRow(int64(91), int64(19), nil, "SPP-001"))
	mock.ExpectExec(regexp.QuoteMeta(`
UPDATE movement_cycles
SET in_transaction_id = $1, completed_at = NOW()
WHERE cycle_id = $2 AND in_transaction_id IS NULL`)).
		WithArgs(int64(20), int64(91)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	cycle, err := closeCycle(context.Background(), tx, 19, 20)
	if err != nil {
		t.Fatalf("closeCycle() error = %v", err)
	}
	if cycle.ID != 91 || cycle.DepartureTransactionID != 19 || cycle.DepartureSPPNumber == nil || *cycle.DepartureSPPNumber != "SPP-001" {
		t.Fatalf("closeCycle() = %#v", cycle)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTransactionApplyCycleIncludesDepartureReferences(t *testing.T) {
	transaction := Transaction{ID: 20}
	transaction.applyCycle(movementCycle{
		ID:                       91,
		DepartureTransactionID:   19,
		DepartureWorkOrderNumber: pointer("WO-001"),
	})

	if transaction.CycleID == nil || *transaction.CycleID != 91 {
		t.Fatalf("CycleID = %#v", transaction.CycleID)
	}
	if transaction.DepartureTransactionID == nil || *transaction.DepartureTransactionID != 19 {
		t.Fatalf("DepartureTransactionID = %#v", transaction.DepartureTransactionID)
	}
	if transaction.DepartureWorkOrderNumber == nil || *transaction.DepartureWorkOrderNumber != "WO-001" {
		t.Fatalf("DepartureWorkOrderNumber = %#v", transaction.DepartureWorkOrderNumber)
	}
}

func TestCompleteStatusStoresFinishedDuration(t *testing.T) {
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
	mock.ExpectExec(regexp.QuoteMeta(`
INSERT INTO movement_status_durations (transaction_id, duration_seconds, completed_at)
SELECT transaction_id,
       GREATEST(0, EXTRACT(EPOCH FROM NOW() - occurred_at)::BIGINT),
       NOW()
FROM movement_transactions
WHERE transaction_id = $1`)).
		WithArgs(int64(19)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	if err := completeStatus(context.Background(), tx, 19); err != nil {
		t.Fatalf("completeStatus() error = %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateReturningSourceRequiresTheDepartureDocument(t *testing.T) {
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
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT out_destination, work_order_number, spp_number
FROM movement_transactions
WHERE transaction_id = $1 AND direction = 'OUT'`)).
		WithArgs(int64(19)).
		WillReturnRows(sqlmock.NewRows([]string{"out_destination", "work_order_number", "spp_number"}).
			AddRow(OutWorkshop, "WO-001", nil))
	mock.ExpectRollback()

	err = validateReturningSource(context.Background(), tx, 19, Input{Direction: DirectionIn, WorkOrderNumber: "WO-002"})
	if !errors.Is(err, ErrReturnDocumentMismatch) {
		t.Fatalf("validateReturningSource() error = %v, want %v", err, ErrReturnDocumentMismatch)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLookupWorkshopReturnsValidatedUnitDocument(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT lambung_unit_id, no_lambung, no_polisi
FROM unit_snapshot
WHERE no_lambung = $1 AND is_active = TRUE`)).
		WithArgs("SAMT119").
		WillReturnRows(sqlmock.NewRows([]string{"lambung_unit_id", "no_lambung", "no_polisi"}).
			AddRow(int64(7), "SAMT119", "DA 1234 AB"))

	result, err := NewService(sqlx.NewDb(db, "sqlmock"), nil, testDocuments{}).LookupWorkshop(context.Background(), "WO-001")
	if err != nil {
		t.Fatalf("LookupWorkshop() error = %v", err)
	}
	if result.NoLambung != "SAMT119" || result.NoPolisi != "DA 1234 AB" || result.DriverName != "Driver" || result.WorkOrderNumber != "WO-001" {
		t.Fatalf("LookupWorkshop() = %#v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLookupSPPReturnsValidatedCustomerDocument(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT lambung_unit_id, no_lambung, no_polisi
FROM unit_snapshot
WHERE no_lambung = $1 AND is_active = TRUE`)).
		WithArgs("SAMT119").
		WillReturnRows(sqlmock.NewRows([]string{"lambung_unit_id", "no_lambung", "no_polisi"}).
			AddRow(int64(7), "SAMT119", "DA 1234 AB"))

	result, err := NewService(sqlx.NewDb(db, "sqlmock"), nil, testDocuments{}).LookupSPP(context.Background(), "SPP-001")
	if err != nil {
		t.Fatalf("LookupSPP() error = %v", err)
	}
	if result.CustomerID != "CUST-1" || result.CustomerName != "Customer One" {
		t.Fatalf("LookupSPP() customer = %#v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type testDocuments struct{}

func (testDocuments) Workshop(context.Context, string) (Document, error) {
	return Document{NoLambung: "SAMT119", NoPolisi: "DA 1234 AB", DriverName: "Driver"}, nil
}

func (testDocuments) SPP(context.Context, string) (Document, error) {
	return Document{NoLambung: "SAMT119", NoPolisi: "DA 1234 AB", DriverName: "Driver", CustomerID: "CUST-1", CustomerName: "Customer One"}, nil
}

func (testDocuments) Driver(context.Context, int64, string) error {
	return nil
}

func TestRecordStartsCycleForOutMovement(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	now := time.Date(2026, time.October, 19, 8, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT m.transaction_id")).
		WithArgs("96942009-f8c4-4dbf-b7e0-6d571765166a").
		WillReturnRows(sqlmock.NewRows(transactionCycleColumns))
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT u.lambung_unit_id")).
		WithArgs("SAMT119").
		WillReturnRows(sqlmock.NewRows([]string{"lambung_unit_id", "no_lambung", "no_polisi", "current_transaction_id", "direction", "in_category"}).
			AddRow(int64(7), "SAMT119", "DA 1234 AB", int64(18), "IN", InWaitingForAssignment))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT m.transaction_id")).
		WithArgs("96942009-f8c4-4dbf-b7e0-6d571765166a").
		WillReturnRows(sqlmock.NewRows(transactionCycleColumns))
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
			"96942009-f8c4-4dbf-b7e0-6d571765166a", sqlmock.AnyArg(), int64(7), "SAMT119", "DA 1234 AB",
			DirectionOut, "", OutWorkshop, int64(0), "Driver", "WO-001", "", "", "", "", "12", "superadmin",
		).
		WillReturnRows(sqlmock.NewRows(transactionColumns).AddRow(
			int64(19), []byte("request-hash"), int64(7), "SAMT119", "DA 1234 AB",
			"OUT", nil, "BENGKEL_LUAR", nil, "Driver", "WO-001", nil, nil, nil, nil,
			"12", "superadmin", now,
		))
	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO movement_cycles (lambung_unit_id, out_transaction_id)
VALUES ($1, $2)
RETURNING cycle_id`)).
		WithArgs(int64(7), int64(19)).
		WillReturnRows(sqlmock.NewRows([]string{"cycle_id"}).AddRow(int64(91)))
	mock.ExpectExec(regexp.QuoteMeta(`
INSERT INTO movement_status_durations (transaction_id, duration_seconds, completed_at)
SELECT transaction_id,
       GREATEST(0, EXTRACT(EPOCH FROM NOW() - occurred_at)::BIGINT),
       NOW()
FROM movement_transactions
WHERE transaction_id = $1`)).
		WithArgs(int64(18)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE unit_snapshot SET current_transaction_id = $1 WHERE lambung_unit_id = $2`)).
		WithArgs(int64(19), int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	transaction, err := NewService(sqlx.NewDb(db, "sqlmock"), nil, testDocuments{}).Record(context.Background(), "96942009-f8c4-4dbf-b7e0-6d571765166a", Input{
		NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: OutWorkshop, WorkOrderNumber: "WO-001",
	}, "12", "superadmin")
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if transaction.CycleID == nil || *transaction.CycleID != 91 || transaction.DepartureTransactionID == nil || *transaction.DepartureTransactionID != 19 {
		t.Fatalf("Record() = %#v", transaction)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRecordCompletesCycleForReturningInMovement(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	now := time.Date(2026, time.October, 19, 9, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT m.transaction_id")).
		WithArgs("96942009-f8c4-4dbf-b7e0-6d571765166a").
		WillReturnRows(sqlmock.NewRows(transactionCycleColumns))
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT u.lambung_unit_id")).
		WithArgs("SAMT119").
		WillReturnRows(sqlmock.NewRows([]string{"lambung_unit_id", "no_lambung", "no_polisi", "current_transaction_id", "direction", "in_category"}).
			AddRow(int64(7), "SAMT119", "DA 1234 AB", int64(19), "OUT", nil))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT m.transaction_id")).
		WithArgs("96942009-f8c4-4dbf-b7e0-6d571765166a").
		WillReturnRows(sqlmock.NewRows(transactionCycleColumns))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT out_destination, work_order_number, spp_number
FROM movement_transactions
WHERE transaction_id = $1 AND direction = 'OUT'`)).
		WithArgs(int64(19)).
		WillReturnRows(sqlmock.NewRows([]string{"out_destination", "work_order_number", "spp_number"}).
			AddRow(OutWorkshop, "WO-001", nil))
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
			"96942009-f8c4-4dbf-b7e0-6d571765166a", sqlmock.AnyArg(), int64(7), "SAMT119", "DA 1234 AB",
			DirectionIn, InWaitingForAssignment, "", int64(0), "Driver", "WO-001", "", "", "", "", "12", "superadmin",
		).
		WillReturnRows(sqlmock.NewRows(transactionColumns).AddRow(
			int64(20), []byte("request-hash"), int64(7), "SAMT119", "DA 1234 AB",
			"IN", InWaitingForAssignment, nil, nil, "Driver", "WO-001", nil, nil, nil, nil,
			"12", "superadmin", now,
		))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT c.cycle_id, c.out_transaction_id AS departure_transaction_id,
       departure.work_order_number AS departure_work_order_number,
       departure.spp_number AS departure_spp_number
FROM movement_cycles c
JOIN movement_transactions departure ON departure.transaction_id = c.out_transaction_id
WHERE c.out_transaction_id = $1 AND c.in_transaction_id IS NULL
FOR UPDATE`)).
		WithArgs(int64(19)).
		WillReturnRows(sqlmock.NewRows([]string{"cycle_id", "departure_transaction_id", "departure_work_order_number", "departure_spp_number"}).
			AddRow(int64(91), int64(19), "WO-001", nil))
	mock.ExpectExec(regexp.QuoteMeta(`
UPDATE movement_cycles
SET in_transaction_id = $1, completed_at = NOW()
WHERE cycle_id = $2 AND in_transaction_id IS NULL`)).
		WithArgs(int64(20), int64(91)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`
INSERT INTO movement_status_durations (transaction_id, duration_seconds, completed_at)
SELECT transaction_id,
       GREATEST(0, EXTRACT(EPOCH FROM NOW() - occurred_at)::BIGINT),
       NOW()
FROM movement_transactions
WHERE transaction_id = $1`)).
		WithArgs(int64(19)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE unit_snapshot SET current_transaction_id = $1 WHERE lambung_unit_id = $2`)).
		WithArgs(int64(20), int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	transaction, err := NewService(sqlx.NewDb(db, "sqlmock"), nil, testDocuments{}).Record(context.Background(), "96942009-f8c4-4dbf-b7e0-6d571765166a", Input{
		NoLambung: "SAMT119", Direction: DirectionIn, InCategory: InWaitingForAssignment, WorkOrderNumber: "WO-001",
	}, "12", "superadmin")
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if transaction.CycleID == nil || *transaction.CycleID != 91 || transaction.DepartureTransactionID == nil || *transaction.DepartureTransactionID != 19 || transaction.DepartureWorkOrderNumber == nil || *transaction.DepartureWorkOrderNumber != "WO-001" {
		t.Fatalf("Record() = %#v", transaction)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func pointer(value string) *string {
	return &value
}
