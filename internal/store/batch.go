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
