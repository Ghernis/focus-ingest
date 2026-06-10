package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ghernis/focus_dt/internal/etl"
)

// FindCompletedImport returns the latest PROCESSED batch for a source file and FOCUS version.
func FindCompletedImport(ctx context.Context, db *sql.DB, dialect, sourceFile, focusVersion string) (int64, bool, error) {
	var id int64
	var err error
	if dialect == "sqlserver" {
		err = db.QueryRowContext(ctx, `
			SELECT TOP 1 ingestion_batch_id FROM dim_ingestion_batch
			WHERE source_file = @p1 AND focus_version = @p2 AND status = 'PROCESSED'
			ORDER BY ingestion_batch_id DESC`, sourceFile, focusVersion).Scan(&id)
	} else {
		err = db.QueryRowContext(ctx, `
			SELECT ingestion_batch_id FROM dim_ingestion_batch
			WHERE source_file = ? AND focus_version = ? AND status = 'PROCESSED'
			ORDER BY ingestion_batch_id DESC LIMIT 1`, sourceFile, focusVersion).Scan(&id)
	}
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func findCompletedImport(ctx context.Context, db *sql.DB, dialect, sourceFile, focusVersion string) (int64, bool, error) {
	return FindCompletedImport(ctx, db, dialect, sourceFile, focusVersion)
}

func purgeBatch(ctx context.Context, db *sql.DB, dialect string, batchID int64) error {
	p := &etl.Processor{DB: db, Dialect: dialect}
	if err := p.PurgeBatch(ctx, batchID); err != nil {
		return fmt.Errorf("purge batch %d: %w", batchID, err)
	}
	return nil
}

// PurgeLoadingBatchesForFile removes incomplete LOADING batches (and their staging) for a source file.
func PurgeLoadingBatchesForFile(ctx context.Context, db *sql.DB, dialect, sourceFile, focusVersion string) (int, error) {
	ids, err := loadingBatchIDs(ctx, db, dialect, sourceFile, focusVersion)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := purgeBatch(ctx, db, dialect, id); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

func loadingBatchIDs(ctx context.Context, db *sql.DB, dialect, sourceFile, focusVersion string) ([]int64, error) {
	var q string
	if dialect == "sqlserver" {
		q = `SELECT ingestion_batch_id FROM dim_ingestion_batch
			WHERE source_file = @p1 AND focus_version = @p2 AND status = 'LOADING'
			ORDER BY ingestion_batch_id`
	} else {
		q = `SELECT ingestion_batch_id FROM dim_ingestion_batch
			WHERE source_file = ? AND focus_version = ? AND status = 'LOADING'
			ORDER BY ingestion_batch_id`
	}
	rows, err := db.QueryContext(ctx, q, sourceFile, focusVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func MarkBatchFailed(ctx context.Context, db *sql.DB, dialect string, batchID int64) error {
	var q string
	if dialect == "sqlserver" {
		q = `UPDATE dim_ingestion_batch SET status = 'FAILED' WHERE ingestion_batch_id = @p1 AND status = 'LOADING'`
	} else {
		q = `UPDATE dim_ingestion_batch SET status = 'FAILED' WHERE ingestion_batch_id = ? AND status = 'LOADING'`
	}
	_, err := db.ExecContext(ctx, q, batchID)
	return err
}
