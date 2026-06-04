package etl

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
)

type distKey struct {
	month, provider, level, parent string
}

func (p *Processor) monthStartExpr(dateCol string) string {
	if p.Dialect == "sqlserver" {
		return fmt.Sprintf("DATEFROMPARTS(YEAR(%s), MONTH(%s), 1)", dateCol, dateCol)
	}
	return fmt.Sprintf("substr(%s,1,7)||'-01'", dateCol)
}

func (p *Processor) castCost(col string) string {
	if p.Dialect == "sqlserver" {
		return fmt.Sprintf("CAST(%s AS DECIMAL(28,10))", col)
	}
	return fmt.Sprintf("CAST(%s AS REAL)", col)
}

func (p *Processor) nowUTC() string {
	if p.Dialect == "sqlserver" {
		return "SYSUTCDATETIME()"
	}
	return "datetime('now')"
}

func (p *Processor) appContextJoins() string {
	return `
LEFT JOIN dim_resource res ON f.resource_sk = res.resource_sk
LEFT JOIN (
  SELECT b.cost_daily_id, MAX(t.tag_value) AS tag_value
  FROM bridge_cost_tag b
  INNER JOIN dim_tag t ON b.tag_sk = t.tag_sk
  WHERE LOWER(t.tag_key) = 'application'
  GROUP BY b.cost_daily_id
) app_tag ON app_tag.cost_daily_id = f.cost_daily_id
LEFT JOIN (
  SELECT b.cost_daily_id, MAX(t.tag_value) AS tag_value
  FROM bridge_cost_tag b
  INNER JOIN dim_tag t ON b.tag_sk = t.tag_sk
  WHERE LOWER(t.tag_key) = 'environment'
  GROUP BY b.cost_daily_id
) env_tag ON env_tag.cost_daily_id = f.cost_daily_id`
}

func (p *Processor) applicationExpr() string {
	return `COALESCE(NULLIF(TRIM(res.application), ''), NULLIF(TRIM(app_tag.tag_value), ''), '(Unassigned)')`
}

func (p *Processor) environmentExpr() string {
	return `COALESCE(NULLIF(TRIM(res.environment), ''), NULLIF(TRIM(env_tag.tag_value), ''), '(Unknown)')`
}

