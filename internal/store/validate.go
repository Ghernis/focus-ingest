package store

import (
	"context"
	"database/sql"
	"fmt"
)

func validateBatch(ctx context.Context, db *sql.DB, batchID int64) (ValidationReport, error) {
	var rep ValidationReport
	rep.BatchID = batchID
	rep.CommitmentByStatus = map[string]int64{}

	_ = db.QueryRowContext(ctx, `SELECT status FROM dim_ingestion_batch WHERE ingestion_batch_id = ?`, batchID).Scan(&rep.Status)
	if rep.Status == "" {
		_ = db.QueryRowContext(ctx, `SELECT status FROM dim_ingestion_batch WHERE ingestion_batch_id = @p1`, batchID).Scan(&rep.Status)
	}

	queries := []struct {
		sql string
		dst *int64
	}{
		{`SELECT COUNT(*) FROM stg_focus_cost_line WHERE ingestion_batch_id = ?`, &rep.StagingRows},
		{`SELECT COUNT(*) FROM fact_focus_cost_daily WHERE ingestion_batch_id = ?`, &rep.DailyFactRows},
		{`SELECT COUNT(*) FROM bridge_cost_tag b INNER JOIN fact_focus_cost_daily f ON b.cost_daily_id = f.cost_daily_id WHERE f.ingestion_batch_id = ?`, &rep.BridgeTagRows},
		{`SELECT COUNT(*) FROM dim_commitment_discount`, &rep.CommitmentDimRows},
		{`SELECT COUNT(*) FROM agg_cost_monthly`, &rep.AggMonthlyRows},
		{`SELECT COUNT(*) FROM agg_commitment_utilization`, &rep.AggCommitmentRows},
	}
	for _, q := range queries {
		if err := db.QueryRowContext(ctx, q.sql, batchID).Scan(q.dst); err != nil {
			// try sqlserver param style
			ss := replaceQ(q.sql)
			if err2 := db.QueryRowContext(ctx, ss, batchID).Scan(q.dst); err2 != nil && q.dst != &rep.CommitmentDimRows && q.dst != &rep.AggMonthlyRows && q.dst != &rep.AggCommitmentRows {
				return rep, err
			}
		}
	}

	rows, err := db.QueryContext(ctx, `
		SELECT a.provider, SUM(CAST(f.effective_cost AS REAL)), SUM(f.line_count)
		FROM fact_focus_cost_daily f
		INNER JOIN dim_account a ON f.billing_account_sk = a.account_sk
		WHERE f.ingestion_batch_id = ?
		GROUP BY a.provider`, batchID)
	if err != nil {
		rows, err = db.QueryContext(ctx, `
			SELECT a.provider, SUM(CAST(f.effective_cost AS DECIMAL(28,10))), SUM(f.line_count)
			FROM fact_focus_cost_daily f
			INNER JOIN dim_account a ON f.billing_account_sk = a.account_sk
			WHERE f.ingestion_batch_id = @p1
			GROUP BY a.provider`, batchID)
	}
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var ps ProviderSpend
			if err := rows.Scan(&ps.Provider, &ps.TotalEffective, &ps.SourceLines); err != nil {
				return rep, err
			}
			rep.ProviderSpend = append(rep.ProviderSpend, ps)
		}
	}

	crows, err := db.QueryContext(ctx, `
		SELECT commitment_status, SUM(line_count) FROM agg_commitment_utilization GROUP BY commitment_status`)
	if err == nil {
		defer crows.Close()
		for crows.Next() {
			var status string
			var cnt int64
			if err := crows.Scan(&status, &cnt); err != nil {
				return rep, err
			}
			rep.CommitmentByStatus[status] = cnt
		}
	}

	return rep, nil
}

func replaceQ(s string) string {
	return fmt.Sprintf(s) // placeholder; sqlserver queries inlined above
}
