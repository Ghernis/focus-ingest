package etl

import "math"

type tierSavingsResult struct {
	totalQtyOnNewTier          float64
	counterfactualCostOnNewTier float64
	monthRealizedSavings       float64
	rateDiffTimesQty           float64
}

func computeIntramonthNewTierSavings(priorUnitRate float64, dates []string, byDate map[string]tierDailyRow, changeDate, newTierCode string) tierSavingsResult {
	var out tierSavingsResult
	for _, d := range dates {
		if d < changeDate {
			continue
		}
		row := byDate[d]
		if row.tierCode != newTierCode {
			continue
		}
		out.totalQtyOnNewTier += row.tierQty
		out.counterfactualCostOnNewTier += priorUnitRate * row.tierQty
		out.monthRealizedSavings += (priorUnitRate - row.tierUnitRate) * row.tierQty
	}
	out.rateDiffTimesQty = out.monthRealizedSavings
	return out
}

func computeMoMTierSavings(priorUnitRate, newUnitRate, qty, actualCost float64) tierSavingsResult {
	if qty <= 0 {
		qty = 0
	}
	savings := (priorUnitRate - newUnitRate) * qty
	if qty > 0 {
		return tierSavingsResult{
			totalQtyOnNewTier:           qty,
			counterfactualCostOnNewTier: priorUnitRate * qty,
			monthRealizedSavings:        savings,
			rateDiffTimesQty:              savings,
		}
	}
	// Fall back to dominant-tier totals when quantity is missing.
	counterfactual := priorUnitRate
	if actualCost > 0 && newUnitRate > 0 {
		counterfactual = priorUnitRate * (actualCost / newUnitRate)
	}
	return tierSavingsResult{
		totalQtyOnNewTier:           actualCost / math.Max(newUnitRate, 1e-12),
		counterfactualCostOnNewTier: counterfactual,
		monthRealizedSavings:        counterfactual - actualCost,
		rateDiffTimesQty:            counterfactual - actualCost,
	}
}

func projectedAnnualFromMonthSavings(monthSavings float64, scope string, daysOnNewTier int) float64 {
	if monthSavings <= 0 {
		return 0
	}
	switch scope {
	case changeScopeMoM:
		return monthSavings * 12
	case changeScopeIntra:
		if daysOnNewTier <= 0 {
			return 0
		}
		return monthSavings / float64(daysOnNewTier) * 365
	default:
		return 0
	}
}

func computeCarryForwardMonthDelta(baselineUnitRate, currentMonthQty, currentMonthActualCost float64) (counterfactualCost float64, monthDelta float64) {
	if currentMonthQty <= 0 {
		return 0, -currentMonthActualCost
	}
	counterfactualCost = baselineUnitRate * currentMonthQty
	monthDelta = counterfactualCost - currentMonthActualCost
	return counterfactualCost, monthDelta
}
