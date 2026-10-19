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

func TestValidateTransitionPreventsConsecutiveDirections(t *testing.T) {
	tests := []struct {
		name        string
		status      Status
		direction   Direction
		category    string
		destination string
		wantError   bool
	}{
		{
			name:      "allows first IN without a cycle",
			direction: DirectionIn,
			category:  InWaitingForAssignment,
		},
		{
			name:      "allows IN after OUT",
			status:    Status{Direction: DirectionOut},
			direction: DirectionIn,
			category:  InWaitingForAssignment,
		},
		{
			name:      "rejects IN after IN",
			status:    Status{Direction: DirectionIn, InCategory: InWaitingForAssignment},
			direction: DirectionIn,
			category:  InWaitingForAssignment,
			wantError: true,
		},
		{
			name:        "rejects OUT after OUT",
			status:      Status{Direction: DirectionOut},
			direction:   DirectionOut,
			destination: OutOther,
			wantError:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateTransition(test.status, test.direction, test.category, test.destination)
			if (err != nil) != test.wantError {
				t.Fatalf("ValidateTransition() error = %v, wantError %t", err, test.wantError)
			}
		})
	}
}

func TestValidateInputRequiresOtherDriverAndNote(t *testing.T) {
	input := Input{Direction: DirectionOut, OutDestination: OutOther}
	if err := input.Validate(); err == nil {
		t.Fatal("Validate() accepted Other without a driver and note")
	}
}

func TestValidateInputAllowsDocumentAndOtherSourcesForIn(t *testing.T) {
	tests := []struct {
		name  string
		input Input
	}{
		{
			name:  "work order",
			input: Input{NoLambung: "A122", Direction: DirectionIn, InCategory: InWaitingForAssignment, WorkOrderNumber: "WO-1"},
		},
		{
			name:  "SPP",
			input: Input{NoLambung: "A122", Direction: DirectionIn, InCategory: InWaitingForAssignment, SPPNumber: "SPP-1"},
		},
		{
			name:  "other",
			input: Input{NoLambung: "A122", Direction: DirectionIn, InCategory: InWaitingForAssignment, DriverID: 42, DriverName: "Driver", Note: "Arrival note"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.input.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestValidateInputRejectsMixedInSources(t *testing.T) {
	input := Input{NoLambung: "A122", Direction: DirectionIn, InCategory: InWaitingForAssignment, WorkOrderNumber: "WO-1", DriverID: 42, DriverName: "Driver"}
	if err := input.Validate(); err == nil {
		t.Fatal("Validate() accepted mixed IN document and other source fields")
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
