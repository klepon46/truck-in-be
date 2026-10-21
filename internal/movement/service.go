package movement

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

var (
	ErrUnitNotFound           = errors.New("unit not found")
	ErrIdempotencyConflict    = errors.New("idempotency key payload conflict")
	ErrWorkOrderUsed          = errors.New("work order has already been used")
	ErrSPPUsed                = errors.New("SPP has already been used")
	ErrSPPMismatch            = errors.New("SPP does not match the active SPP")
	ErrCustomerMismatch       = errors.New("customer does not match the active SPP")
	ErrExternalDocument       = errors.New("external document is invalid")
	ErrIntegrationUnavailable = errors.New("document integration is unavailable")
	ErrDocumentUnitMismatch   = errors.New("document vehicle or police number does not match Unit Lambung")
	ErrReturnDocumentMismatch = errors.New("arrival document does not match the departure")
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
	NoLambung    string
	NoPolisi     string
	DriverName   string
	CustomerID   string
	CustomerName string
}

type LookupDocument struct {
	NoLambung       string `json:"noLambung"`
	NoPolisi        string `json:"noPolisi"`
	DriverName      string `json:"driverName"`
	WorkOrderNumber string `json:"workOrderNumber,omitempty"`
	SPPNumber       string `json:"sppNumber,omitempty"`
	CustomerID      string `json:"customerId,omitempty"`
	CustomerName    string `json:"customerName,omitempty"`
}

type SPPLookupDocument struct {
	LookupDocument
	// Always present, including [] when this valid SPP cannot be used for OUT. Does not restrict returning IN.
	AllowedOutDestinations []string `json:"allowedOutDestinations" validate:"required" enums:"CUSTOMER,FILLING_SHED_KUIN" extensions:"!x-nullable"`
}

type Transaction struct {
	ID                       int64     `db:"transaction_id" json:"transactionId"`
	CycleID                  *int64    `db:"cycle_id" json:"cycleId" extensions:"x-nullable=true"`
	DepartureTransactionID   *int64    `db:"departure_transaction_id" json:"departureTransactionId" extensions:"x-nullable=true"`
	DepartureWorkOrderNumber *string   `db:"departure_work_order_number" json:"departureWorkOrderNumber" extensions:"x-nullable=true"`
	DepartureSPPNumber       *string   `db:"departure_spp_number" json:"departureSppNumber" extensions:"x-nullable=true"`
	NoLambung                string    `db:"no_lambung_snapshot" json:"noLambung"`
	NoPolisi                 string    `db:"no_polisi_snapshot" json:"noPolisi"`
	Direction                Direction `db:"direction" json:"direction"`
	InCategory               *string   `db:"in_category" json:"inCategory" extensions:"x-nullable=true"`
	OutDestination           *string   `db:"out_destination" json:"outDestination" extensions:"x-nullable=true"`
	DriverID                 *int64    `db:"driver_id" json:"driverId" extensions:"x-nullable=true"`
	DriverName               *string   `db:"driver_name_snapshot" json:"driverName" extensions:"x-nullable=true"`
	WorkOrderNumber          *string   `db:"work_order_number" json:"workOrderNumber" extensions:"x-nullable=true"`
	SPPNumber                *string   `db:"spp_number" json:"sppNumber" extensions:"x-nullable=true"`
	CustomerID               *string   `db:"customer_id" json:"customerId" extensions:"x-nullable=true"`
	CustomerName             *string   `db:"customer_name_snapshot" json:"customerName" extensions:"x-nullable=true"`
	Note                     *string   `db:"note" json:"note" extensions:"x-nullable=true"`
	Coordinates              *string   `db:"coordinates" json:"coordinates" extensions:"x-nullable=true"`
	ActorID                  string    `db:"actor_id" json:"actorId"`
	ActorName                string    `db:"actor_name_snapshot" json:"actorName"`
	OccurredAt               time.Time `db:"occurred_at" json:"occurredAt"`
	RequestHash              []byte    `db:"request_hash" json:"-"`
	LambungUnitID            int64     `db:"lambung_unit_id" json:"lambungUnitId"`
}

type Service struct {
	db                          *sqlx.DB
	refresher                   UnitRefresher
	documents                   Documents
	validateOtherDriverOnSubmit bool
}

type ServiceOption func(*Service)

