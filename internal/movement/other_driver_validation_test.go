package movement

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

type otherDriverProbe struct {
	driverCalls, workshopCalls, sppCalls int
}

func (p *otherDriverProbe) Driver(context.Context, int64, string) error {
	p.driverCalls++
	return errors.New("integration returned status 404")
}

func (p *otherDriverProbe) Workshop(ctx context.Context, number string) (Document, error) {
	p.workshopCalls++
	return testDocuments{}.Workshop(ctx, number)
}

func (p *otherDriverProbe) SPP(ctx context.Context, number string) (Document, error) {
	p.sppCalls++
	return testDocuments{}.SPP(ctx, number)
}

func otherInput(direction Direction) Input {
	input := Input{NoLambung: "SAMT119", Direction: direction, DriverID: 42, DriverName: " Driver Name ", Note: " Note "}
	if direction == DirectionIn {
		input.InCategory = InWaitingForAssignment
	} else {
		input.OutDestination = OutOther
	}
	input.Normalize()
	return input
}

func TestOtherDriverValidationSwitchForBothDirections(t *testing.T) {
	for _, direction := range []Direction{DirectionIn, DirectionOut} {
		for _, test := range []struct {
			name    string
			options []ServiceOption
			check   bool
		}{
			{name: "default enabled", check: true},
			{name: "explicit enabled", options: []ServiceOption{WithOtherDriverValidation(true)}, check: true},
			{name: "testing bypass", options: []ServiceOption{WithOtherDriverValidation(false)}},
		} {
			t.Run(string(direction)+"/"+test.name, func(t *testing.T) {
				probe := &otherDriverProbe{}
				input := otherInput(direction)
				if err := input.Validate(); err != nil {
					t.Fatal(err)
				}
				document, err := NewService(nil, nil, probe, test.options...).validateDocument(context.Background(), &input)
				if test.check {
					if probe.driverCalls != 1 || !errors.Is(err, ErrExternalDocument) {
						t.Fatalf("enabled validation must preserve the upstream rejection: %v", err)
					}
				} else if err != nil || probe.driverCalls != 0 || document.DriverName != "Driver Name" {
					t.Fatalf("bypass must use the trimmed APP snapshot without a Driver call: %v", err)
				}
			})
		}
		input := otherInput(direction)
		if _, err := NewService(nil, nil, nil, WithOtherDriverValidation(false)).validateDocument(context.Background(), &input); err != nil {
			t.Fatalf("OTHER bypass must not depend on an integration client: %v", err)
		}
	}
}

func TestOtherDriverBypassDoesNotBypassDocumentValidation(t *testing.T) {
	probe := &otherDriverProbe{}
	service := NewService(nil, nil, probe, WithOtherDriverValidation(false))
	for _, input := range []Input{
		{NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: OutWorkshop, WorkOrderNumber: "WO-001"},
		{NoLambung: "SAMT119", Direction: DirectionOut, OutDestination: OutFillingShed, SPPNumber: "SPP-001"},
	} {
		if _, err := service.validateDocument(context.Background(), &input); err != nil {
			t.Fatal(err)
		}
		if _, err := NewService(nil, nil, nil, WithOtherDriverValidation(false)).validateDocument(context.Background(), &input); !errors.Is(err, ErrExternalDocument) {
			t.Fatal("missing WO/SPP integration must not be bypassed")
		}
	}
	if probe.driverCalls != 0 || probe.workshopCalls != 1 || probe.sppCalls != 1 {
		t.Fatal("the switch changed a non-OTHER integration path")
	}
}

func TestOtherDriverLocalRequirementsRemainEnforced(t *testing.T) {
	for _, direction := range []Direction{DirectionIn, DirectionOut} {
		for _, change := range []func(*Input){
			func(i *Input) { i.DriverID = 0 },
			func(i *Input) { i.DriverID = -42 },
			func(i *Input) { i.DriverName = "  " },
			func(i *Input) { i.Note = "  " },
		} {
			input := otherInput(direction)
			change(&input)
			input.Normalize()
			if !errors.Is(input.Validate(), ErrInvalidInput) {
				t.Fatal("OTHER must still require a positive driver ID, name, and note")
			}
		}
	}
}

func TestOtherDriverSwitchDoesNotInvalidateStoredReplays(t *testing.T) {
	for _, check := range []bool{true, false} {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		input := otherInput(DirectionOut)
		input.Coordinates = "0,0"
		hash, err := requestHash(input)
		if err != nil {
			t.Fatal(err)
		}
		mock.ExpectQuery("SELECT m.transaction_id").WithArgs("96942009-f8c4-4dbf-b7e0-6d571765166a").
			WillReturnRows(sqlmock.NewRows([]string{"transaction_id", "request_hash", "driver_id", "driver_name_snapshot", "coordinates"}).
				AddRow(20, hash, 42, "Driver Name", "0,0"))
		result, err := NewService(sqlx.NewDb(db, "sqlmock"), nil, nil, WithOtherDriverValidation(check)).Record(
			context.Background(), "96942009-f8c4-4dbf-b7e0-6d571765166a", input, "actor", "Satpam")
		if err != nil || result.ID != 20 || result.Coordinates == nil || *result.Coordinates != "0,0" {
			t.Fatalf("changing the server switch must not invalidate a committed replay: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}
