package operations

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestDashboardMapsAggregateColumns(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT COUNT(*) AS total_units,
       COUNT(*) FILTER (WHERE m.direction = 'IN') AS units_in,
       COUNT(*) FILTER (WHERE m.direction = 'OUT') AS units_out,
       COUNT(*) FILTER (WHERE m.transaction_id IS NULL) AS units_no_status
FROM unit_snapshot u
LEFT JOIN movement_transactions m ON m.transaction_id = u.current_transaction_id
WHERE u.is_active = TRUE`)).
		WillReturnRows(sqlmock.NewRows([]string{"total_units", "units_in", "units_out", "units_no_status"}).
			AddRow(12, 5, 4, 3))

	result, err := NewService(sqlx.NewDb(db, "sqlmock")).Dashboard(context.Background())
	if err != nil {
		t.Fatalf("Dashboard() error = %v", err)
	}
	if result != (Dashboard{TotalUnits: 12, UnitsIn: 5, UnitsOut: 4, UnitsNoStatus: 3}) {
		t.Fatalf("Dashboard() = %#v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListTransactionsMapsCycleAndDepartureReferences(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM movement_transactions m `)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	now := time.Date(2026, time.October, 19, 7, 15, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT m.transaction_id, m.no_lambung_snapshot, m.no_polisi_snapshot, m.direction, m.in_category,
       m.out_destination, m.driver_name_snapshot, m.work_order_number, m.spp_number,
       m.customer_name_snapshot, m.note, m.actor_name_snapshot, m.occurred_at,
       c.cycle_id, c.out_transaction_id AS departure_transaction_id,
       departure.work_order_number AS departure_work_order_number,
       departure.spp_number AS departure_spp_number,
       COALESCE(d.duration_seconds, GREATEST(0, EXTRACT(EPOCH FROM NOW() - m.occurred_at)::BIGINT)) AS duration_seconds,
       d.completed_at AS status_completed_at
FROM movement_transactions m
LEFT JOIN movement_cycles c ON m.transaction_id = c.out_transaction_id OR m.transaction_id = c.in_transaction_id
LEFT JOIN movement_transactions departure ON departure.transaction_id = c.out_transaction_id
LEFT JOIN movement_status_durations d ON d.transaction_id = m.transaction_id
 ORDER BY m.transaction_id DESC LIMIT ? OFFSET ?`)).
		WithArgs(25, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"transaction_id", "no_lambung_snapshot", "no_polisi_snapshot", "direction", "in_category",
			"out_destination", "driver_name_snapshot", "work_order_number", "spp_number",
			"customer_name_snapshot", "note", "actor_name_snapshot", "occurred_at", "cycle_id",
			"departure_transaction_id", "departure_work_order_number", "departure_spp_number", "duration_seconds", "status_completed_at",
		}).AddRow(
			int64(20), "SAMT223", "DA 1234 AB", "IN", "WAITING_FOR_ASSIGNMENT", nil, nil, nil, nil,
			nil, nil, "satpam", now, int64(91), int64(19), "WO-001", nil, int64(1800), now.Add(30*time.Minute),
		))

	transactions, page, err := NewService(sqlx.NewDb(db, "sqlmock")).ListTransactions(context.Background(), TransactionFilter{Page: NormalizePage(1, 25)})
	if err != nil {
		t.Fatalf("ListTransactions() error = %v", err)
	}
	if page.Total != 1 || len(transactions) != 1 {
		t.Fatalf("ListTransactions() = %#v, page = %#v", transactions, page)
	}
	transaction := transactions[0]
	if transaction.CycleID == nil || *transaction.CycleID != 91 || transaction.DepartureTransactionID == nil || *transaction.DepartureTransactionID != 19 || transaction.DepartureWorkOrderNumber == nil || *transaction.DepartureWorkOrderNumber != "WO-001" {
		t.Fatalf("transaction = %#v", transaction)
	}
	if transaction.DurationSeconds != 1800 || transaction.StatusCompletedAt == nil || !transaction.StatusCompletedAt.Equal(now.Add(30*time.Minute)) {
		t.Fatalf("transaction duration = %d, completedAt = %#v", transaction.DurationSeconds, transaction.StatusCompletedAt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
