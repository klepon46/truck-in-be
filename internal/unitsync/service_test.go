package unitsync

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"

	"truckin-be/internal/integration"
)

type fakeLambungClient struct {
	pages map[string]integration.UnitPage
	calls []string
}

func (c *fakeLambungClient) Units(_ context.Context, cursor string, _ int) (integration.UnitPage, error) {
	c.calls = append(c.calls, cursor)
	page, found := c.pages[cursor]
	if !found {
		return integration.UnitPage{}, fmt.Errorf("unexpected cursor %q", cursor)
	}
	return page, nil
}

func TestFetchAllTraversesCursorPages(t *testing.T) {
	nextCursor := "cursor-42"
	client := &fakeLambungClient{pages: map[string]integration.UnitPage{
		"": {
			Items:      []integration.Unit{{ID: 41, NoLambung: "samt001", NoPolisi: "DA 8346 CF"}},
			NextCursor: &nextCursor,
			HasMore:    true,
		},
		"cursor-42": {
			Items:   []integration.Unit{{ID: 42, NoLambung: "SAMT007", NoPolisi: "DA 8515 CX"}},
			HasMore: false,
		},
	}}
	service := NewService(nil, client, 100)

	units, err := service.fetchAll(context.Background())
	if err != nil {
		t.Fatalf("fetchAll() error = %v", err)
	}
	if !reflect.DeepEqual(client.calls, []string{"", "cursor-42"}) {
		t.Fatalf("cursors = %#v", client.calls)
	}
	if len(units) != 2 || units[0].NoLambung != "SAMT001" || units[1].NoLambung != "SAMT007" {
		t.Fatalf("units = %#v", units)
	}
}

func TestFetchAllRejectsRepeatedCursor(t *testing.T) {
	nextCursor := "cursor-42"
	client := &fakeLambungClient{pages: map[string]integration.UnitPage{
		"": {
			Items:      []integration.Unit{{ID: 41, NoLambung: "SAMT001", NoPolisi: "DA 8346 CF"}},
			NextCursor: &nextCursor,
			HasMore:    true,
		},
		"cursor-42": {
			Items:      []integration.Unit{{ID: 42, NoLambung: "SAMT007", NoPolisi: "DA 8515 CX"}},
			NextCursor: &nextCursor,
			HasMore:    true,
		},
	}}
	service := NewService(nil, client, 100)

	if _, err := service.fetchAll(context.Background()); err == nil {
		t.Fatal("fetchAll() accepted a repeated cursor")
	}
}

func TestNewServiceCapsLambungPageSize(t *testing.T) {
	service := NewService(nil, nil, 201)
	if service.pageSize != 200 {
		t.Fatalf("page size = %d", service.pageSize)
	}
}

func TestSyncAllInactivatesUnitsMissingFromCompletedPages(t *testing.T) {
	nextCursor := "cursor-42"
	client := &fakeLambungClient{pages: map[string]integration.UnitPage{
		"": {
			Items:      []integration.Unit{{ID: 41, NoLambung: "SAMT001", NoPolisi: "DA 8346 CF"}},
			NextCursor: &nextCursor,
			HasMore:    true,
		},
		"cursor-42": {
			Items:   []integration.Unit{{ID: 42, NoLambung: "SAMT007", NoPolisi: "DA 8515 CX"}},
			HasMore: false,
		},
	}}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO unit_sync_runs (started_at, status) VALUES (NOW(), 'RUNNING') RETURNING sync_run_id`)).
		WillReturnRows(sqlmock.NewRows([]string{"sync_run_id"}).AddRow(7))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(upsertSQL)).WithArgs(int64(41), "SAMT001", "DA 8346 CF").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(upsertSQL)).WithArgs(int64(42), "SAMT007", "DA 8515 CX").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE unit_snapshot SET is_active = FALSE WHERE NOT (lambung_unit_id = ANY($1))`)).WithArgs(sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE unit_sync_runs SET finished_at = NOW(), status = 'SUCCEEDED', units_read = $1, error_message = NULL WHERE sync_run_id = $2`)).WithArgs(2, int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	service := NewService(sqlx.NewDb(db, "sqlmock"), client, 100)
	if err := service.SyncAll(context.Background()); err != nil {
		t.Fatalf("SyncAll() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSyncAllDoesNotChangeSnapshotsAfterMalformedCursor(t *testing.T) {
	nextCursor := "cursor-42"
	client := &fakeLambungClient{pages: map[string]integration.UnitPage{
		"": {
			Items:      []integration.Unit{{ID: 41, NoLambung: "SAMT001", NoPolisi: "DA 8346 CF"}},
			NextCursor: &nextCursor,
			HasMore:    true,
		},
		"cursor-42": {
			Items:      []integration.Unit{{ID: 42, NoLambung: "SAMT007", NoPolisi: "DA 8515 CX"}},
			NextCursor: &nextCursor,
			HasMore:    true,
		},
	}}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO unit_sync_runs (started_at, status) VALUES (NOW(), 'RUNNING') RETURNING sync_run_id`)).
		WillReturnRows(sqlmock.NewRows([]string{"sync_run_id"}).AddRow(7))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE unit_sync_runs SET finished_at = NOW(), status = $1, units_read = $2, error_message = CASE WHEN $1 = 'FAILED' THEN 'sync failed' ELSE NULL END WHERE sync_run_id = $3`)).WithArgs("FAILED", 0, int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))

	service := NewService(sqlx.NewDb(db, "sqlmock"), client, 100)
	if err := service.SyncAll(context.Background()); err == nil {
		t.Fatal("SyncAll() accepted a repeated cursor")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureKnownRejectsInvalidUnitBeforeUpsert(t *testing.T) {
	client := &fakeLambungClient{pages: map[string]integration.UnitPage{
		"": {
			Items:   []integration.Unit{{ID: 0, NoLambung: "SAMT001", NoPolisi: ""}},
			HasMore: false,
		},
	}}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT EXISTS(SELECT 1 FROM unit_snapshot WHERE no_lambung = $1 AND is_active = TRUE)`)).
		WithArgs("SAMT001").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	service := NewService(sqlx.NewDb(db, "sqlmock"), client, 100)
	err = service.EnsureKnown(context.Background(), "SAMT001")
	if err == nil || err.Error() != "invalid unit from Unit Lambung" {
		t.Fatalf("EnsureKnown() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
