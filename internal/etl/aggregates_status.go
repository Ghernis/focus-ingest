package etl

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ghernis/focus_dt/internal/focus"
)

const (
	AggregatesStatusPending  = "PENDING"
	AggregatesStatusComplete = "COMPLETE"
)

// RebuildAggregatesIncremental refreshes aggregate tables only for billing months
// tied to batches that are not yet marked aggregates-complete.
func (p *Processor) RebuildAggregatesIncremental(ctx context.Context) (int, error) {
	months, batchIDs, err := p.monthsPendingAggregates(ctx)
	if err != nil {
		return 0, err
	}
	if len(months) == 0 {
		return 0, nil
	}
	if err := p.rebuildAggregatesForMonths(ctx, months); err != nil {
		return 0, err
	}
	return len(months), p.markBatchesAggregatesComplete(ctx, batchIDs)
}

// RebuildAggregatesFull truncates and rebuilds all aggregate tables from facts.
func (p *Processor) RebuildAggregatesFull(ctx context.Context) error {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := p.rebuildAggregates(ctx, tx, nil); err != nil {
		return err
	}
	if err := p.markAllProcessedBatchesAggregatesComplete(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *Processor) monthsPendingAggregates(ctx context.Context) ([]string, []int64, error) {
	batchIDs, err := p.pendingAggregateBatchIDs(ctx)
	if err != nil {
		return nil, nil, err
	}
	if len(batchIDs) == 0 {
		return nil, nil, nil
	}
	months, err := p.billingMonthsForBatches(ctx, batchIDs)
	if err != nil {
		return nil, nil, err
	}
	return months, batchIDs, nil
}

func (p *Processor) pendingAggregateBatchIDs(ctx context.Context) ([]int64, error) {
	q := `SELECT ingestion_batch_id FROM dim_ingestion_batch
		WHERE status = 'PROCESSED'
		  AND (aggregates_status IS NULL OR aggregates_status <> ?)
		ORDER BY ingestion_batch_id`
	if p.Dialect == "sqlserver" {
		q = `SELECT ingestion_batch_id FROM dim_ingestion_batch
			WHERE status = 'PROCESSED'
			  AND (aggregates_status IS NULL OR aggregates_status <> @p1)
			ORDER BY ingestion_batch_id`
	}
	rows, err := p.DB.QueryContext(ctx, q, AggregatesStatusComplete)
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

func (p *Processor) billingMonthsForBatches(ctx context.Context, batchIDs []int64) ([]string, error) {
	if len(batchIDs) == 0 {
		return nil, nil
	}
	ph, args := inClausePlaceholders(p.Dialect, batchIDs, 1)
	q := fmt.Sprintf(`SELECT DISTINCT %s
		FROM fact_focus_cost_daily
		WHERE ingestion_batch_id IN (%s)
		  AND billing_period_start IS NOT NULL
		ORDER BY 1`, p.dateOnlySelectExpr("billing_period_start"), ph)
	rows, err := p.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var months []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		m = focus.DateOnly(strings.TrimSpace(m))
		if m != "" {
			months = append(months, m)
		}
	}
	return months, rows.Err()
}

func (p *Processor) markBatchesAggregatesComplete(ctx context.Context, batchIDs []int64) error {
	if len(batchIDs) == 0 {
		return nil
	}
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := p.markBatchesAggregatesCompleteTx(ctx, tx, batchIDs); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *Processor) markBatchesAggregatesCompleteTx(ctx context.Context, tx *sql.Tx, batchIDs []int64) error {
	ph, batchArgs := inClausePlaceholders(p.Dialect, batchIDs, 2)
	args := append([]interface{}{AggregatesStatusComplete}, batchArgs...)
	q := fmt.Sprintf(`UPDATE dim_ingestion_batch SET aggregates_status = ? WHERE ingestion_batch_id IN (%s)`, ph)
	if p.Dialect == "sqlserver" {
		q = fmt.Sprintf(`UPDATE dim_ingestion_batch SET aggregates_status = @p1 WHERE ingestion_batch_id IN (%s)`, ph)
	}
	_, err := tx.ExecContext(ctx, p.q(q), args...)
	return err
}

func (p *Processor) markAllProcessedBatchesAggregatesComplete(ctx context.Context, tx *sql.Tx) error {
	q := `UPDATE dim_ingestion_batch SET aggregates_status = ? WHERE status = 'PROCESSED'`
	if p.Dialect == "sqlserver" {
		q = `UPDATE dim_ingestion_batch SET aggregates_status = @p1 WHERE status = 'PROCESSED'`
	}
	_, err := tx.ExecContext(ctx, q, AggregatesStatusComplete)
	return err
}

func (p *Processor) setBatchAggregatesStatus(ctx context.Context, tx *sql.Tx, batchID int64, status string) error {
	q := `UPDATE dim_ingestion_batch SET aggregates_status = ? WHERE ingestion_batch_id = ?`
	if p.Dialect == "sqlserver" {
		q = `UPDATE dim_ingestion_batch SET aggregates_status = @p1 WHERE ingestion_batch_id = @p2`
	}
	_, err := tx.ExecContext(ctx, q, status, batchID)
	return err
}

func inClausePlaceholders(dialect string, ids []int64, startAt int) (string, []interface{}) {
	ph := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
		if dialect == "sqlserver" {
			ph[i] = fmt.Sprintf("@p%d", startAt+i)
		} else {
			ph[i] = "?"
		}
	}
	return strings.Join(ph, ","), args
}

func (p *Processor) monthEq(column, month string) string {
	month = focus.DateOnly(strings.TrimSpace(month))
	if month == "" {
		return "1=0"
	}
	escaped := strings.ReplaceAll(month, "'", "''")
	if p.Dialect == "sqlserver" {
		return fmt.Sprintf("CAST(%s AS DATE) = '%s'", column, escaped)
	}
	return fmt.Sprintf("substr(%s, 1, 10) = '%s'", column, escaped)
}

// dateOnlySelectExpr returns a SQL expression that yields yyyy-mm-dd text for a date column.
// On SQL Server, plain CAST(... AS VARCHAR) is locale-dependent and breaks Go string keys.
func (p *Processor) dateOnlySelectExpr(column string) string {
	if p.Dialect == "sqlserver" {
		return fmt.Sprintf("CONVERT(VARCHAR(10), %s, 23)", column)
	}
	return fmt.Sprintf("substr(%s, 1, 10)", column)
}
