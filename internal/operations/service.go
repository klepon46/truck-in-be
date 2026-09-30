package operations

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type Page struct {
	Number int `json:"page"`
	Size   int `json:"pageSize"`
	Total  int `json:"totalItems"`
}

func (p Page) Offset() int { return (p.Number - 1) * p.Size }

func NormalizePage(page, size int) Page {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 25
	}
	if size > 100 {
		size = 100
	}
	return Page{Number: page, Size: size}
}

type Dashboard struct {
	TotalUnits    int `json:"totalUnits"`
	UnitsIn       int `json:"unitsIn"`
	UnitsOut      int `json:"unitsOut"`
	UnitsNoStatus int `json:"unitsNoStatus"`
}

type UnitFilter struct {
	Page
	Query           string
	IncludeInactive bool
	Status          string
	Destination     string
}

type Unit struct {
	ID              int64      `db:"lambung_unit_id" json:"id"`
	NoLambung       string     `db:"no_lambung" json:"noLambung"`
	NoPolisi        string     `db:"no_polisi" json:"noPolisi"`
	IsActive        bool       `db:"is_active" json:"isActive"`
	Status          string     `json:"status"`
	Direction       *string    `db:"direction" json:"direction" extensions:"x-nullable=true"`
	InCategory      *string    `db:"in_category" json:"inCategory" extensions:"x-nullable=true"`
	OutDestination  *string    `db:"out_destination" json:"outDestination" extensions:"x-nullable=true"`
	DriverName      *string    `db:"driver_name_snapshot" json:"driverName" extensions:"x-nullable=true"`
	LastUpdatedAt   *time.Time `db:"occurred_at" json:"lastUpdatedAt" extensions:"x-nullable=true"`
	DurationSeconds int64      `db:"duration_seconds" json:"durationSeconds"`
}

type TransactionFilter struct {
	Page
	UnitID    *int64
	Query     string
	Direction string
	Detail    string
	From      *time.Time
	To        *time.Time
}

type Transaction struct {
	ID              int64     `db:"transaction_id" json:"transactionId"`
	NoLambung       string    `db:"no_lambung_snapshot" json:"noLambung"`
	NoPolisi        string    `db:"no_polisi_snapshot" json:"noPolisi"`
	Direction       string    `db:"direction" json:"direction"`
	InCategory      *string   `db:"in_category" json:"inCategory" extensions:"x-nullable=true"`
	OutDestination  *string   `db:"out_destination" json:"outDestination" extensions:"x-nullable=true"`
	DriverName      *string   `db:"driver_name_snapshot" json:"driverName" extensions:"x-nullable=true"`
	WorkOrderNumber *string   `db:"work_order_number" json:"workOrderNumber" extensions:"x-nullable=true"`
	SPPNumber       *string   `db:"spp_number" json:"sppNumber" extensions:"x-nullable=true"`
	CustomerName    *string   `db:"customer_name_snapshot" json:"customerName" extensions:"x-nullable=true"`
	Note            *string   `db:"note" json:"note" extensions:"x-nullable=true"`
	ActorName       string    `db:"actor_name_snapshot" json:"actorName"`
	OccurredAt      time.Time `db:"occurred_at" json:"occurredAt"`
}

type Service struct{ db *sqlx.DB }

func NewService(db *sqlx.DB) *Service { return &Service{db: db} }

func (s *Service) Dashboard(ctx context.Context) (Dashboard, error) {
	var result Dashboard
	err := s.db.GetContext(ctx, &result, `
SELECT COUNT(*) AS total_units,
       COUNT(*) FILTER (WHERE m.direction = 'IN') AS units_in,
       COUNT(*) FILTER (WHERE m.direction = 'OUT') AS units_out,
       COUNT(*) FILTER (WHERE m.transaction_id IS NULL) AS units_no_status
FROM unit_snapshot u
LEFT JOIN movement_transactions m ON m.transaction_id = u.current_transaction_id
WHERE u.is_active = TRUE`)
	return result, err
}

func (s *Service) ListUnits(ctx context.Context, filter UnitFilter) ([]Unit, Page, error) {
	where, args := unitWhere(filter)
	var total int
	if err := s.db.GetContext(ctx, &total, s.db.Rebind(`SELECT COUNT(*) FROM unit_snapshot u LEFT JOIN movement_transactions m ON m.transaction_id = u.current_transaction_id `+where), args...); err != nil {
		return nil, filter.Page, err
	}
	args = append(args, filter.Size, filter.Offset())
	var units []Unit
	err := s.db.SelectContext(ctx, &units, s.db.Rebind(`
SELECT u.lambung_unit_id, u.no_lambung, u.no_polisi, u.is_active, m.direction, m.in_category,
       m.out_destination, m.driver_name_snapshot, m.occurred_at,
       COALESCE(EXTRACT(EPOCH FROM NOW() - m.occurred_at)::BIGINT, 0) AS duration_seconds
FROM unit_snapshot u
LEFT JOIN movement_transactions m ON m.transaction_id = u.current_transaction_id `+where+`
ORDER BY u.no_lambung ASC LIMIT ? OFFSET ?`), args...)
	for index := range units {
		units[index].Status = status(units[index].Direction, units[index].InCategory)
	}
	filter.Total = total
	return units, filter.Page, err
}

