package movement

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
)

func TestCoordinatesValidationForAllMovementSources(t *testing.T) {
	sources := []Input{
		{Direction: DirectionIn, InCategory: InWaitingForAssignment, WorkOrderNumber: "WO-1"},
		{Direction: DirectionIn, InCategory: InWaitingForAssignment, SPPNumber: "SPP-1"},
		{Direction: DirectionIn, InCategory: InWaitingForAssignment, DriverID: 42, DriverName: "Driver", Note: "Arrival"},
		{Direction: DirectionOut, OutDestination: OutWorkshop, WorkOrderNumber: "WO-1"},
		{Direction: DirectionOut, OutDestination: OutFillingShed, SPPNumber: "SPP-1"},
		{Direction: DirectionOut, OutDestination: OutCustomer, SPPNumber: "SPP-1"},
		{Direction: DirectionOut, OutDestination: OutOther, DriverID: 42, DriverName: "Driver", Note: "Departure"},
	}
	for _, coordinates := range []string{"", "  ", "-3.319437,114.590889", "  -3.319437 , 114.590889  ", "90,180", "-90,-180", "0,0", "+.5,-.5"} {
		for _, source := range sources {
			input := source
			input.NoLambung = "SAMT119"
			input.Coordinates = coordinates
			input.Normalize()
			if err := input.Validate(); err != nil {
				t.Errorf("%s/%s/%s rejected coordinates %q: %v", input.Direction, input.OutDestination, input.InCategory, coordinates, err)
			}
			if strings.ContainsAny(input.Coordinates, " \t\n") {
				t.Error("normalization did not trim coordinate component whitespace")
			}
		}
	}
}

func TestCoordinatesRejectInvalidPairs(t *testing.T) {
	for _, coordinates := range []string{
		"-3.3", "-3.3,", ",114.5", "1,2,3", "91,114", "-91,114", "3,181", "3,-181",
		"latitude,longitude", "NaN,0", "0,Infinity", "-Inf,0", "1e2,0", "1e-3,0", "1 2,3",
		"-3;114", "0x1,2", ".,2", "1\n2,3", "0." + strings.Repeat("0", 100) + ",0",
	} {
		t.Run(coordinates, func(t *testing.T) {
			input := Input{NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: OutWorkshop, WorkOrderNumber: "WO-1", Coordinates: coordinates}
			input.Normalize()
			if !errors.Is(input.Validate(), ErrInvalidInput) {
				t.Error("accepted an invalid coordinate pair")
			}
		})
	}
}

func TestCoordinatesPreserveLegacyRequestHash(t *testing.T) {
	// Frozen pre-coordinate representation: do not derive this JSON from Input.
	legacyJSON := `{"NoLambung":"SAMT119","Direction":"OUT","InCategory":"","OutDestination":"BENGKEL_LUAR","WorkOrderNumber":"WO-1","SPPNumber":"","DriverID":0,"DriverName":"","Note":""}`
	want := sha256.Sum256([]byte(legacyJSON))
	input := Input{NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: OutWorkshop, WorkOrderNumber: "WO-1"}
	for _, coordinates := range []string{"", "   "} {
		input.Coordinates = coordinates
		input.Normalize()
		got, err := requestHash(input)
		if err != nil || !bytes.Equal(got, want[:]) {
			t.Fatal("adding optional coordinates changed an existing request hash")
		}
		if _, err := replay(Transaction{ID: 19, RequestHash: want[:]}, got); err != nil {
			t.Fatalf("legacy replay failed: %v", err)
		}
	}
	input.Coordinates = "-3.319437,114.590889"
	withCoordinates, err := requestHash(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := replay(Transaction{RequestHash: want[:]}, withCoordinates); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatal("adding coordinates to an old pending payload must conflict")
	}
	input.Coordinates = "-3.319438,114.590889"
	changed, err := requestHash(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := replay(Transaction{RequestHash: withCoordinates}, changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatal("changing coordinates must conflict under the same idempotency key")
	}
}
