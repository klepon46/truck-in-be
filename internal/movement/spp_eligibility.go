package movement

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type sppEligibilityState struct {
	UnitID               int64     `db:"lambung_unit_id"`
	NoLambung            string    `db:"no_lambung"`
	NoPolisi             string    `db:"no_polisi"`
	CurrentTransactionID *int64    `db:"current_transaction_id"`
	Direction            Direction `db:"direction"`
	InCategory           string    `db:"in_category"`
	ActiveSPP            *string   `db:"active_spp"`
	ActiveCustomerID     *string   `db:"active_customer_id"`
	FillingShedUsed      bool      `db:"filling_shed_used"`
	SPPCompleted         bool      `db:"spp_completed"`
}

// One SELECT keeps status, cycle correlation, and lifecycle facts in one snapshot.
const sppEligibilityQuery = `
SELECT u.lambung_unit_id, u.no_lambung, u.no_polisi, u.current_transaction_id,
       COALESCE(current_m.direction, '') AS direction,
       COALESCE(current_m.in_category, '') AS in_category,
       departure.spp_number AS active_spp, departure.customer_id AS active_customer_id,
       EXISTS (
           SELECT 1 FROM movement_transactions used
           WHERE used.spp_number = $2 AND used.out_destination = 'FILLING_SHED_KUIN'
       ) AS filling_shed_used,
       EXISTS (
           SELECT 1 FROM movement_transactions history
           LEFT JOIN movement_cycles history_cycle
             ON history_cycle.out_transaction_id = history.transaction_id
            AND history_cycle.lambung_unit_id = history.lambung_unit_id
           LEFT JOIN movement_transactions returned
             ON returned.transaction_id = history_cycle.in_transaction_id
            AND returned.lambung_unit_id = history.lambung_unit_id AND returned.direction = 'IN'
           WHERE history.spp_number = $2 AND (
               (history.direction = 'IN' AND history.in_category <> 'WAITING_FOR_DELIVERY_TO_CUSTOMER')
               OR (history.direction = 'OUT' AND history.out_destination IN ('FILLING_SHED_KUIN', 'CUSTOMER')
                   AND returned.in_category <> 'WAITING_FOR_DELIVERY_TO_CUSTOMER')
           )
       ) AS spp_completed
FROM unit_snapshot u
LEFT JOIN movement_transactions current_m
  ON current_m.transaction_id = u.current_transaction_id AND current_m.lambung_unit_id = u.lambung_unit_id
LEFT JOIN movement_cycles active_cycle
  ON active_cycle.in_transaction_id = current_m.transaction_id AND active_cycle.lambung_unit_id = u.lambung_unit_id
 AND active_cycle.completed_at IS NOT NULL
 AND current_m.direction = 'IN' AND current_m.in_category = 'WAITING_FOR_DELIVERY_TO_CUSTOMER'
LEFT JOIN movement_transactions departure
  ON departure.transaction_id = active_cycle.out_transaction_id AND departure.lambung_unit_id = u.lambung_unit_id
 AND departure.direction = 'OUT' AND departure.out_destination IN ('FILLING_SHED_KUIN', 'CUSTOMER')
WHERE u.no_lambung = $1 AND u.is_active = TRUE`

func readSPPState(ctx context.Context, query queryer, noLambung, number string) (sppEligibilityState, error) {
	var state sppEligibilityState
	if err := query.GetContext(ctx, &state, sppEligibilityQuery, noLambung, number); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sppEligibilityState{}, ErrUnitNotFound
		}
		return sppEligibilityState{}, fmt.Errorf("read SPP eligibility state: %w", err)
	}
	return state, nil
}

func validateSPPDeparture(state sppEligibilityState, destination, number string, document Document) error {
	if destination != OutCustomer && destination != OutFillingShed {
		return ErrInvalidInput
	}
	if state.CurrentTransactionID != nil && state.Direction == "" {
		return ErrInvalidTransition
	}
	if err := ValidateTransition(Status{Direction: state.Direction, InCategory: state.InCategory}, DirectionOut, "", destination); err != nil {
		return err
	}
	if destination == OutFillingShed {
		if state.FillingShedUsed || state.SPPCompleted {
			return ErrSPPUsed
		}
		return nil
	}
	if state.SPPCompleted || state.ActiveSPP == nil || *state.ActiveSPP != number {
		return ErrSPPMismatch
	}
	if state.ActiveCustomerID == nil || *state.ActiveCustomerID == "" || document.CustomerID == "" || *state.ActiveCustomerID != document.CustomerID {
		return ErrCustomerMismatch
	}
	return nil
}

func allowedSPPOutDestinations(state sppEligibilityState, number string, document Document) []string {
	allowed := make([]string, 0, 1)
	for _, destination := range []string{OutCustomer, OutFillingShed} {
		if validateSPPDeparture(state, destination, number, document) == nil {
			allowed = append(allowed, destination)
		}
	}
	return allowed
}
