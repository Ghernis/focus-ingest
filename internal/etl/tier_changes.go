package etl

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

type tierChangeEvent struct {
	scope          string
	month          string
	changeDate     string
	provider       string
	resourceSK     int64
	serviceSK      int64
	applicationSK  int64
	environment    string
	priorTierCode  string
	newTierCode    string
	priorTierRank  int
	newTierRank    int
	priorSkuSK     int64
	newSkuSK       int64
	priorUnitRate  float64
	newUnitRate    float64
	postChangeQty  float64
	daysPrior      int
	daysNew        int
	priorMonthCost float64
	currentCost    float64
}

func (p *Processor) buildFactResourceTierChanges(ctx context.Context, tx *sql.Tx, month string, daily []tierDailyRow, rollups map[string]*serviceTierRollup, refreshed interface{}) error {
	priorMonth := priorBillingMonth(month)
	priorDaily, err := p.loadTierDailyForMonth(ctx, tx, priorMonth)
	if err != nil {
		return err
	}

	events := detectIntraMonthTierChanges(daily)
	events = append(events, detectMoMTierChanges(month, priorMonth, daily, priorDaily)...)

	for _, ev := range events {
		unitSavings := (ev.priorUnitRate - ev.newUnitRate) * ev.postChangeQty
		costDelta := ev.priorMonthCost - ev.currentCost
		if ev.scope == changeScopeIntra {
			costDelta = (ev.priorUnitRate * ev.postChangeQty) - ev.currentCost
		}
		var projectedAnnual float64
		if ev.scope == changeScopeMoM {
			projectedAnnual = unitSavings * 12
		} else if ev.daysNew > 0 {
			projectedAnnual = unitSavings / float64(ev.daysNew) * 365
		}
		dir := tierChangeDirection(ev.priorTierRank, ev.newTierRank, ev.priorUnitRate, ev.newUnitRate)
		if dir == changeNeutral && ev.priorTierCode != ev.newTierCode {
			dir = tierChangeDirection(ev.priorTierRank, ev.newTierRank, ev.priorUnitRate, ev.newUnitRate)
		}

		if err := p.insertTierChangeFact(ctx, tx, ev, unitSavings, costDelta, projectedAnnual, dir, refreshed); err != nil {
			return err
		}
		mom := ev.scope == changeScopeMoM
		intra := ev.scope == changeScopeIntra
		addTierRollup(rollups, ev.provider, ev.serviceSK, unitSavings, costDelta, dir, mom, intra)
	}
	return nil
}

func (p *Processor) loadTierDailyForMonth(ctx context.Context, tx *sql.Tx, month string) ([]tierDailyRow, error) {
	if month == "" {
		return nil, nil
	}
	q := `SELECT charge_date, billing_period_start, provider, resource_sk, service_sk, application_sk, environment,
		tier_code, tier_rank, tier_sku_sk, tier_unit_rate, tier_cost, tier_qty
		FROM fact_resource_tier_daily WHERE ` + p.monthEq("billing_period_start", month)
	rows, err := tx.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []tierDailyRow
	for rows.Next() {
		var r tierDailyRow
		var unitRateStr, costStr, qtyStr string
		if err := rows.Scan(&r.chargeDate, &r.billingMonth, &r.provider, &r.resourceSK, &r.serviceSK, &r.applicationSK, &r.environment,
			&r.tierCode, &r.tierRank, &r.tierSkuSK, &unitRateStr, &costStr, &qtyStr); err != nil {
			return nil, err
		}
		r.tierUnitRate = parseDecimal(unitRateStr)
		r.tierCost = parseDecimal(costStr)
		r.tierQty = parseDecimal(qtyStr)
		out = append(out, r)
	}
	return out, rows.Err()
}

func detectIntraMonthTierChanges(daily []tierDailyRow) []tierChangeEvent {
	byKey := map[resourceServiceKey][]tierDailyRow{}
	for _, d := range daily {
		k := resourceServiceKey{resourceSK: d.resourceSK, serviceSK: d.serviceSK}
		byKey[k] = append(byKey[k], d)
	}
	var events []tierChangeEvent
	for k, rows := range byKey {
		sort.Slice(rows, func(i, j int) bool { return rows[i].chargeDate < rows[j].chargeDate })
		if len(rows) < 2 {
			continue
		}
		dates := make([]string, 0, len(rows))
		byDate := map[string]tierDailyRow{}
		for _, r := range rows {
			if _, ok := byDate[r.chargeDate]; !ok {
				dates = append(dates, r.chargeDate)
			}
			byDate[r.chargeDate] = r
		}
		sort.Strings(dates)
		for i := 1; i < len(dates); i++ {
			prev := byDate[dates[i-1]]
			cur := byDate[dates[i]]
			if prev.tierCode == cur.tierCode {
				continue
			}
			changeDate := cur.chargeDate
			daysPrior, daysNew := 0, 0
			var priorCost, newCost, totalQty float64
			for _, d := range dates {
				row := byDate[d]
				totalQty += row.tierQty
				if d < changeDate {
					daysPrior++
					if row.tierCode == prev.tierCode {
						priorCost += row.tierCost
					}
				} else {
					daysNew++
					if row.tierCode == cur.tierCode {
						newCost += row.tierCost
					}
				}
			}
			postQty := cur.tierQty
			if postQty <= 0 {
				postQty = totalQty
			}
			events = append(events, tierChangeEvent{
				scope:         changeScopeIntra,
				month:         cur.billingMonth,
				changeDate:    changeDate,
				provider:      cur.provider,
				resourceSK:    k.resourceSK,
				serviceSK:     k.serviceSK,
				applicationSK: cur.applicationSK,
				environment:   cur.environment,
				priorTierCode: prev.tierCode,
				newTierCode:   cur.tierCode,
				priorTierRank: prev.tierRank,
				newTierRank:   cur.tierRank,
				priorSkuSK:    prev.tierSkuSK,
				newSkuSK:      cur.tierSkuSK,
				priorUnitRate: prev.tierUnitRate,
				newUnitRate:   cur.tierUnitRate,
				postChangeQty: postQty,
				daysPrior:     daysPrior,
				daysNew:       daysNew,
				currentCost:   newCost,
			})
			break
		}
	}
	return events
}

