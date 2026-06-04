package etl

import (
	"context"
	"database/sql"
	"fmt"
)

// RebuildAggregates recomputes all aggregate tables from fact_focus_cost_daily.
func (p *Processor) RebuildAggregates(ctx context.Context) error {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := p.rebuildAggregates(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// RebuildTagsAll rebuilds bridge_cost_tag for every PROCESSED batch using staging tag JSON.
func (p *Processor) RebuildTagsAll(ctx context.Context) error {
	batchIDs, err := p.processedBatchIDs(ctx)
	if err != nil {
		return err
	}
	if len(batchIDs) == 0 {
		return fmt.Errorf("no PROCESSED ingestion batches found")
	}

	// Read staging outside the transaction. With SQLite MaxOpenConns(1), any
	// p.DB query while a tx holds the sole connection deadlocks database/sql.
	type batchStaging struct {
		id   int64
		rows []normRow
	}
	staging := make([]batchStaging, 0, len(batchIDs))
	for _, batchID := range batchIDs {
		rows, err := p.loadNormalized(ctx, batchID)
		if err != nil {
			return fmt.Errorf("batch %d: %w", batchID, err)
		}
		if len(rows) == 0 {
			continue
		}
		staging = append(staging, batchStaging{id: batchID, rows: rows})
	}
	if len(staging) == 0 {
		return fmt.Errorf("no staging rows for PROCESSED batches")
	}

	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM bridge_cost_tag`); err != nil {
		return err
	}

	for _, batch := range staging {
		if err := p.buildTags(ctx, tx, batch.id, batch.rows); err != nil {
			return fmt.Errorf("batch %d tags: %w", batch.id, err)
		}
	}
	return tx.Commit()
}

// PurgeBatch removes facts, tag bridges, staging rows, and the batch record.
func (p *Processor) PurgeBatch(ctx context.Context, batchID int64) error {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := p.deleteBatchFacts(ctx, tx, batchID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, p.q(`DELETE FROM stg_focus_cost_line WHERE ingestion_batch_id = ?`), batchID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, p.q(`DELETE FROM dim_ingestion_batch WHERE ingestion_batch_id = ?`), batchID); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *Processor) processedBatchIDs(ctx context.Context) ([]int64, error) {
	q := `SELECT ingestion_batch_id FROM dim_ingestion_batch WHERE status = 'PROCESSED' ORDER BY ingestion_batch_id`
	if p.Dialect == "sqlserver" {
		q = `SELECT ingestion_batch_id FROM dim_ingestion_batch WHERE status = 'PROCESSED' ORDER BY ingestion_batch_id`
	}
	rows, err := p.DB.QueryContext(ctx, q)
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

var _ = sql.ErrNoRows