// WithOtherDriverValidation controls submit-time checks only; the picker is unchanged.
func WithOtherDriverValidation(enabled bool) ServiceOption {
	return func(service *Service) { service.validateOtherDriverOnSubmit = enabled }
}

func NewService(db *sqlx.DB, refresher UnitRefresher, documents Documents, options ...ServiceOption) *Service {
	service := &Service{db: db, refresher: refresher, documents: documents, validateOtherDriverOnSubmit: true}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) LookupWorkshop(ctx context.Context, number string) (LookupDocument, error) {
	if s.documents == nil || strings.TrimSpace(number) == "" {
		return LookupDocument{}, ErrInvalidInput
	}
	document, err := s.documents.Workshop(ctx, strings.TrimSpace(number))
	if err != nil {
		return LookupDocument{}, errors.Join(ErrExternalDocument, err)
	}
	return s.lookupDocument(ctx, document, LookupDocument{WorkOrderNumber: strings.TrimSpace(number)})
}

func (s *Service) LookupSPP(ctx context.Context, number string) (SPPLookupDocument, error) {
	number = strings.TrimSpace(number)
	if s.documents == nil || number == "" {
		return SPPLookupDocument{}, ErrInvalidInput
	}
	document, err := s.documents.SPP(ctx, number)
	if err != nil {
		if errors.Is(err, ErrIntegrationUnavailable) {
			return SPPLookupDocument{}, err
		}
		return SPPLookupDocument{}, errors.Join(ErrExternalDocument, err)
	}
	if err := s.prepareDocumentUnit(ctx, document); err != nil {
		return SPPLookupDocument{}, err
	}
	state, err := readSPPState(ctx, s.db, document.NoLambung, number)
	if err != nil {
		return SPPLookupDocument{}, err
	}
	if err := validateDocumentUnit(document, snapshot{NoLambung: state.NoLambung, NoPolisi: state.NoPolisi}); err != nil {
		return SPPLookupDocument{}, err
	}
	return SPPLookupDocument{
		LookupDocument: LookupDocument{
			NoLambung: state.NoLambung, NoPolisi: state.NoPolisi, DriverName: document.DriverName,
			SPPNumber: number, CustomerID: document.CustomerID, CustomerName: document.CustomerName,
		},
		AllowedOutDestinations: allowedSPPOutDestinations(state, number, document),
	}, nil
}

func (s *Service) prepareDocumentUnit(ctx context.Context, document Document) error {
	if document.NoLambung == "" || document.NoPolisi == "" || document.DriverName == "" {
		return ErrExternalDocument
	}
	if s.refresher != nil {
		return s.refresher.EnsureKnown(ctx, document.NoLambung)
	}
	return nil
}