func (p *Processor) rebuildAppAggregates(ctx context.Context, tx *sql.Tx) error {
	tables := []string{
		"agg_app_monthly",
		"agg_app_service_monthly",
		"agg_app_service_resource_monthly",
		"agg_cost_distribution_monthly",
	}
	for _, t := range tables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t); err != nil {
			return err
		}
	}

	month := p.monthStartExpr("f.charge_date")
	app := p.applicationExpr()
	env := p.environmentExpr()
	billed := p.castCost("f.billed_cost")
	effective := p.castCost("f.effective_cost")
	now := p.nowUTC()
	joins := p.appContextJoins()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO agg_app_monthly (
		  month_start, provider, application, environment,
		  billed_cost, effective_cost, line_count, refreshed_utc)
		SELECT %s, a.provider, %s, %s,
		  SUM(%s), SUM(%s), SUM(f.line_count), %s
		FROM fact_focus_cost_daily f
		INNER JOIN dim_account a ON f.billing_account_sk = a.account_sk
		%s
		GROUP BY %s, a.provider, %s, %s`,
		month, app, env, billed, effective, now, joins, month, app, env)); err != nil {
		return fmt.Errorf("agg_app_monthly: %w", err)
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO agg_app_service_monthly (
		  month_start, provider, application, environment, service_sk,
		  billed_cost, effective_cost, line_count, refreshed_utc)
		SELECT %s, a.provider, %s, %s, f.service_sk,
		  SUM(%s), SUM(%s), SUM(f.line_count), %s
		FROM fact_focus_cost_daily f
		INNER JOIN dim_account a ON f.billing_account_sk = a.account_sk
		%s
		GROUP BY %s, a.provider, %s, %s, f.service_sk`,
		month, app, env, billed, effective, now, joins, month, app, env)); err != nil {
		return fmt.Errorf("agg_app_service_monthly: %w", err)
	}

	resKey := "COALESCE(CAST(f.resource_sk AS TEXT), '')"
	if p.Dialect == "sqlserver" {
		resKey = "COALESCE(CONVERT(VARCHAR(32), f.resource_sk), '')"
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO agg_app_service_resource_monthly (
		  month_start, provider, application, environment, service_sk, resource_sk,
		  billed_cost, effective_cost, line_count, refreshed_utc)
		SELECT %s, a.provider, %s, %s, f.service_sk, %s,
		  SUM(%s), SUM(%s), SUM(f.line_count), %s
		FROM fact_focus_cost_daily f
		INNER JOIN dim_account a ON f.billing_account_sk = a.account_sk
		%s
		GROUP BY %s, a.provider, %s, %s, f.service_sk, %s`,
		month, app, env, resKey, billed, effective, now, joins, month, app, env, resKey)); err != nil {
		return fmt.Errorf("agg_app_service_resource_monthly: %w", err)
	}

	return p.rebuildCostDistribution(ctx, tx)
}

func (p *Processor) rebuildCostDistribution(ctx context.Context, tx *sql.Tx) error {
	groups := map[distKey][]float64{}

	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT month_start, provider, application, SUM(%s)
		FROM agg_app_monthly
		GROUP BY month_start, provider, application`, p.castCost("billed_cost")))
	if err != nil {
		return err
	}
	for rows.Next() {
		var month, provider, app string
		var cost float64
		if err := rows.Scan(&month, &provider, &app, &cost); err != nil {
			rows.Close()
			return err
		}
		k := distKey{month: month, provider: provider, level: "APP"}
		groups[k] = append(groups[k], cost)
	}
	rows.Close()

	rows, err = tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT month_start, provider, application, environment, service_sk, %s
		FROM agg_app_service_monthly`, p.castCost("billed_cost")))
	if err != nil {
		return err
	}
	for rows.Next() {
		var month, provider, app, env string
		var svcSK int64
		var cost float64
		if err := rows.Scan(&month, &provider, &app, &env, &svcSK, &cost); err != nil {
			rows.Close()
			return err
		}
		k := distKey{month: month, provider: provider, level: "APP_SERVICE", parent: app + "|" + env}
		groups[k] = append(groups[k], cost)
	}
	rows.Close()

	rows, err = tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT month_start, provider, application, environment, service_sk, resource_sk, %s
		FROM agg_app_service_resource_monthly`, p.castCost("billed_cost")))
	if err != nil {
		return err
	}
	for rows.Next() {
		var month, provider, app, env, resSK string
		var svcSK int64
		var cost float64
		if err := rows.Scan(&month, &provider, &app, &env, &svcSK, &resSK, &cost); err != nil {
			rows.Close()
			return err
		}
		k := distKey{
			month: month, provider: provider, level: "APP_SERVICE_RESOURCE",
			parent: fmt.Sprintf("%s|%s|%d", app, env, svcSK),
		}
		groups[k] = append(groups[k], cost)
	}
	rows.Close()

	for k, costs := range groups {
		stats := computeDistribution(costs)
		var parent interface{}
		if k.parent != "" {
			parent = k.parent
		}
		q := `INSERT INTO agg_cost_distribution_monthly (
			month_start, provider, level_name, parent_key,
			entity_count, total_cost, min_cost, p50_cost, p75_cost, p90_cost, p95_cost, p99_cost,
			max_cost, avg_cost, gini, cr5, cr10, cr20, top_10_cost_pct, tail_80_cost_pct, refreshed_utc
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
		if p.Dialect == "sqlserver" {
			q = `INSERT INTO agg_cost_distribution_monthly (
			month_start, provider, level_name, parent_key,
			entity_count, total_cost, min_cost, p50_cost, p75_cost, p90_cost, p95_cost, p99_cost,
			max_cost, avg_cost, gini, cr5, cr10, cr20, top_10_cost_pct, tail_80_cost_pct, refreshed_utc
		) VALUES (@p1,@p2,@p3,@p4,@p5,@p6,@p7,@p8,@p9,@p10,@p11,@p12,@p13,@p14,@p15,@p16,@p17,@p18,@p19,@p20,SYSUTCDATETIME())`
		}
		if _, err := tx.ExecContext(ctx, p.q(q),
			k.month, k.provider, k.level, parent,
			stats.EntityCount,
			formatCost(stats.TotalCost), formatCost(stats.MinCost), formatCost(stats.P50Cost),
			formatCost(stats.P75Cost), formatCost(stats.P90Cost), formatCost(stats.P95Cost), formatCost(stats.P99Cost),
			formatCost(stats.MaxCost), formatCost(stats.AvgCost),
			formatRatio(stats.Gini), formatRatio(stats.CR5), formatRatio(stats.CR10), formatRatio(stats.CR20),
			formatRatio(stats.Top10CostPct), formatRatio(stats.Tail80CostPct),
		); err != nil {
			return fmt.Errorf("distribution %s/%s: %w", k.level, k.parent, err)
		}
	}
	return nil
}

func formatCost(v float64) string {
	return strconv.FormatFloat(v, 'f', 10, 64)
}

func formatRatio(v float64) string {
	return strconv.FormatFloat(v, 'f', 8, 64)
}
