package etl

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ghernis/focus_dt/internal/focus"
)

func (p *Processor) rebuildTierForMonth(ctx context.Context, tx *sql.Tx, month string) error {
	month = focus.DateOnly(strings.TrimSpace(month))
	if month == "" {
		return nil
	}
	if err := p.deleteTierForMonth(ctx, tx, month); err != nil {
		return err
	}
	daily, err := p.buildFactResourceTierDaily(ctx, tx, month)
	if err != nil {
		return err
	}
	rollups := map[string]*serviceTierRollup{}
	refreshed := p.refreshedUTCParam()
	if err := p.buildFactResourceTierChanges(ctx, tx, month, daily, rollups, refreshed); err != nil {
		return err
	}
	if err := p.buildFactResourceTierCarryforward(ctx, tx, month, daily, refreshed); err != nil {
		return err
	}
	if err := p.buildTierAggregates(ctx, tx, month, rollups, refreshed); err != nil {
		return err
	}
	return p.updateSavingsSummaryFromTierRollups(ctx, tx, month, rollups)
}

func (p *Processor) rebuildTierAllMonths(ctx context.Context, tx *sql.Tx) error {
	if err := p.enrichAllSkuTiers(ctx, tx); err != nil {
		return fmt.Errorf("enrich sku tiers: %w", err)
	}
	months, err := p.distinctFactBillingMonths(ctx, tx)
	if err != nil {
		return err
	}
	for _, m := range months {
		if err := p.rebuildTierForMonth(ctx, tx, m); err != nil {
			return fmt.Errorf("tier %s: %w", m, err)
		}
	}
	return nil
}

func (p *Processor) deleteTierForMonth(ctx context.Context, tx *sql.Tx, month string) error {
	m := p.monthEq("month_start", month)
	bm := p.monthEq("billing_period_start", month)
	for _, t := range []struct{ table, where string }{
		{"fact_resource_tier_daily", bm},
		{"fact_resource_tier_change", m},
		{"fact_resource_tier_carryforward", m},
		{"agg_resource_tier_change_monthly", m},
		{"agg_resource_tier_change_intramonth", m},
		{"agg_tier_change_summary_monthly", m},
	} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t.table+" WHERE "+t.where); err != nil {
			return fmt.Errorf("delete %s: %w", t.table, err)
		}
	}
	return nil
}

func (p *Processor) distinctFactBillingMonths(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT `+p.dateOnlySelectExpr("billing_period_start")+` FROM fact_focus_cost_daily ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		m = focus.DateOnly(strings.TrimSpace(m))
		if m != "" {
			out = append(out, m)
		}
	}
	return out, rows.Err()
}
