package publish

import (
	"context"
	"database/sql"
	"fmt"
)

// realignLocalSKs updates local SQLite FK columns when server assigned different surrogate keys.
func realignLocalSKs(ctx context.Context, local *sql.DB, maps map[string]map[int64]int64) error {
	if len(maps) == 0 {
		return nil
	}
	tx, err := local.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	type colMap struct {
		table string
		col   string
		dim   string
	}
	updates := []colMap{
		{"fact_focus_cost_daily", "billing_account_sk", "dim_account"},
		{"fact_focus_cost_daily", "sub_account_sk", "dim_sub_account"},
		{"fact_focus_cost_daily", "resource_sk", "dim_resource"},
		{"fact_focus_cost_daily", "service_sk", "dim_service"},
		{"fact_focus_cost_daily", "sku_sk", "dim_sku"},
		{"fact_focus_cost_daily", "region_sk", "dim_region"},
		{"fact_focus_cost_daily", "commitment_sk", "dim_commitment_discount"},
		{"fact_focus_cost_daily", "capacity_reservation_sk", "dim_capacity_reservation"},
		{"bridge_cost_tag", "tag_sk", "dim_tag"},
		{"agg_cost_daily", "sub_account_sk", "dim_sub_account"},
		{"agg_cost_daily", "service_sk", "dim_service"},
		{"agg_cost_daily", "region_sk", "dim_region"},
		{"agg_cost_monthly", "sub_account_sk", "dim_sub_account"},
		{"agg_commitment_utilization", "commitment_sk", "dim_commitment_discount"},
		{"agg_commitment_utilization_daily", "commitment_sk", "dim_commitment_discount"},
		{"agg_savings_summary", "service_sk", "dim_service"},
		{"agg_app_monthly", "application_sk", "dim_application"},
		{"agg_app_service_monthly", "application_sk", "dim_application"},
		{"agg_app_service_monthly", "service_sk", "dim_service"},
		{"agg_app_service_resource_monthly", "application_sk", "dim_application"},
		{"agg_app_service_resource_monthly", "service_sk", "dim_service"},
		{"agg_app_service_resource_monthly", "resource_sk", "dim_resource"},
		{"agg_cost_anomaly_monthly", "application_sk", "dim_application"},
		{"agg_cost_anomaly_monthly", "service_sk", "dim_service"},
	}

	for _, u := range updates {
		m := maps[u.dim]
		if len(m) == 0 {
			continue
		}
		for oldSK, newSK := range m {
			q := fmt.Sprintf(`UPDATE %s SET %s = ? WHERE %s = ?`, u.table, u.col, u.col)
			if _, err := tx.ExecContext(ctx, q, newSK, oldSK); err != nil {
				return fmt.Errorf("realign %s.%s %d->%d: %w", u.table, u.col, oldSK, newSK, err)
			}
		}
	}

	// Update local dim table PKs for pending rows that were realigned.
	for dim, m := range maps {
		pk := dimPK(dim)
		if pk == "" {
			continue
		}
		for oldSK, newSK := range m {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET %s = ? WHERE %s = ?`, dim, pk, pk), newSK, oldSK); err != nil {
				return fmt.Errorf("realign %s pk: %w", dim, err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE dim_sync_pending SET local_sk = ? WHERE dim_table = ? AND local_sk = ?`, newSK, dim, oldSK); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func dimPK(table string) string {
	for _, t := range sequenceTables {
		if t.table == table {
			return t.pk
		}
	}
	return ""
}
