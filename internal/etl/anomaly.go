package etl

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
)

const (
	anomalyZThreshold     = 2.5
	anomalyMinHistoryFlag = 2
	anomalyLookbackMonths = 3
	anomalySpikeZ         = 3.0
	anomalySurgePct       = 0.5
)

const (
	anomalyTypeNew    = "NEW"
	anomalyTypeSpike  = "SPIKE"
	anomalyTypeSurge  = "SURGE"
	anomalyTypeDrop   = "DROP"
	anomalyTypeNormal = "NORMAL"
)

type monthlyCostPoint struct {
	month string
	cost  float64
}

type anomalyMetric struct {
	month          string
	provider       string
	entityLevel    string
	applicationSK  int64
	serviceSK      int64 // 0 when not applicable
	currentCost    float64
	avg3m          float64
	stddev3m       float64
	zScore         float64
	pctChangeVsAvg float64
	historyMonths  int
	anomalyFlag    bool
	anomalyType    string
}

func classifyAnomalyType(historyMonths int, currentCost, avg3m, zScore float64) string {
	if historyMonths < 2 {
		return anomalyTypeNew
	}
	if math.Abs(zScore) > anomalySpikeZ {
		return anomalyTypeSpike
	}
	if avg3m > 0 && (currentCost-avg3m)/avg3m > anomalySurgePct {
		return anomalyTypeSurge
	}
	if currentCost == 0 && avg3m > 0 {
		return anomalyTypeDrop
	}
	return anomalyTypeNormal
}

func computeMonthlyAnomalies(points []monthlyCostPoint) []anomalyMetric {
	if len(points) == 0 {
		return nil
	}
	sorted := append([]monthlyCostPoint(nil), points...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].month < sorted[j].month })

	var out []anomalyMetric
	for i, pt := range sorted {
		start := i - anomalyLookbackMonths
		if start < 0 {
			start = 0
		}
		prior := sorted[start:i]
		history := len(prior)
		avg := 0.0
		stddev := 0.0
		if history > 0 {
			var sum float64
			for _, p := range prior {
				sum += p.cost
			}
			avg = sum / float64(history)
			stddev = stdDevPopulation(costsFromPoints(prior), avg)
		}

		z := 0.0
		flag := false
		if history >= anomalyMinHistoryFlag {
			if stddev > 0 {
				z = (pt.cost - avg) / stddev
				flag = math.Abs(z) >= anomalyZThreshold
			} else if math.Abs(pt.cost-avg) > 1e-9 {
				z = 9.99
				if pt.cost > avg {
					z = 9.99
				} else {
					z = -9.99
				}
				flag = true
			}
		}

		pct := 0.0
		if avg > 0 {
			pct = ((pt.cost - avg) / avg) * 100
		}

		out = append(out, anomalyMetric{
			month:          pt.month,
			currentCost:    pt.cost,
			avg3m:          avg,
			stddev3m:       stddev,
			zScore:         z,
			pctChangeVsAvg: pct,
			historyMonths:  history,
			anomalyFlag:    flag,
			anomalyType:    classifyAnomalyType(history, pt.cost, avg, z),
		})
	}
	return out
}

func costsFromPoints(points []monthlyCostPoint) []float64 {
	out := make([]float64, len(points))
	for i, p := range points {
		out[i] = p.cost
	}
	return out
}

func stdDevPopulation(values []float64, mean float64) float64 {
	if len(values) <= 1 {
		return 0
	}
	var sumSq float64
	for _, v := range values {
		d := v - mean
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(len(values)))
}

func (p *Processor) rebuildCostAnomaliesForMonth(ctx context.Context, tx *sql.Tx, month string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM agg_cost_anomaly_monthly WHERE `+monthEq("month_start", month)); err != nil {
		return fmt.Errorf("delete anomalies: %w", err)
	}
	if err := p.rebuildAppAnomalies(ctx, tx, month); err != nil {
		return err
	}
	return p.rebuildServiceAnomalies(ctx, tx, month)
}

func (p *Processor) rebuildCostAnomalies(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM agg_cost_anomaly_monthly`); err != nil {
		return fmt.Errorf("delete anomalies: %w", err)
	}
	if err := p.rebuildAppAnomalies(ctx, tx, ""); err != nil {
		return err
	}
	return p.rebuildServiceAnomalies(ctx, tx, "")
}

