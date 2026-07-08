package etl

import "testing"

func TestPickDominantTierDaily(t *testing.T) {
	byTier := map[tierDayTierKey]tierDailyRow{
		{tierDayKey: tierDayKey{chargeDate: "2024-01-01", billingMonth: "2024-01-01", provider: "AZURE", resourceSK: 1, serviceSK: 1}, tierCode: "D4s v5", tierSkuSK: 10}: {tierCost: 90, tierQty: 9, tierCode: "D4s v5"},
		{tierDayKey: tierDayKey{chargeDate: "2024-01-01", billingMonth: "2024-01-01", provider: "AZURE", resourceSK: 1, serviceSK: 1}, tierCode: "D2s v5", tierSkuSK: 20}: {tierCost: 10, tierQty: 1, tierCode: "D2s v5"},
	}
	dom := pickDominantTierDaily(byTier)
	if len(dom) != 1 || dom[0].tierCode != "D4s v5" {
		t.Fatalf("dominant=%+v", dom)
	}
}

func TestChangeDirection(t *testing.T) {
	if changeDirection(10, 5) != changeDownsize {
		t.Fatal("expected downsize")
	}
	if changeDirection(5, 10) != changeUpsize {
		t.Fatal("expected upsize")
	}
	if changeDirection(5, 5) != changeNeutral {
		t.Fatal("expected neutral")
	}
}

func TestDetectIntraMonthTierChanges(t *testing.T) {
	daily := []tierDailyRow{
		{chargeDate: "2024-03-10", billingMonth: "2024-03-01", provider: "AZURE", resourceSK: 1, serviceSK: 1, tierCode: "D4s v5", tierRank: 680404, tierSkuSK: 1, tierUnitRate: 10, tierCost: 100, tierQty: 10},
		{chargeDate: "2024-03-20", billingMonth: "2024-03-01", provider: "AZURE", resourceSK: 1, serviceSK: 1, tierCode: "D2s v5", tierRank: 680204, tierSkuSK: 2, tierUnitRate: 4, tierCost: 40, tierQty: 10},
	}
	events := detectIntraMonthTierChanges(daily)
	if len(events) != 1 {
		t.Fatalf("events=%d", len(events))
	}
	if events[0].changeDate != "2024-03-20" || events[0].priorTierCode != "D4s v5" || events[0].newTierCode != "D2s v5" {
		t.Fatalf("unexpected event: %+v", events[0])
	}
	if events[0].daysPrior != 1 || events[0].daysNew != 1 {
		t.Fatalf("days prior=%d new=%d want 1/1", events[0].daysPrior, events[0].daysNew)
	}
}

func TestDetectIntraMonthTierChanges_MultipleTransitions(t *testing.T) {
	daily := []tierDailyRow{
		{chargeDate: "2024-03-10", billingMonth: "2024-03-01", provider: "AZURE", resourceSK: 7, serviceSK: 9, tierCode: "D8s v5", tierRank: 680804, tierSkuSK: 1, tierUnitRate: 8, tierCost: 80, tierQty: 10},
		{chargeDate: "2024-03-15", billingMonth: "2024-03-01", provider: "AZURE", resourceSK: 7, serviceSK: 9, tierCode: "D4s v5", tierRank: 680404, tierSkuSK: 2, tierUnitRate: 4, tierCost: 40, tierQty: 10},
		{chargeDate: "2024-03-20", billingMonth: "2024-03-01", provider: "AZURE", resourceSK: 7, serviceSK: 9, tierCode: "D2s v5", tierRank: 680204, tierSkuSK: 3, tierUnitRate: 2, tierCost: 20, tierQty: 10},
	}

	events := detectIntraMonthTierChanges(daily)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	if events[0].changeDate != "2024-03-15" || events[0].priorTierCode != "D8s v5" || events[0].newTierCode != "D4s v5" {
		t.Fatalf("unexpected first event: %+v", events[0])
	}
	if events[1].changeDate != "2024-03-20" || events[1].priorTierCode != "D4s v5" || events[1].newTierCode != "D2s v5" {
		t.Fatalf("unexpected second event: %+v", events[1])
	}

	if events[0].monthRealizedSavings != 40 {
		t.Fatalf("expected first event savings 40, got %v", events[0].monthRealizedSavings)
	}
	if events[1].monthRealizedSavings != 20 {
		t.Fatalf("expected second event savings 20, got %v", events[1].monthRealizedSavings)
	}

	// days_on_prior_tier = length of the run being left, not cumulative index from month start.
	if events[0].daysPrior != 1 || events[0].daysNew != 1 {
		t.Fatalf("event 1 days prior=%d new=%d want 1/1", events[0].daysPrior, events[0].daysNew)
	}
	if events[1].daysPrior != 1 || events[1].daysNew != 1 {
		t.Fatalf("event 2 days prior=%d new=%d want 1/1", events[1].daysPrior, events[1].daysNew)
	}
}
