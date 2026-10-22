package movement

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

type missingDocuments struct{ calls int }

func (d *missingDocuments) Workshop(context.Context, string) (Document, error) {
	d.calls++
	return Document{}, ErrWorkOrderNotFound
}
func (d *missingDocuments) SPP(context.Context, string) (Document, error) {
	d.calls++
	return Document{}, ErrSPPNotFound
}
func (d *missingDocuments) Driver(context.Context, int64, string) error {
	d.calls++
	return errors.New("unexpected Driver call")
}

func TestStoredDocumentReplayPrecedesNotFoundRevalidation(t *testing.T) {
	for _, kind := range []string{"WO", "SPP legacy", "SPP reference"} {
		for _, direction := range []Direction{DirectionIn, DirectionOut} {
			t.Run(kind+"/"+string(direction), func(t *testing.T) {
				db, mock, err := sqlmock.New()
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				input := Input{NoLambung: "SAMT072", Direction: direction, Coordinates: "0,0"}
				if kind == "WO" {
					input.WorkOrderNumber, input.OutDestination = "DOC/001", OutWorkshop
				} else {
					input.SPPNumber, input.OutDestination = "DOC/001", OutFillingShed
					if kind == "SPP reference" {
						input.SPPNumber = "123-example"
					}
				}
				if direction == DirectionIn {
					input.InCategory, input.OutDestination = InWaitingForAssignment, ""
				}
				hash, err := requestHash(input)
				if err != nil {
					t.Fatal(err)
				}
				documents := &missingDocuments{}
				service := NewService(sqlx.NewDb(db, "sqlmock"), nil, documents)
				const key = "96942009-f8c4-4dbf-b7e0-6d571765166a"
				for _, conflict := range []bool{false, true} {
					mock.ExpectQuery("SELECT m.transaction_id").WithArgs(key).
						WillReturnRows(sqlmock.NewRows([]string{"transaction_id", "request_hash", "coordinates", "work_order_number", "spp_number"}).
							AddRow(20, hash, "0,0", input.WorkOrderNumber, "DOC/001"))
					retry := input
					if conflict {
						retry.Coordinates = "1,1"
					}
					result, err := service.Record(context.Background(), key, retry, "actor", "Satpam")
					if conflict {
						if !errors.Is(err, ErrIdempotencyConflict) {
							t.Fatalf("changed payload must still conflict: %v", err)
						}
					} else if err != nil || result.ID != 20 || result.Coordinates == nil || *result.Coordinates != "0,0" {
						t.Fatalf("stored movement must replay even if document is now absent: %v", err)
					}
				}
				if documents.calls != 0 {
					t.Fatal("replay/conflict must be resolved before current upstream availability")
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
