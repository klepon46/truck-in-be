package movement

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

var (
	ErrUnitNotFound        = errors.New("unit not found")
	ErrIdempotencyConflict = errors.New("idempotency key payload conflict")
	ErrWorkOrderUsed       = errors.New("work order has already been used")
	ErrSPPUsed             = errors.New("SPP has already been used")
	ErrSPPMismatch         = errors.New("SPP does not match the active SPP")
	ErrCustomerMismatch    = errors.New("customer does not match the active SPP")
	ErrExternalDocument    = errors.New("external document is invalid")
)

type UnitRefresher interface {
	EnsureKnown(context.Context, string) error
}

type Documents interface {
	Workshop(context.Context, string) (Document, error)
	SPP(context.Context, string) (Document, error)
	Driver(context.Context, int64, string) error
}

type Document struct {
	DriverName   string
	CustomerID   string
	CustomerName string
}

type Transaction struct {
	ID              int64     `db:"transaction_id" json:"transactionId"`
	NoLambung       string    `db:"no_lambung_snapshot" json:"noLambung"`
	NoPolisi        string    `db:"no_polisi_snapshot" json:"noPolisi"`
	Direction       Direction `db:"direction" json:"direction"`
	InCategory      *string   `db:"in_category" json:"inCategory" extensions:"x-nullable=true"`
	OutDestination  *string   `db:"out_destination" json:"outDestination" extensions:"x-nullable=true"`
	DriverID        *int64    `db:"driver_id" json:"driverId" extensions:"x-nullable=true"`
	DriverName      *string   `db:"driver_name_snapshot" json:"driverName" extensions:"x-nullable=true"`
	WorkOrderNumber *string   `db:"work_order_number" json:"workOrderNumber" extensions:"x-nullable=true"`
	SPPNumber       *string   `db:"spp_number" json:"sppNumber" extensions:"x-nullable=true"`
	CustomerID      *string   `db:"customer_id" json:"customerId" extensions:"x-nullable=true"`
	CustomerName    *string   `db:"customer_name_snapshot" json:"customerName" extensions:"x-nullable=true"`
	Note            *string   `db:"note" json:"note" extensions:"x-nullable=true"`
	ActorID         string    `db:"actor_id" json:"actorId"`
	ActorName       string    `db:"actor_name_snapshot" json:"actorName"`
	OccurredAt      time.Time `db:"occurred_at" json:"occurredAt"`
	RequestHash     []byte    `db:"request_hash" json:"-"`
	LambungUnitID   int64     `db:"lambung_unit_id" json:"lambungUnitId"`
}

type Service struct {
	db        *sqlx.DB
	refresher UnitRefresher
	documents Documents
}

func NewService(db *sqlx.DB, refresher UnitRefresher, documents Documents) *Service {
	return &Service{db: db, refresher: refresher, documents: documents}
}

