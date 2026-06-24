package etl

import "testing"

func TestDominantSkuByResourceMonth(t *testing.T) {
	aggs := []skuMonthAgg{
		{month: "2024-01-01", resourceSK: 1, skuSK: 10, skuCost: 90, skuQty: 9},
		{month: "2024-01-01", resourceSK: 1, skuSK: 20, skuCost: 10, skuQty: 1},
	}
	dom := dominantSkuByResourceMonth(aggs, "2024-01-01")
	if dom[1].skuSK != 10 {
		t.Fatalf("dominant sku=%d want 10", dom[1].skuSK)
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

func TestDetectIntraMonthChanges(t *testing.T) {
	keys := map[int64]string{1: "AWS|SKU-LARGE", 2: "AWS|SKU-SMALL"}
	days := []dailySkuAgg{
		{chargeDate: "2024-03-10", skuSK: 1, cost: 100, qty: 10},
		{chargeDate: "2024-03-20", skuSK: 2, cost: 40, qty: 10},
	}
	events := detectIntraMonthChanges(days, keys)
	if len(events) != 1 {
		t.Fatalf("events=%d", len(events))
	}
	if events[0].changeDate != "2024-03-20" || events[0].priorSKU != 1 || events[0].newSKU != 2 {
		t.Fatalf("unexpected event: %+v", events[0])
	}
}
