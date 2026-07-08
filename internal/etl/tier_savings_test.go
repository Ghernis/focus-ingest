package etl

import "testing"

func TestComputeIntramonthNewTierSavings(t *testing.T) {
	daily := []tierDailyRow{
		{chargeDate: "2026-01-10", tierCode: "D4s v3", tierUnitRate: 10, tierCost: 100, tierQty: 10},
		{chargeDate: "2026-01-20", tierCode: "D2s v5", tierUnitRate: 4, tierCost: 40, tierQty: 10},
	}
	dates := []string{"2026-01-10", "2026-01-20"}
	byDate := map[string]tierDailyRow{
		"2026-01-10": daily[0],
		"2026-01-20": daily[1],
	}
	got := computeIntramonthNewTierSavings(10, dates, byDate, "2026-01-20", "D2s v5")
	if got.totalQtyOnNewTier != 10 {
		t.Fatalf("qty=%v", got.totalQtyOnNewTier)
	}
	if got.monthRealizedSavings != 60 {
		t.Fatalf("month savings=%v want 60", got.monthRealizedSavings)
	}
	if got.counterfactualCostOnNewTier != 100 {
		t.Fatalf("counterfactual=%v want 100", got.counterfactualCostOnNewTier)
	}
}

func TestComputeMoMTierSavings(t *testing.T) {
	got := computeMoMTierSavings(10, 4, 10, 40)
	if got.monthRealizedSavings != 60 {
		t.Fatalf("month savings=%v", got.monthRealizedSavings)
	}
	if got.counterfactualCostOnNewTier != 100 {
		t.Fatalf("counterfactual=%v", got.counterfactualCostOnNewTier)
	}
}

func TestProjectedAnnualFromMonthSavings(t *testing.T) {
	if got := projectedAnnualFromMonthSavings(60, changeScopeMoM, 0); got != 720 {
		t.Fatalf("mom annual=%v", got)
	}
	if got := projectedAnnualFromMonthSavings(13, changeScopeIntra, 13); got != 365 {
		t.Fatalf("intra annual=%v", got)
	}
}

func TestDetectIntraMonthTierChanges_Savings(t *testing.T) {
	daily := []tierDailyRow{
		{chargeDate: "2024-03-10", billingMonth: "2024-03-01", provider: "AZURE", resourceSK: 1, serviceSK: 1, tierCode: "D4s v5", tierRank: 680404, tierSkuSK: 1, tierUnitRate: 10, tierCost: 100, tierQty: 10},
		{chargeDate: "2024-03-20", billingMonth: "2024-03-01", provider: "AZURE", resourceSK: 1, serviceSK: 1, tierCode: "D2s v5", tierRank: 680204, tierSkuSK: 2, tierUnitRate: 4, tierCost: 40, tierQty: 10},
	}
	events := detectIntraMonthTierChanges(daily)
	if len(events) != 1 {
		t.Fatalf("events=%d", len(events))
	}
	if events[0].monthRealizedSavings != 60 {
		t.Fatalf("monthRealizedSavings=%v", events[0].monthRealizedSavings)
	}
	if events[0].counterfactualCost != 100 {
		t.Fatalf("counterfactual=%v", events[0].counterfactualCost)
	}
}

func TestTierChangeDirection_PrefersRate(t *testing.T) {
	// D4s v3 -> F4s v2 would rank as upsize, but lower rate means downsize.
	if got := tierChangeDirection(680403, 700402, 0.0736, 0.0664); got != changeDownsize {
		t.Fatalf("got %s want DOWNSIZE", got)
	}
}

func TestComputeCarryForwardMonthDelta_Positive(t *testing.T) {
	counterfactual, delta := computeCarryForwardMonthDelta(10, 10, 40)
	if counterfactual != 100 {
		t.Fatalf("counterfactual=%v want 100", counterfactual)
	}
	if delta != 60 {
		t.Fatalf("delta=%v want 60", delta)
	}
}

func TestComputeCarryForwardMonthDelta_Negative(t *testing.T) {
	counterfactual, delta := computeCarryForwardMonthDelta(4, 10, 80)
	if counterfactual != 40 {
		t.Fatalf("counterfactual=%v want 40", counterfactual)
	}
	if delta != -40 {
		t.Fatalf("delta=%v want -40", delta)
	}
}
