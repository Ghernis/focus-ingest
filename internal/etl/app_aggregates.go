package etl

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"
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

// billingMonthStartExpr returns the billing period start date used as the monthly grain.
func (p *Processor) billingMonthStartExpr() string {
	return "f.billing_period_start"
}

func (p *Processor) subAccountJoin() string {
	return `
INNER JOIN dim_sub_account sa ON f.sub_account_sk = sa.sub_account_sk
INNER JOIN dim_account a ON sa.billing_account_sk = a.account_sk`
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

// refreshedUTCParam returns a timestamp value for INSERT bind parameters.
// nowUTC() is a SQL expression for generated SELECT statements only.
func (p *Processor) refreshedUTCParam() interface{} {
	t := time.Now().UTC()
	if p.Dialect == "sqlserver" {
		return t
	}
	return t.Format("2006-01-02 15:04:05")
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

func (p *Processor) environmentExpr() string {
	return `COALESCE(NULLIF(TRIM(res.environment), ''), NULLIF(TRIM(env_tag.tag_value), ''), '(Unknown)')`
}

func (p *Processor) rebuildAppAggregates(ctx context.Context, tx *sql.Tx) error {
	tables := []string{
		"agg_app_monthly",
		"agg_app_service_monthly",
		"agg_app_service_resource_monthly",
		"agg_cost_distribution_monthly",
		"agg_cost_anomaly_monthly",
	}
	for _, t := range tables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t); err != nil {
			return err
		}
	}

	if err := p.syncApplicationsFromFacts(ctx, tx); err != nil {
		return fmt.Errorf("sync applications: %w", err)
	}

	month := p.billingMonthStartExpr()
	env := p.environmentExpr()
	billed := p.castCost("f.billed_cost")
	effective := p.castCost("f.effective_cost")
	now := p.nowUTC()
	joins := p.appContextJoins()
	appJoin := p.applicationDimJoin()
	appSK := p.applicationSKExpr()
	subJoin := p.subAccountJoin()

	if err := p.ensureApplicationsForFactCanon(ctx, tx, ""); err != nil {
		return fmt.Errorf("ensure app dims: %w", err)
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO agg_app_monthly (
		  month_start, provider, application_sk, environment,
		  billed_cost, effective_cost, line_count, refreshed_utc)
		SELECT %s, a.provider, %s, %s,
		  SUM(%s), SUM(%s), SUM(f.line_count), %s
		FROM fact_focus_cost_daily f
		%s
		%s
		%s
		WHERE f.sub_account_sk IS NOT NULL
		GROUP BY %s, a.provider, %s, %s`,
		month, appSK, env, billed, effective, now, subJoin, joins, appJoin, month, appSK, env)); err != nil {
		return fmt.Errorf("agg_app_monthly: %w", err)
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO agg_app_service_monthly (
		  month_start, provider, application_sk, environment, service_sk,
		  billed_cost, effective_cost, line_count, refreshed_utc)
		SELECT %s, a.provider, %s, %s, f.service_sk,
		  SUM(%s), SUM(%s), SUM(f.line_count), %s
		FROM fact_focus_cost_daily f
		%s
		%s
		%s
		WHERE f.sub_account_sk IS NOT NULL
		GROUP BY %s, a.provider, %s, %s, f.service_sk`,
		month, appSK, env, billed, effective, now, subJoin, joins, appJoin, month, appSK, env)); err != nil {
		return fmt.Errorf("agg_app_service_monthly: %w", err)
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO agg_app_service_resource_monthly (
		  month_start, provider, application_sk, environment, service_sk, resource_sk,
		  billed_cost, effective_cost, line_count, refreshed_utc)
		SELECT %s, a.provider, %s, %s, f.service_sk, f.resource_sk,
		  SUM(%s), SUM(%s), SUM(f.line_count), %s
		FROM fact_focus_cost_daily f
		%s
		%s
		%s
		WHERE f.sub_account_sk IS NOT NULL AND f.resource_sk IS NOT NULL
		GROUP BY %s, a.provider, %s, %s, f.service_sk, f.resource_sk`,
		month, appSK, env, billed, effective, now, subJoin, joins, appJoin, month, appSK, env)); err != nil {
		return fmt.Errorf("agg_app_service_resource_monthly: %w", err)
	}

	if err := p.rebuildCostDistribution(ctx, tx); err != nil {
		return err
	}
	return p.rebuildCostAnomalies(ctx, tx)
}

func (p *Processor) RebuildCostDistributionForMonth(ctx context.Context, tx *sql.Tx, month string) error {
	return p.rebuildCostDistributionForMonth(ctx, tx, month)
}

func (p *Processor) rebuildCostDistributionForMonth(ctx context.Context, tx *sql.Tx, month string) error {
	return p.rebuildCostDistributionFiltered(ctx, tx, p.monthEq("month_start", month))
}

func (p *Processor) rebuildCostDistribution(ctx context.Context, tx *sql.Tx) error {
	return p.rebuildCostDistributionFiltered(ctx, tx, "")
}

func (p *Processor) rebuildCostDistributionFiltered(ctx context.Context, tx *sql.Tx, monthWhere string) error {
	// Built only from agg_app_* tables (not raw facts/staging).
	groups := map[distKey][]float64{}
	costCol := p.castCost("billed_cost")

	appWhere := ""
	if monthWhere != "" {
		appWhere = "WHERE " + monthWhere
	}
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT month_start, provider, application_sk, SUM(%s)
		FROM agg_app_monthly
		%s
		GROUP BY month_start, provider, application_sk`, costCol, appWhere))
	if err != nil {
		return err
	}
	for rows.Next() {
		var month, provider string
		var appSK int64
		var cost float64
		if err := rows.Scan(&month, &provider, &appSK, &cost); err != nil {
			rows.Close()
			return err
		}
		k := distKey{month: month, provider: provider, level: "APP"}
		groups[k] = append(groups[k], cost)
	}
	rows.Close()

	// Per-application: distribution of environment-level costs within each app.
	rows, err = tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT month_start, provider, application_sk, environment, SUM(%s)
		FROM agg_app_monthly
		%s
		GROUP BY month_start, provider, application_sk, environment`, costCol, appWhere))
	if err != nil {
		return err
	}
	for rows.Next() {
		var month, provider, env string
		var appSK int64
		var cost float64
		if err := rows.Scan(&month, &provider, &appSK, &env, &cost); err != nil {
			rows.Close()
			return err
		}
		k := distKey{
			month: month, provider: provider, level: "APP",
			parent: strconv.FormatInt(appSK, 10),
		}
		groups[k] = append(groups[k], cost)
	}
	rows.Close()

	rows, err = tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT month_start, provider, application_sk, service_sk, SUM(%s)
		FROM agg_app_service_monthly
		%s
		GROUP BY month_start, provider, application_sk, service_sk`, costCol, appWhere))
	if err != nil {
		return err
	}
	for rows.Next() {
		var month, provider string
		var appSK, svcSK int64
		var cost float64
		if err := rows.Scan(&month, &provider, &appSK, &svcSK, &cost); err != nil {
			rows.Close()
			return err
		}
		k := distKey{month: month, provider: provider, level: "SERVICE", parent: strconv.FormatInt(appSK, 10)}
		groups[k] = append(groups[k], cost)
	}
	rows.Close()

	rows, err = tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT month_start, provider, application_sk, service_sk, resource_sk, SUM(%s)
		FROM agg_app_service_resource_monthly
		%s
		GROUP BY month_start, provider, application_sk, service_sk, resource_sk`, costCol, appWhere))
	if err != nil {
		return err
	}
	for rows.Next() {
		var month, provider string
		var appSK, svcSK, resSK int64
		var cost float64
		if err := rows.Scan(&month, &provider, &appSK, &svcSK, &resSK, &cost); err != nil {
			rows.Close()
			return err
		}
		k := distKey{
			month: month, provider: provider, level: "RESOURCE",
			parent: fmt.Sprintf("%d|%d", appSK, svcSK),
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
			max_cost, avg_cost, stddev_cost, gini, cr5, cr10, cr20, top_10_cost_pct, tail_80_cost_pct, refreshed_utc
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,datetime('now'))`
		if p.Dialect == "sqlserver" {
			q = `INSERT INTO agg_cost_distribution_monthly (
			month_start, provider, level_name, parent_key,
			entity_count, total_cost, min_cost, p50_cost, p75_cost, p90_cost, p95_cost, p99_cost,
			max_cost, avg_cost, stddev_cost, gini, cr5, cr10, cr20, top_10_cost_pct, tail_80_cost_pct, refreshed_utc
		) VALUES (@p1,@p2,@p3,@p4,@p5,@p6,@p7,@p8,@p9,@p10,@p11,@p12,@p13,@p14,@p15,@p16,@p17,@p18,@p19,@p20,@p21,SYSUTCDATETIME())`
		}
		if _, err := tx.ExecContext(ctx, p.q(q),
			k.month, k.provider, k.level, parent,
			stats.EntityCount,
			formatCost(stats.TotalCost), formatCost(stats.MinCost), formatCost(stats.P50Cost),
			formatCost(stats.P75Cost), formatCost(stats.P90Cost), formatCost(stats.P95Cost), formatCost(stats.P99Cost),
			formatCost(stats.MaxCost), formatCost(stats.AvgCost), formatCost(stats.StdDevCost),
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
