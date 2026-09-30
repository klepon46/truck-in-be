package movement

import "testing"

func TestValidateTransitionAllowsCustomerOnlyForActiveDeliverySPP(t *testing.T) {
	if err := ValidateTransition(Status{Direction: DirectionIn, InCategory: InWaitingForDelivery}, DirectionOut, "", OutCustomer); err != nil {
		t.Fatalf("ValidateTransition() error = %v", err)
	}
	if err := ValidateTransition(Status{Direction: DirectionIn, InCategory: InWaitingForDelivery}, DirectionOut, "", OutOther); err == nil {
		t.Fatal("ValidateTransition() allowed Other from an active delivery SPP")
	}
}

func TestValidateInputRequiresOtherDriverAndNote(t *testing.T) {
	input := Input{Direction: DirectionOut, OutDestination: OutOther}
	if err := input.Validate(); err == nil {
		t.Fatal("Validate() accepted Other without a driver and note")
	}
}

func TestValidateInputRejectsDocumentsForIn(t *testing.T) {
	input := Input{Direction: DirectionIn, InCategory: InWaitingForAssignment, WorkOrderNumber: "WO-1"}
	if err := input.Validate(); err == nil {
		t.Fatal("Validate() accepted an OUT document on IN")
	}
}

func TestValidateInputRejectsClientDriverForDocumentMovement(t *testing.T) {
	input := Input{
		NoLambung:       "SAMT223",
		Direction:       DirectionOut,
		OutDestination:  OutWorkshop,
		WorkOrderNumber: "WO-1",
		DriverID:        42,
	}
	if err := input.Validate(); err == nil {
		t.Fatal("Validate() accepted a client-supplied driver for a workshop movement")
	}
}