func (s *Service) lookupDocument(ctx context.Context, document Document, result LookupDocument) (LookupDocument, error) {
	if err := s.prepareDocumentUnit(ctx, document); err != nil {
		return LookupDocument{}, err
	}
	var unit snapshot
	err := s.db.GetContext(ctx, &unit, `
SELECT lambung_unit_id, no_lambung, no_polisi
FROM unit_snapshot
WHERE no_lambung = $1 AND is_active = TRUE`, document.NoLambung)
	if errors.Is(err, sql.ErrNoRows) {
		return LookupDocument{}, ErrUnitNotFound
	}
	if err != nil {
		return LookupDocument{}, err
	}
	if err := validateDocumentUnit(document, unit); err != nil {
		return LookupDocument{}, err
	}
	result.NoLambung = unit.NoLambung
	result.NoPolisi = unit.NoPolisi
	result.DriverName = document.DriverName
	return result, nil
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
	if err := validateDocumentUnit(document, snapshot); err != nil {
		return Transaction{}, err
	}
	if existing, found, err := s.findByKey(ctx, tx, idempotencyKey); err != nil {
		return Transaction{}, fmt.Errorf("recheck existing movement: %w", err)
	} else if found {
		return replay(existing, hash)
	}
	if err := ValidateTransition(snapshot.Status, input.Direction, input.InCategory, input.OutDestination); err != nil {
		return Transaction{}, err
	}
	if input.Direction == DirectionIn && snapshot.CurrentTransactionID != nil {
		if err := validateReturningSource(ctx, tx, *snapshot.CurrentTransactionID, input); err != nil {
			return Transaction{}, err
		}
	}
	if input.Direction == DirectionOut && input.SPPNumber != "" {
		state, err := readSPPState(ctx, tx, snapshot.NoLambung, input.SPPNumber)
		if err != nil {
			return Transaction{}, err
		}
		if err := validateSPPDeparture(state, input.OutDestination, input.SPPNumber, document); err != nil {
			return Transaction{}, fmt.Errorf("validate SPP departure: %w", err)
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
	if input.Direction == DirectionOut {
		cycle, err := beginCycle(ctx, tx, snapshot.ID, transaction)
		if err != nil {
			return Transaction{}, fmt.Errorf("start movement cycle: %w", err)
		}
		transaction.applyCycle(cycle)
	} else if snapshot.CurrentTransactionID != nil {
		cycle, err := closeCycle(ctx, tx, *snapshot.CurrentTransactionID, transaction.ID)
		if err != nil {
			return Transaction{}, fmt.Errorf("complete movement cycle: %w", err)
		}
		transaction.applyCycle(cycle)
	}
	if snapshot.CurrentTransactionID != nil {
		if err := completeStatus(ctx, tx, *snapshot.CurrentTransactionID); err != nil {
			return Transaction{}, fmt.Errorf("complete previous movement status: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE unit_snapshot SET current_transaction_id = $1 WHERE lambung_unit_id = $2`, transaction.ID, snapshot.ID); err != nil {
		return Transaction{}, fmt.Errorf("update unit snapshot: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Transaction{}, fmt.Errorf("commit movement transaction: %w", err)
	}
	return transaction, nil
}

func validateReturningSource(ctx context.Context, tx *sqlx.Tx, transactionID int64, input Input) error {
	var departure struct {
		Destination string  `db:"out_destination"`
		WorkOrder   *string `db:"work_order_number"`
		SPP         *string `db:"spp_number"`
	}
	if err := tx.GetContext(ctx, &departure, `
SELECT out_destination, work_order_number, spp_number
FROM movement_transactions
WHERE transaction_id = $1 AND direction = 'OUT'`, transactionID); err != nil {
		return ErrReturnDocumentMismatch
	}
	switch departure.Destination {
	case OutWorkshop:
		if departure.WorkOrder == nil || input.WorkOrderNumber != *departure.WorkOrder {
			return ErrReturnDocumentMismatch
		}
	case OutFillingShed, OutCustomer:
		if departure.SPP == nil || input.SPPNumber != *departure.SPP {
			return ErrReturnDocumentMismatch
		}
	case OutOther:
		if input.WorkOrderNumber != "" || input.SPPNumber != "" {
			return ErrReturnDocumentMismatch
		}
	default:
		return ErrReturnDocumentMismatch
	}
	return nil
}

func (t *Transaction) applyCycle(cycle movementCycle) {
	t.CycleID = &cycle.ID
	t.DepartureTransactionID = &cycle.DepartureTransactionID
	t.DepartureWorkOrderNumber = cycle.DepartureWorkOrderNumber
	t.DepartureSPPNumber = cycle.DepartureSPPNumber
}

func (s *Service) validateDocument(ctx context.Context, input *Input) (Document, error) {
	// Record has already checked the OTHER payload. Never bypass a WO/SPP lookup.
	if !s.validateOtherDriverOnSubmit && input.DriverID > 0 && input.WorkOrderNumber == "" && input.SPPNumber == "" {
		return Document{DriverName: input.DriverName}, nil
	}
	if s.documents == nil {
		return Document{}, ErrExternalDocument
	}
	switch {
	case input.WorkOrderNumber != "":
		document, err := s.documents.Workshop(ctx, input.WorkOrderNumber)
		if err != nil {
			return Document{}, errors.Join(ErrExternalDocument, fmt.Errorf("validate workshop document: %w", err))
		}
		if document.NoLambung == "" || document.NoPolisi == "" || document.DriverName == "" || !strings.EqualFold(document.NoLambung, input.NoLambung) {
			return Document{}, ErrExternalDocument
		}
		return document, nil
	case input.SPPNumber != "":
		document, err := s.documents.SPP(ctx, input.SPPNumber)
		if err != nil {
			if errors.Is(err, ErrIntegrationUnavailable) {
				return Document{}, fmt.Errorf("validate SPP document: %w", err)
			}
			return Document{}, errors.Join(ErrExternalDocument, fmt.Errorf("validate SPP document: %w", err))
		}
		if document.NoLambung == "" || document.NoPolisi == "" || document.DriverName == "" || !strings.EqualFold(document.NoLambung, input.NoLambung) {
			return Document{}, ErrExternalDocument
		}
		return document, nil
	case input.DriverID != 0:
		if err := s.documents.Driver(ctx, input.DriverID, input.DriverName); err != nil {
			return Document{}, errors.Join(ErrExternalDocument, fmt.Errorf("validate driver: %w", err))
		}
		return Document{DriverName: input.DriverName}, nil
	default:
		return Document{}, ErrInvalidInput
	}
}

func validateDocumentUnit(document Document, unit snapshot) error {
	if document.NoLambung == "" {
		return nil
	}
	if !strings.EqualFold(document.NoLambung, unit.NoLambung) || !strings.EqualFold(strings.TrimSpace(document.NoPolisi), strings.TrimSpace(unit.NoPolisi)) {
		return errors.Join(ErrExternalDocument, ErrDocumentUnitMismatch)
	}
	return nil
}

type snapshot struct {
	ID                   int64  `db:"lambung_unit_id"`
	NoLambung            string `db:"no_lambung"`
	NoPolisi             string `db:"no_polisi"`
	CurrentTransactionID *int64 `db:"current_transaction_id"`
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

func insertTransaction(ctx context.Context, tx *sqlx.Tx, idempotencyKey string, hash []byte, unit snapshot, input Input, document Document, actorID, actorName string) (Transaction, error) {
	var transaction Transaction
	err := tx.GetContext(ctx, &transaction, `
INSERT INTO movement_transactions (
    idempotency_key, request_hash, lambung_unit_id, no_lambung_snapshot, no_polisi_snapshot,
    direction, in_category, out_destination, driver_id, driver_name_snapshot,
    work_order_number, spp_number, customer_id, customer_name_snapshot, note,
    actor_id, actor_name_snapshot, coordinates
) VALUES (
    $1, $2, $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, 0), NULLIF($10, ''),
    NULLIF($11, ''), NULLIF($12, ''), NULLIF($13, ''), NULLIF($14, ''), NULLIF($15, ''), $16, $17, NULLIF($18, '')
) RETURNING transaction_id, request_hash, lambung_unit_id, no_lambung_snapshot,
    no_polisi_snapshot, direction, in_category, out_destination, driver_id, driver_name_snapshot,
    work_order_number, spp_number, customer_id, customer_name_snapshot, note, actor_id,
    actor_name_snapshot, occurred_at, coordinates`,
		idempotencyKey, hash, unit.ID, unit.NoLambung, unit.NoPolisi, input.Direction, input.InCategory,
		input.OutDestination, input.DriverID, document.DriverName, input.WorkOrderNumber, input.SPPNumber,
		document.CustomerID, document.CustomerName, input.Note, actorID, actorName, input.Coordinates)
	return transaction, err
}

type queryer interface {
	GetContext(context.Context, any, string, ...any) error
}

func (s *Service) findByKey(ctx context.Context, query queryer, idempotencyKey string) (Transaction, bool, error) {
	var transaction Transaction
	err := query.GetContext(ctx, &transaction, `
SELECT m.transaction_id, m.request_hash, m.lambung_unit_id, m.no_lambung_snapshot,
       m.no_polisi_snapshot, m.direction, m.in_category, m.out_destination, m.driver_id, m.driver_name_snapshot,
       m.work_order_number, m.spp_number, m.customer_id, m.customer_name_snapshot, m.note, m.actor_id,
       m.actor_name_snapshot, m.occurred_at, m.coordinates, c.cycle_id,
       c.out_transaction_id AS departure_transaction_id,
       departure.work_order_number AS departure_work_order_number,
       departure.spp_number AS departure_spp_number
FROM movement_transactions m
LEFT JOIN movement_cycles c ON m.transaction_id = c.out_transaction_id OR m.transaction_id = c.in_transaction_id
LEFT JOIN movement_transactions departure ON departure.transaction_id = c.out_transaction_id
WHERE m.idempotency_key = $1`, idempotencyKey)
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