func (s *Service) Record(ctx context.Context, idempotencyKey string, input Input, actorID, actorName string) (Transaction, error) {
	input.Normalize()
	if err := input.Validate(); err != nil || idempotencyKey == "" || actorID == "" || actorName == "" {
		return Transaction{}, ErrInvalidInput
	}
	hash, err := requestHash(input)
	if err != nil {
		return Transaction{}, err
	}
	if existing, found, err := s.findByKey(ctx, s.db, idempotencyKey); err != nil {
		return Transaction{}, fmt.Errorf("look up existing movement: %w", err)
	} else if found {
		return replay(existing, hash)
	}

	if s.refresher != nil {
		if err := s.refresher.EnsureKnown(ctx, input.NoLambung); err != nil {
			return Transaction{}, fmt.Errorf("refresh Unit Lambung snapshot: %w", err)
		}
	}
	document, err := s.validateDocument(ctx, &input)
	if err != nil {
		return Transaction{}, fmt.Errorf("validate movement document: %w", err)
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return Transaction{}, fmt.Errorf("start movement transaction: %w", err)
	}
	defer tx.Rollback()

	snapshot, err := lockSnapshot(ctx, tx, input.NoLambung)
	if errors.Is(err, ErrUnitNotFound) {
		return Transaction{}, ErrUnitNotFound
	}
	if err != nil {
		return Transaction{}, fmt.Errorf("lock unit snapshot: %w", err)
	}
	if existing, found, err := s.findByKey(ctx, tx, idempotencyKey); err != nil {
		return Transaction{}, fmt.Errorf("recheck existing movement: %w", err)
	} else if found {
		return replay(existing, hash)
	}
	if err := ValidateTransition(snapshot.Status, input.Direction, input.InCategory, input.OutDestination); err != nil {
		return Transaction{}, err
	}
	if input.Direction == DirectionOut && input.OutDestination == OutCustomer {
		if err := validateActiveSPP(ctx, tx, snapshot.CurrentTransactionID, input.SPPNumber, document); err != nil {
			return Transaction{}, fmt.Errorf("validate active SPP: %w", err)
		}
	}

	transaction, err := insertTransaction(ctx, tx, idempotencyKey, hash, snapshot, input, document, actorID, actorName)
	if err != nil {
		var postgresError *pq.Error
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			if postgresError.Constraint == "movement_transactions_work_order_number_uidx" {
				return Transaction{}, ErrWorkOrderUsed
			}
			if postgresError.Constraint == "movement_transactions_filling_shed_spp_number_uidx" {
				return Transaction{}, ErrSPPUsed
			}
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				return Transaction{}, rollbackErr
			}
			if existing, found, lookupErr := s.findByKey(ctx, s.db, idempotencyKey); lookupErr == nil && found {
				return replay(existing, hash)
			}
		}
		return Transaction{}, fmt.Errorf("insert movement transaction: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE unit_snapshot SET current_transaction_id = $1 WHERE lambung_unit_id = $2`, transaction.ID, snapshot.ID); err != nil {
		return Transaction{}, fmt.Errorf("update unit snapshot: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Transaction{}, fmt.Errorf("commit movement transaction: %w", err)
	}
	return transaction, nil
}

func (s *Service) validateDocument(ctx context.Context, input *Input) (Document, error) {
	if input.Direction == DirectionIn {
		return Document{}, nil
	}
	if s.documents == nil {
		return Document{}, ErrExternalDocument
	}
	switch input.OutDestination {
	case OutWorkshop:
		document, err := s.documents.Workshop(ctx, input.WorkOrderNumber)
		if err != nil {
			return Document{}, errors.Join(ErrExternalDocument, fmt.Errorf("validate workshop document: %w", err))
		}
		if document.DriverName == "" {
			return Document{}, ErrExternalDocument
		}
		return document, nil
	case OutFillingShed, OutCustomer:
		document, err := s.documents.SPP(ctx, input.SPPNumber)
		if err != nil {
			return Document{}, errors.Join(ErrExternalDocument, fmt.Errorf("validate SPP document: %w", err))
		}
		if document.DriverName == "" {
			return Document{}, ErrExternalDocument
		}
		return document, nil
	case OutOther:
		if err := s.documents.Driver(ctx, input.DriverID, input.DriverName); err != nil {
			return Document{}, errors.Join(ErrExternalDocument, fmt.Errorf("validate driver: %w", err))
		}
		return Document{DriverName: input.DriverName}, nil
	default:
		return Document{}, ErrInvalidInput
	}
}

type snapshot struct {
	ID                   int64
	NoLambung            string
	NoPolisi             string
	CurrentTransactionID *int64
	Status               Status
}

func lockSnapshot(ctx context.Context, tx *sqlx.Tx, noLambung string) (snapshot, error) {
	var value snapshot
	var direction *Direction
	var category *string
	err := tx.QueryRowxContext(ctx, `
SELECT u.lambung_unit_id, u.no_lambung, u.no_polisi, u.current_transaction_id,
       m.direction, m.in_category
FROM unit_snapshot u
LEFT JOIN movement_transactions m ON m.transaction_id = u.current_transaction_id
WHERE u.no_lambung = $1 AND u.is_active = TRUE
FOR UPDATE OF u`, noLambung).Scan(&value.ID, &value.NoLambung, &value.NoPolisi, &value.CurrentTransactionID, &direction, &category)
	if errors.Is(err, sql.ErrNoRows) {
		return snapshot{}, ErrUnitNotFound
	}
	if err != nil {
		return snapshot{}, err
	}
	if direction != nil {
		value.Status.Direction = *direction
	}
	if category != nil {
		value.Status.InCategory = *category
	}
	return value, nil
}

