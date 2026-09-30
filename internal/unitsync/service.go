package unitsync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"truckin-be/internal/integration"
	"truckin-be/internal/movement"
)

type LambungClient interface {
	Units(context.Context, string, int) (integration.UnitPage, error)
}

type Service struct {
	db       *sqlx.DB
	client   LambungClient
	pageSize int
}

const maxLambungPageSize = 200

func NewService(db *sqlx.DB, client LambungClient, pageSize int) *Service {
	if pageSize < 1 {
		pageSize = 100
	}
	if pageSize > maxLambungPageSize {
		pageSize = maxLambungPageSize
	}
	return &Service{db: db, client: client, pageSize: pageSize}
}

func (s *Service) EnsureKnown(ctx context.Context, noLambung string) error {
	var found bool
	if err := s.db.GetContext(ctx, &found, `SELECT EXISTS(SELECT 1 FROM unit_snapshot WHERE no_lambung = $1 AND is_active = TRUE)`, noLambung); err != nil {
		return err
	}
	if found {
		return nil
	}
	cursor := ""
	seenCursors := make(map[string]struct{})
	for {
		page, err := s.client.Units(ctx, cursor, s.pageSize)
		if err != nil {
			return err
		}
		nextCursor, hasMore, err := nextCursor(page, seenCursors)
		if err != nil {
			return err
		}
		for _, unit := range page.Items {
			normalizeUnit(&unit)
			if err := validateUnit(unit); err != nil {
				return err
			}
			if unit.NoLambung != noLambung {
				continue
			}
			return s.upsertOne(ctx, unit)
		}
		if !hasMore {
			break
		}
		cursor = nextCursor
	}
	return movement.ErrUnitNotFound
}

func (s *Service) SyncAll(ctx context.Context) error {
	var runID int64
	if err := s.db.GetContext(ctx, &runID, `INSERT INTO unit_sync_runs (started_at, status) VALUES (NOW(), 'RUNNING') RETURNING sync_run_id`); err != nil {
		return err
	}
	units, err := s.fetchAll(ctx)
	if err != nil {
		s.finishRun(ctx, runID, "FAILED", 0)
		return err
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		s.finishRun(ctx, runID, "FAILED", 0)
		return err
	}
	defer tx.Rollback()
	ids := make([]int64, 0, len(units))
	for _, unit := range units {
		if _, err := tx.ExecContext(ctx, upsertSQL, unit.ID, unit.NoLambung, unit.NoPolisi); err != nil {
			s.finishRun(ctx, runID, "FAILED", 0)
			return err
		}
		ids = append(ids, unit.ID)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE unit_snapshot SET is_active = FALSE WHERE NOT (lambung_unit_id = ANY($1))`, pq.Array(ids)); err != nil {
		s.finishRun(ctx, runID, "FAILED", 0)
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE unit_sync_runs SET finished_at = NOW(), status = 'SUCCEEDED', units_read = $1, error_message = NULL WHERE sync_run_id = $2`, len(units), runID); err != nil {
		s.finishRun(ctx, runID, "FAILED", 0)
		return err
	}
	if err := tx.Commit(); err != nil {
		s.finishRun(ctx, runID, "FAILED", 0)
		return err
	}
	return nil
}

func (s *Service) fetchAll(ctx context.Context) ([]integration.Unit, error) {
	var all []integration.Unit
	seen := make(map[int64]struct{})
	cursor := ""
	seenCursors := make(map[string]struct{})
	for {
		page, err := s.client.Units(ctx, cursor, s.pageSize)
		if err != nil {
			return nil, err
		}
		for _, unit := range page.Items {
			normalizeUnit(&unit)
			if err := validateUnit(unit); err != nil {
				return nil, err
			}
			if _, duplicate := seen[unit.ID]; duplicate {
				return nil, fmt.Errorf("duplicate unit %d from Unit Lambung", unit.ID)
			}
			seen[unit.ID] = struct{}{}
			all = append(all, unit)
		}
		nextCursor, hasMore, err := nextCursor(page, seenCursors)
		if err != nil {
			return nil, err
		}
		if !hasMore {
			return all, nil
		}
		cursor = nextCursor
	}
}

func validateUnit(unit integration.Unit) error {
	if unit.ID <= 0 || unit.NoLambung == "" || unit.NoPolisi == "" {
		return errors.New("invalid unit from Unit Lambung")
	}
	return nil
}

func nextCursor(page integration.UnitPage, seen map[string]struct{}) (string, bool, error) {
	if !page.HasMore {
		return "", false, nil
	}
	if page.NextCursor == nil {
		return "", false, errors.New("Unit Lambung page has no next cursor")
	}
	cursor := strings.TrimSpace(*page.NextCursor)
	if cursor == "" {
		return "", false, errors.New("Unit Lambung page has an empty next cursor")
	}
	if _, duplicate := seen[cursor]; duplicate {
		return "", false, errors.New("Unit Lambung page repeated a cursor")
	}
	seen[cursor] = struct{}{}
	return cursor, true, nil
}

func (s *Service) upsertOne(ctx context.Context, unit integration.Unit) error {
	_, err := s.db.ExecContext(ctx, upsertSQL, unit.ID, unit.NoLambung, unit.NoPolisi)
	return err
}

func (s *Service) finishRun(ctx context.Context, runID int64, status string, unitsRead int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE unit_sync_runs SET finished_at = NOW(), status = $1, units_read = $2, error_message = CASE WHEN $1 = 'FAILED' THEN 'sync failed' ELSE NULL END WHERE sync_run_id = $3`, status, unitsRead, runID)
	return err
}

func normalizeUnit(unit *integration.Unit) {
	unit.NoLambung = strings.ToUpper(strings.TrimSpace(unit.NoLambung))
	unit.NoPolisi = strings.TrimSpace(unit.NoPolisi)
}

const upsertSQL = `
INSERT INTO unit_snapshot (lambung_unit_id, no_lambung, no_polisi, is_active, synced_at)
VALUES ($1, $2, $3, TRUE, NOW())
ON CONFLICT (lambung_unit_id) DO UPDATE SET
    no_lambung = EXCLUDED.no_lambung,
    no_polisi = EXCLUDED.no_polisi,
    is_active = TRUE,
    synced_at = NOW()`

func RunPeriodically(ctx context.Context, service *Service, interval time.Duration, report func(error)) {
	if err := service.SyncAll(ctx); err != nil {
		report(err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := service.SyncAll(ctx); err != nil {
				report(err)
			}
		}
	}
}
