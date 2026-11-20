package movement

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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
	PriorSPPUsed         bool      `db:"prior_spp_used"`
	UsedByOtherUnit      bool      `db:"used_by_other_unit"`
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
       unitlog_spp_completed($2) AS spp_completed,
       EXISTS (
           SELECT 1 FROM movement_transactions used WHERE used.spp_number = $2
        ) AS prior_spp_used,
        unitlog_spp_used_by_other_unit($2, u.lambung_unit_id) AS used_by_other_unit
FROM unit_snapshot u
LEFT JOIN movement_transactions current_m
  ON current_m.transaction_id = u.current_transaction_id AND current_m.lambung_unit_id = u.lambung_unit_id
LEFT JOIN movement_spp_return_context departure
  ON departure.in_transaction_id = current_m.transaction_id AND departure.lambung_unit_id = u.lambung_unit_id
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
	if number == "" {
		return ErrInvalidInput
	}
	if (state.CurrentTransactionID != nil && state.Direction == "") ||
		(state.Direction == DirectionIn && !isInCategory(state.InCategory)) {
		return ErrInvalidTransition
	}
	if err := ValidateTransition(Status{Direction: state.Direction, InCategory: state.InCategory}, DirectionOut, "", destination); err != nil {
		return err
	}
	if state.UsedByOtherUnit {
		return ErrSPPUsed
	}
	// Previous SPP/category/customer/completion facts are historical only.
	// Current scanned document requirements still apply to Customer.
	if destination == OutCustomer && strings.TrimSpace(document.CustomerID) == "" {
		return ErrCustomerMismatch
	}
	return nil
}

func allowedSPPOutDestinations(state sppEligibilityState, number string, document Document) []string {
	allowed := make([]string, 0, 2)
	for _, destination := range []string{OutCustomer, OutFillingShed} {
		if validateSPPDeparture(state, destination, number, document) == nil {
			allowed = append(allowed, destination)
		}
	}
	return allowed
}
