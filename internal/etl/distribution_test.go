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
}

func TestGiniEqual(t *testing.T) {
	g := giniCoefficient([]float64{25, 25, 25, 25})
	if math.Abs(g) > 1e-9 {
		t.Fatalf("gini equal=%v", g)
	}
}
