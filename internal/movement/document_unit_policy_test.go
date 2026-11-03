package movement

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestDocumentUnitPolicyPreservesWOAndUnitChecks(t *testing.T) {
	unit := snapshot{NoLambung: "SAMT119", NoPolisi: "DA 1234 AB"}
	for _, test := range []struct {
		name         string
		document     Document
		wantMismatch bool
	}{
		{"WO matching", Document{NoLambung: "samt119", NoPolisi: " da 1234 ab "}, false},
		{"WO different police", Document{NoLambung: "SAMT119", NoPolisi: "DA 9999 XX"}, true},
		{"WO missing police", Document{NoLambung: "SAMT119"}, true},
		{"WO wrong unit", Document{NoLambung: "SAMT999", NoPolisi: unit.NoPolisi}, true},
		{"SPP ignores missing police", Document{SPPNumber: "SPP-001", NoLambung: "SAMT119"}, false},
		{"SPP ignores arbitrary police", Document{SPPNumber: "SPP-001", NoLambung: "SAMT119", NoPolisi: "arbitrary text"}, false},
		{"SPP wrong unit still rejected", Document{SPPNumber: "SPP-001", NoLambung: "SAMT999", NoPolisi: unit.NoPolisi}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateDocumentUnit(test.document, unit)
			if test.wantMismatch {
				if !errors.Is(err, ErrExternalDocument) || !errors.Is(err, ErrDocumentUnitMismatch) {
					t.Fatalf("expected unit validation error: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

type policyDocuments struct {
	testDocuments
	document Document
}

func (d policyDocuments) Workshop(context.Context, string) (Document, error) { return d.document, nil }
func (d policyDocuments) SPP(context.Context, string) (Document, error)      { return d.document, nil }

func TestSPPPoliceRelaxationPreservesRequiredDocumentFields(t *testing.T) {
	for _, spp := range []bool{false, true} {
		for _, missing := range []string{"unit", "driver", "police"} {
			doc := Document{NoLambung: "SAMT119", NoPolisi: "DA 1234 AB", DriverName: "Driver"}
			if spp {
				doc.SPPNumber = "SPP-001"
			}
			switch missing {
			case "unit":
				doc.NoLambung = ""
			case "driver":
				doc.DriverName = ""
			case "police":
				doc.NoPolisi = ""
			}
			service := NewService(nil, nil, policyDocuments{document: doc})
			wantValid := spp && missing == "police"
			if err := service.prepareDocumentUnit(context.Background(), doc); (err == nil) != wantValid {
				t.Errorf("prepare SPP=%t missing %s: %v", spp, missing, err)
			}
			for _, direction := range []Direction{DirectionIn, DirectionOut} {
				input := Input{NoLambung: "SAMT119", Direction: direction, WorkOrderNumber: "WO-001"}
				if spp {
					input.WorkOrderNumber, input.SPPNumber = "", "123-example"
				}
				if _, err := service.validateDocument(context.Background(), &input); (err == nil) != wantValid {
					t.Errorf("submit SPP=%t %s missing %s: %v", spp, direction, missing, err)
				}
				input.NoLambung = "SAMT999"
				if _, err := service.validateDocument(context.Background(), &input); !errors.Is(err, ErrExternalDocument) {
					t.Errorf("wrong submitted unit must be rejected: %v", err)
				}
			}
		}
	}
}

func TestSPPUnknownOrInactiveUnitStillFailsBeforeWrites(t *testing.T) {
	for _, action := range []string{"lookup", "IN", "OUT"} {
		t.Run(action, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			service := NewService(sqlx.NewDb(db, "sqlmock"), nil, policyDocuments{document: Document{SPPNumber: "SPP-001", NoLambung: "SAMT119", DriverName: "Driver"}})
			if action != "lookup" {
				mock.ExpectQuery("SELECT m.transaction_id").WillReturnRows(sqlmock.NewRows([]string{"transaction_id"}))
				mock.ExpectBegin()
			}
			// Both lookup and lock must exclude inactive units, not just compare No Lambung.
			mock.ExpectQuery(`WHERE u.no_lambung = \$1 AND u.is_active = TRUE`).WillReturnRows(sqlmock.NewRows([]string{"lambung_unit_id"}))
			if action == "lookup" {
				_, err = service.LookupSPP(context.Background(), "123-example")
			} else {
				mock.ExpectRollback()
				input := Input{NoLambung: "SAMT119", Direction: Direction(action), SPPNumber: "123-example", OutDestination: OutFillingShed}
				if action == "IN" {
					input.InCategory, input.OutDestination = InWaitingForAssignment, ""
				}
				_, err = service.Record(context.Background(), "key", input, "actor", "Satpam")
			}
			if !errors.Is(err, ErrUnitNotFound) {
				t.Fatalf("missing active master must remain UNIT_NOT_FOUND: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
