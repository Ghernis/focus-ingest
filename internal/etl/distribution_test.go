package etl

import (
	"math"
	"testing"
)

func TestComputeDistribution(t *testing.T) {
	stats := computeDistribution([]float64{100, 50, 30, 10, 10})
	if stats.EntityCount != 5 {
		t.Fatalf("entity_count=%d", stats.EntityCount)
	}
	if math.Abs(stats.TotalCost-200) > 1e-9 {
		t.Fatalf("total=%v", stats.TotalCost)
	}
	if stats.MaxCost != 100 || stats.MinCost != 10 {
		t.Fatalf("min/max %v %v", stats.MinCost, stats.MaxCost)
	}
	if stats.CR5 != 1.0 {
		t.Fatalf("cr5=%v", stats.CR5)
	}
	if stats.Top10CostPct != 100 {
		t.Fatalf("top10=%v", stats.Top10CostPct)
	}
	if math.Abs(stats.Top10CostPct-stats.CR10*100) > 1e-9 {
		t.Fatalf("top10 should equal cr10*100")
	}
}

func TestGiniEqual(t *testing.T) {
	g := giniCoefficient([]float64{25, 25, 25, 25})
	if math.Abs(g) > 1e-9 {
		t.Fatalf("gini equal=%v", g)
	}
}

func TestTail80CostPct_BottomEntities(t *testing.T) {
	// 10 entities: nine at 1, one at 991 → bottom 80% (8 entities) contribute 8/1000
	costs := []float64{1, 1, 1, 1, 1, 1, 1, 1, 1, 991}
	stats := computeDistribution(costs)
	if math.Abs(stats.Tail80CostPct-0.8) > 1e-6 {
		t.Fatalf("tail80=%v want 0.8 (%%)", stats.Tail80CostPct)
	}
}

func TestStdDevCost(t *testing.T) {
	stats := computeDistribution([]float64{10, 10, 10, 10})
	if math.Abs(stats.StdDevCost) > 1e-9 {
		t.Fatalf("stddev equal=%v", stats.StdDevCost)
	}
	stats = computeDistribution([]float64{0, 10})
	if math.Abs(stats.StdDevCost-5) > 1e-9 {
		t.Fatalf("stddev=%v want 5", stats.StdDevCost)
	}
}