func (p *Processor) rebuildAppAnomalies(ctx context.Context, tx *sql.Tx, onlyMonth string) error {
	costCol := p.castCost("billed_cost")
	scope := ""
	if onlyMonth != "" {
		scope = fmt.Sprintf(`WHERE application_sk IN (
			SELECT DISTINCT application_sk FROM agg_app_monthly WHERE %s
		)`, monthEq("month_start", onlyMonth))
	}
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT month_start, provider, application_sk, SUM(%s)
		FROM agg_app_monthly
		%s
		GROUP BY month_start, provider, application_sk
		ORDER BY provider, application_sk, month_start`, costCol, scope))
	if err != nil {
		return fmt.Errorf("load app monthly: %w", err)
	}
	defer rows.Close()

	type key struct {
		provider string
		appSK    int64
	}
	series := map[key][]monthlyCostPoint{}
	for rows.Next() {
		var month, provider string
		var appSK int64
		var cost float64
		if err := rows.Scan(&month, &provider, &appSK, &cost); err != nil {
			return err
		}
		k := key{provider: provider, appSK: appSK}
		series[k] = append(series[k], monthlyCostPoint{month: month, cost: cost})
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for k, pts := range series {
		for _, m := range computeMonthlyAnomalies(pts) {
			if onlyMonth != "" && m.month != onlyMonth {
				continue
			}
			if err := p.insertAnomalyRow(ctx, tx, m, k.provider, "APP", k.appSK, 0); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Processor) rebuildServiceAnomalies(ctx context.Context, tx *sql.Tx, onlyMonth string) error {
	costCol := p.castCost("billed_cost")
	scope := ""
	if onlyMonth != "" {
		scope = fmt.Sprintf(`WHERE application_sk IN (
			SELECT DISTINCT application_sk FROM agg_app_service_monthly WHERE %s
		)`, monthEq("month_start", onlyMonth))
	}
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT month_start, provider, application_sk, service_sk, SUM(%s)
		FROM agg_app_service_monthly
		%s
		GROUP BY month_start, provider, application_sk, service_sk
		ORDER BY provider, application_sk, service_sk, month_start`, costCol, scope))
	if err != nil {
		return fmt.Errorf("load app service monthly: %w", err)
	}
	defer rows.Close()

	type key struct {
		provider string
		appSK    int64
		svcSK    int64
	}
	series := map[key][]monthlyCostPoint{}
	for rows.Next() {
		var month, provider string
		var appSK, svcSK int64
		var cost float64
		if err := rows.Scan(&month, &provider, &appSK, &svcSK, &cost); err != nil {
			return err
		}
		k := key{provider: provider, appSK: appSK, svcSK: svcSK}
		series[k] = append(series[k], monthlyCostPoint{month: month, cost: cost})
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for k, pts := range series {
		for _, m := range computeMonthlyAnomalies(pts) {
			if onlyMonth != "" && m.month != onlyMonth {
				continue
			}
			if err := p.insertAnomalyRow(ctx, tx, m, k.provider, "SERVICE", k.appSK, k.svcSK); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Processor) insertAnomalyRow(ctx context.Context, tx *sql.Tx, m anomalyMetric, provider, level string, appSK, svcSK int64) error {
	var svc interface{}
	if level == "SERVICE" {
		svc = svcSK
	}
	flag := 0
	if m.anomalyFlag {
		flag = 1
	}

	q := `INSERT INTO agg_cost_anomaly_monthly (
		month_start, provider, entity_level, application_sk, service_sk,
		billed_cost_current, billed_cost_avg_3m, billed_cost_stddev_3m,
		z_score, pct_change_vs_avg, history_months, anomaly_flag, anomaly_type, refreshed_utc
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,datetime('now'))`
	if p.Dialect == "sqlserver" {
		q = `INSERT INTO agg_cost_anomaly_monthly (
		month_start, provider, entity_level, application_sk, service_sk,
		billed_cost_current, billed_cost_avg_3m, billed_cost_stddev_3m,
		z_score, pct_change_vs_avg, history_months, anomaly_flag, anomaly_type, refreshed_utc
	) VALUES (@p1,@p2,@p3,@p4,@p5,@p6,@p7,@p8,@p9,@p10,@p11,@p12,@p13,SYSUTCDATETIME())`
	}

	_, err := tx.ExecContext(ctx, p.q(q),
		m.month, provider, level, appSK, svc,
		formatCost(m.currentCost), formatCost(m.avg3m), formatCost(m.stddev3m),
		formatRatio(m.zScore), formatRatio(m.pctChangeVsAvg),
		m.historyMonths, flag, m.anomalyType,
	)
	return err
}