func validateActiveSPP(ctx context.Context, tx *sqlx.Tx, currentTransactionID *int64, sppNumber string, current Document) error {
	if currentTransactionID == nil {
		return ErrSPPMismatch
	}
	var previous struct {
		Number     string  `db:"spp_number"`
		CustomerID *string `db:"customer_id"`
	}
	err := tx.GetContext(ctx, &previous, `
SELECT spp_number, customer_id
FROM movement_transactions
WHERE lambung_unit_id = (
    SELECT lambung_unit_id FROM movement_transactions WHERE transaction_id = $1
) AND direction = 'OUT' AND spp_number IS NOT NULL AND transaction_id < $1
ORDER BY transaction_id DESC LIMIT 1`, *currentTransactionID)
	if err != nil {
		return ErrSPPMismatch
	}
	if previous.Number != sppNumber {
		return ErrSPPMismatch
	}
	if previous.CustomerID == nil || *previous.CustomerID == "" || current.CustomerID == "" || *previous.CustomerID != current.CustomerID {
		return ErrCustomerMismatch
	}
	return nil
}

func insertTransaction(ctx context.Context, tx *sqlx.Tx, idempotencyKey string, hash []byte, unit snapshot, input Input, document Document, actorID, actorName string) (Transaction, error) {
	var transaction Transaction
	err := tx.GetContext(ctx, &transaction, `
INSERT INTO movement_transactions (
    idempotency_key, request_hash, lambung_unit_id, no_lambung_snapshot, no_polisi_snapshot,
    direction, in_category, out_destination, driver_id, driver_name_snapshot,
    work_order_number, spp_number, customer_id, customer_name_snapshot, note,
    actor_id, actor_name_snapshot
) VALUES (
    $1, $2, $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, 0), NULLIF($10, ''),
    NULLIF($11, ''), NULLIF($12, ''), NULLIF($13, ''), NULLIF($14, ''), NULLIF($15, ''), $16, $17
) RETURNING transaction_id, idempotency_key, request_hash, lambung_unit_id, no_lambung_snapshot,
    no_polisi_snapshot, direction, in_category, out_destination, driver_id, driver_name_snapshot,
    work_order_number, spp_number, customer_id, customer_name_snapshot, note, actor_id,
    actor_name_snapshot, occurred_at`,
		idempotencyKey, hash, unit.ID, unit.NoLambung, unit.NoPolisi, input.Direction, input.InCategory,
		input.OutDestination, input.DriverID, document.DriverName, input.WorkOrderNumber, input.SPPNumber,
		document.CustomerID, document.CustomerName, input.Note, actorID, actorName)
	return transaction, err
}

type queryer interface {
	GetContext(context.Context, any, string, ...any) error
}

func (s *Service) findByKey(ctx context.Context, query queryer, idempotencyKey string) (Transaction, bool, error) {
	var transaction Transaction
	err := query.GetContext(ctx, &transaction, `
SELECT transaction_id, idempotency_key, request_hash, lambung_unit_id, no_lambung_snapshot,
       no_polisi_snapshot, direction, in_category, out_destination, driver_id, driver_name_snapshot,
       work_order_number, spp_number, customer_id, customer_name_snapshot, note, actor_id,
       actor_name_snapshot, occurred_at
FROM movement_transactions WHERE idempotency_key = $1`, idempotencyKey)
	if errors.Is(err, sql.ErrNoRows) {
		return Transaction{}, false, nil
	}
	return transaction, err == nil, err
}

func replay(transaction Transaction, hash []byte) (Transaction, error) {
	if string(transaction.RequestHash) != string(hash) {
		return Transaction{}, ErrIdempotencyConflict
	}
	return transaction, nil
}

func requestHash(input Input) ([]byte, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("hash movement request: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return hash[:], nil
}
