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
}