func detectMoMTierChanges(month, priorMonth string, current, prior []tierDailyRow) []tierChangeEvent {
	curDom := dominantTierByMonth(current, month)
	priorDom := dominantTierByMonth(prior, priorMonth)
	var events []tierChangeEvent
	for k, cur := range curDom {
		prev, ok := priorDom[k]
		if !ok || prev.tierCode == cur.tierCode {
			continue
		}
		postQty := cur.tierQty
		events = append(events, tierChangeEvent{
			scope:          changeScopeMoM,
			month:          month,
			changeDate:     month,
			provider:       cur.provider,
			resourceSK:     k.resourceSK,
			serviceSK:      k.serviceSK,
			applicationSK:  cur.applicationSK,
			environment:    cur.environment,
			priorTierCode:  prev.tierCode,
			newTierCode:    cur.tierCode,
			priorTierRank:  prev.tierRank,
			newTierRank:    cur.tierRank,
			priorSkuSK:     prev.tierSkuSK,
			newSkuSK:       cur.tierSkuSK,
			priorUnitRate:  prev.tierUnitRate,
			newUnitRate:    cur.tierUnitRate,
			postChangeQty:  postQty,
			priorMonthCost: prev.tierCost,
			currentCost:    cur.tierCost,
		})
	}
	return events
}

func dominantTierByMonth(daily []tierDailyRow, month string) map[resourceServiceKey]tierDailyRow {
	byTier := map[resourceServiceKey]map[string]tierDailyRow{}
	for _, d := range daily {
		if d.billingMonth != month {
			continue
		}
		k := resourceServiceKey{d.resourceSK, d.serviceSK}
		if byTier[k] == nil {
			byTier[k] = map[string]tierDailyRow{}
		}
		prev := byTier[k][d.tierCode]
		prev.chargeDate = d.chargeDate
		prev.billingMonth = d.billingMonth
		prev.provider = d.provider
		prev.resourceSK = d.resourceSK
		prev.serviceSK = d.serviceSK
		prev.applicationSK = d.applicationSK
		prev.environment = d.environment
		prev.tierCode = d.tierCode
		prev.tierRank = d.tierRank
		prev.tierSkuSK = d.tierSkuSK
		prev.tierCost += d.tierCost
		prev.tierQty += d.tierQty
		byTier[k][d.tierCode] = prev
	}
	out := map[resourceServiceKey]tierDailyRow{}
	for k, tiers := range byTier {
		var best tierDailyRow
		for _, t := range tiers {
			t.tierUnitRate = unitRate(t.tierCost, t.tierQty)
			if t.tierCost > best.tierCost || (t.tierCost == best.tierCost && t.tierSkuSK < best.tierSkuSK) {
				best = t
			}
		}
		if best.tierCode != "" {
			out[k] = best
		}
	}
	return out
}

func (p *Processor) insertTierChangeFact(ctx context.Context, tx *sql.Tx, ev tierChangeEvent, unitSavings, costDelta, projectedAnnual float64, dir string, refreshed interface{}) error {
	insertSQL := `INSERT INTO fact_resource_tier_change (
		change_scope, month_start, change_date, provider, resource_sk, service_sk, application_sk, environment,
		prior_tier_code, new_tier_code, prior_tier_rank, new_tier_rank,
		prior_tier_sku_sk, new_tier_sku_sk, prior_unit_rate, new_unit_rate, post_change_quantity,
		days_on_prior_tier, days_on_new_tier,
		realized_savings_unit, realized_savings_cost_delta, projected_annual_savings, change_direction, refreshed_utc
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	if p.Dialect == "sqlserver" {
		insertSQL = `INSERT INTO fact_resource_tier_change (
		change_scope, month_start, change_date, provider, resource_sk, service_sk, application_sk, environment,
		prior_tier_code, new_tier_code, prior_tier_rank, new_tier_rank,
		prior_tier_sku_sk, new_tier_sku_sk, prior_unit_rate, new_unit_rate, post_change_quantity,
		days_on_prior_tier, days_on_new_tier,
		realized_savings_unit, realized_savings_cost_delta, projected_annual_savings, change_direction, refreshed_utc
	) VALUES (@p1,@p2,@p3,@p4,@p5,@p6,@p7,@p8,@p9,@p10,@p11,@p12,@p13,@p14,@p15,@p16,@p17,@p18,@p19,@p20,@p21,@p22,@p23,@p24)`
	}
	_, err := tx.ExecContext(ctx, p.q(insertSQL),
		ev.scope, ev.month, ev.changeDate, ev.provider, ev.resourceSK, ev.serviceSK, ev.applicationSK, ev.environment,
		ev.priorTierCode, ev.newTierCode, ev.priorTierRank, ev.newTierRank,
		ev.priorSkuSK, ev.newSkuSK, formatCost(ev.priorUnitRate), formatCost(ev.newUnitRate), formatCost(ev.postChangeQty),
		ev.daysPrior, ev.daysNew,
		formatCost(unitSavings), formatCost(costDelta), formatCost(projectedAnnual), dir, refreshed,
	)
	if err != nil {
		return fmt.Errorf("fact_resource_tier_change: %w", err)
	}
	return nil
}
