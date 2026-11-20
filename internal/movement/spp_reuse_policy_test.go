package movement

import (
	"reflect"
	"testing"
)

func TestReusableSPPAllowsBothDestinationsAfterEveryIN(t *testing.T) {
	for _, category := range []string{InWaitingForAssignment, InWaitingForDelivery, InServiceAndRepair} {
		for _, history := range []string{"no trusted cycle", "used Filling Shed", "old terminal return"} {
			t.Run(category+"/"+history, func(t *testing.T) {
				id := int64(20)
				state := sppEligibilityState{CurrentTransactionID: &id, Direction: DirectionIn, InCategory: category}
				state.FillingShedUsed = history == "used Filling Shed"
				state.SPPCompleted = history == "old terminal return"
				state.PriorSPPUsed = true
				want := []string{OutCustomer, OutFillingShed}
				if got := allowedSPPOutDestinations(state, "SPP-A", Document{CustomerID: "C400"}); !reflect.DeepEqual(got, want) {
					t.Fatalf("same-unit reuse destinations %v, want %v", got, want)
				}
			})
		}
	}
}
