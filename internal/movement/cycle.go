package movement

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
)

var errOpenCycleNotFound = errors.New("open movement cycle not found")

type movementCycle struct {
	ID                       int64   `db:"cycle_id"`
	DepartureTransactionID   int64   `db:"departure_transaction_id"`
	DepartureWorkOrderNumber *string `db:"departure_work_order_number"`
	DepartureSPPNumber       *string `db:"departure_spp_number"`
}

func beginCycle(ctx context.Context, tx *sqlx.Tx, unitID int64, departure Transaction) (movementCycle, error) {
	var cycle movementCycle
	if err := tx.GetContext(ctx, &cycle.ID, `
INSERT INTO movement_cycles (lambung_unit_id, out_transaction_id)
VALUES ($1, $2)
RETURNING cycle_id`, unitID, departure.ID); err != nil {
		return movementCycle{}, err
	}
	cycle.DepartureTransactionID = departure.ID
	cycle.DepartureWorkOrderNumber = departure.WorkOrderNumber
	cycle.DepartureSPPNumber = departure.SPPNumber
	return cycle, nil
}

func closeCycle(ctx context.Context, tx *sqlx.Tx, departureTransactionID, arrivalTransactionID int64) (movementCycle, error) {
	var cycle movementCycle
	if err := tx.GetContext(ctx, &cycle, `
SELECT c.cycle_id, c.out_transaction_id AS departure_transaction_id,
       departure.work_order_number AS departure_work_order_number,
       departure.spp_number AS departure_spp_number
FROM movement_cycles c
JOIN movement_transactions departure ON departure.transaction_id = c.out_transaction_id
WHERE c.out_transaction_id = $1 AND c.in_transaction_id IS NULL
	FOR UPDATE`, departureTransactionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return movementCycle{}, errOpenCycleNotFound
		}
		return movementCycle{}, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE movement_cycles
SET in_transaction_id = $1, completed_at = NOW()
WHERE cycle_id = $2 AND in_transaction_id IS NULL`, arrivalTransactionID, cycle.ID)
	if err != nil {
		return movementCycle{}, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return movementCycle{}, err
	}
	if updated != 1 {
		return movementCycle{}, errOpenCycleNotFound
	}
	return cycle, nil
}