func (s *Service) Unit(ctx context.Context, id int64) (Unit, error) {
	var unit Unit
	err := s.db.GetContext(ctx, &unit, `
SELECT u.lambung_unit_id, u.no_lambung, u.no_polisi, u.is_active, m.direction, m.in_category,
       m.out_destination, m.driver_name_snapshot, m.occurred_at,
       COALESCE(EXTRACT(EPOCH FROM NOW() - m.occurred_at)::BIGINT, 0) AS duration_seconds
FROM unit_snapshot u LEFT JOIN movement_transactions m ON m.transaction_id = u.current_transaction_id
WHERE u.lambung_unit_id = $1`, id)
	if err == nil {
		unit.Status = status(unit.Direction, unit.InCategory)
	}
	return unit, err
}

func (s *Service) ListTransactions(ctx context.Context, filter TransactionFilter) ([]Transaction, Page, error) {
	where, args := transactionWhere(filter)
	var total int
	if err := s.db.GetContext(ctx, &total, s.db.Rebind(`SELECT COUNT(*) FROM movement_transactions m `+where), args...); err != nil {
		return nil, filter.Page, err
	}
	args = append(args, filter.Size, filter.Offset())
	var transactions []Transaction
	err := s.db.SelectContext(ctx, &transactions, s.db.Rebind(`
SELECT m.transaction_id, m.no_lambung_snapshot, m.no_polisi_snapshot, m.direction, m.in_category,
       m.out_destination, m.driver_name_snapshot, m.work_order_number, m.spp_number,
       m.customer_name_snapshot, m.note, m.actor_name_snapshot, m.occurred_at
FROM movement_transactions m `+where+` ORDER BY m.transaction_id DESC LIMIT ? OFFSET ?`), args...)
	filter.Total = total
	return transactions, filter.Page, err
}

func unitWhere(filter UnitFilter) (string, []any) {
	var clauses []string
	var args []any
	if !filter.IncludeInactive {
		clauses = append(clauses, "u.is_active = TRUE")
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		clauses = append(clauses, "(u.no_lambung ILIKE ? OR u.no_polisi ILIKE ?)")
		args = append(args, "%"+query+"%", "%"+query+"%")
	}
	switch filter.Status {
	case "IN", "OUT":
		clauses = append(clauses, "m.direction = ?")
		args = append(args, filter.Status)
	case "NULL":
		clauses = append(clauses, "m.transaction_id IS NULL")
	}
	if filter.Destination != "" {
		clauses = append(clauses, "m.out_destination = ?")
		args = append(args, filter.Destination)
	}
	return where(clauses), args
}

func transactionWhere(filter TransactionFilter) (string, []any) {
	var clauses []string
	var args []any
	if filter.UnitID != nil {
		clauses = append(clauses, "m.lambung_unit_id = ?")
		args = append(args, *filter.UnitID)
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		clauses = append(clauses, "(m.no_lambung_snapshot ILIKE ? OR m.no_polisi_snapshot ILIKE ?)")
		args = append(args, "%"+query+"%", "%"+query+"%")
	}
	if filter.Direction == "IN" || filter.Direction == "OUT" {
		clauses = append(clauses, "m.direction = ?")
		args = append(args, filter.Direction)
	}
	if filter.Detail != "" {
		clauses = append(clauses, "COALESCE(m.in_category, m.out_destination) = ?")
		args = append(args, filter.Detail)
	}
	if filter.From != nil {
		clauses = append(clauses, "m.occurred_at >= ?")
		args = append(args, *filter.From)
	}
	if filter.To != nil {
		clauses = append(clauses, "m.occurred_at < ?")
		args = append(args, *filter.To)
	}
	return where(clauses), args
}

func where(clauses []string) string {
	if len(clauses) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(clauses, " AND ")
}

func status(direction, category *string) string {
	if direction == nil {
		return "NULL"
	}
	if *direction == "IN" && category != nil {
		return fmt.Sprintf("IN - %s", *category)
	}
	return *direction
}
