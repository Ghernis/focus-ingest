package etl

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed process_batch_sqlserver_core.sql
var processBatchSQLServerCore string

//go:embed process_batch_sqlserver_tags.sql
var processBatchSQLServerTags string

const sqlBatchHeader = `
SET NOCOUNT ON;
DECLARE @IngestionBatchId BIGINT = @p2;
DECLARE @FocusVersion VARCHAR(16) = @p1;
`

func (p *Processor) processBatchSQLServer(ctx context.Context, batchID int64, focusVersion string) error {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, sqlBatchHeader+processBatchSQLServerCore, focusVersion, batchID); err != nil {
		return fmt.Errorf("sql etl core: %w", err)
	}
	if !p.SkipTags {
		if _, err := tx.ExecContext(ctx, sqlBatchHeader+processBatchSQLServerTags, focusVersion, batchID); err != nil {
			return fmt.Errorf("sql etl tags: %w", err)
		}
	}
	if !p.SkipAggregates {
		if err := p.rebuildAggregates(ctx, tx); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE dbo.dim_ingestion_batch
		SET row_count = (
		  SELECT COUNT(*) FROM dbo.stg_focus_cost_line WHERE ingestion_batch_id = @p2
		),
		status = 'PROCESSED'
		WHERE ingestion_batch_id = @p2`, focusVersion, batchID); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *Processor) processBatchGo(ctx context.Context, batchID int64, focusVersion string) error {
	rows, err := p.loadNormalized(ctx, batchID)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("batch %d: no valid staging rows", batchID)
	}

	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := p.upsertDimensions(ctx, tx, rows); err != nil {
		return err
	}
	if err := p.rollupDaily(ctx, tx, batchID, focusVersion, rows); err != nil {
		return err
	}
	if !p.SkipTags {
		if err := p.buildTags(ctx, tx, batchID, rows); err != nil {
			return err
		}
	}
	if !p.SkipAggregates {
		if err := p.rebuildAggregates(ctx, tx); err != nil {
			return err
		}
	}
	if err := p.updateBatchStatus(ctx, tx, batchID, int64(len(rows))); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *Processor) ProcessBatch(ctx context.Context, batchID int64, focusVersion string) error {
	if p.Dialect == "sqlserver" && !p.UseGoETL {
		return p.processBatchSQLServer(ctx, batchID, focusVersion)
	}
	return p.processBatchGo(ctx, batchID, focusVersion)
}

// execSQLServerScript runs a DDL/DML script split on GO boundaries (for ad-hoc use).
func execSQLServerScript(ctx context.Context, db *sql.DB, script string, args ...interface{}) error {
	for _, batch := range splitOnGO(script) {
		batch = strings.TrimSpace(batch)
		if batch == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, batch, args...); err != nil {
			return err
		}
	}
	return nil
}

func splitOnGO(script string) []string {
	var batches []string
	var b strings.Builder
	for _, line := range strings.Split(script, "\n") {
		if strings.TrimSpace(strings.ToUpper(line)) == "GO" {
			batches = append(batches, strings.TrimSpace(b.String()))
			b.Reset()
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if tail := strings.TrimSpace(b.String()); tail != "" {
		batches = append(batches, tail)
	}
	return batches
}
