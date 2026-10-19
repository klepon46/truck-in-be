package movement

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func completeStatus(ctx context.Context, tx *sqlx.Tx, transactionID int64) error {
	result, err := tx.ExecContext(ctx, `
INSERT INTO movement_status_durations (transaction_id, duration_seconds, completed_at)
SELECT transaction_id,
       GREATEST(0, EXTRACT(EPOCH FROM NOW() - occurred_at)::BIGINT),
       NOW()
FROM movement_transactions
WHERE transaction_id = $1`, transactionID)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return fmt.Errorf("movement transaction %d was not found", transactionID)
	}
	return nil
}
