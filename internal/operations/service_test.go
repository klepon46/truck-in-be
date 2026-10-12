package operations

import (
	"context"
	"regexp"
	"testing"

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
