package etl

import (
	"math"
	"testing"
)

func TestComputeMonthlyAnomalies_Spike(t *testing.T) {
	points := []monthlyCostPoint{
		{month: "2024-01-01", cost: 100},
		{month: "2024-02-01", cost: 110},
		{month: "2024-03-01", cost: 105},
		{month: "2024-04-01", cost: 500},
	}
	metrics := computeMonthlyAnomalies(points)
	if len(metrics) != 4 {
		t.Fatalf("len=%d", len(metrics))
	}
	last := metrics[3]
	if !last.anomalyFlag {
		t.Fatalf("expected anomaly flag on spike month")
	}
	if last.zScore <= anomalyZThreshold {
		t.Fatalf("z_score=%v", last.zScore)
	}
	if math.Abs(last.currentCost-500) > 1e-9 {
		t.Fatalf("current=%v", last.currentCost)
	}
	if last.anomalyType != anomalyTypeSpike {
		t.Fatalf("anomaly_type=%q want SPIKE", last.anomalyType)
	}
}

func TestComputeMonthlyAnomalies_InsufficientHistory(t *testing.T) {
	points := []monthlyCostPoint{{month: "2024-01-01", cost: 100}}
	metrics := computeMonthlyAnomalies(points)
	if metrics[0].anomalyFlag {
		t.Fatal("should not flag without enough history")
	}
	if metrics[0].historyMonths != 0 {
		t.Fatalf("history=%d", metrics[0].historyMonths)
	}
	if metrics[0].anomalyType != anomalyTypeNew {
		t.Fatalf("anomaly_type=%q want NEW", metrics[0].anomalyType)
	}
}

func TestClassifyAnomalyType_Surge(t *testing.T) {
	points := []monthlyCostPoint{
		{month: "2024-01-01", cost: 100},
		{month: "2024-02-01", cost: 50},
		{month: "2024-03-01", cost: 150},
		{month: "2024-04-01", cost: 200},
	}
	last := computeMonthlyAnomalies(points)[3]
	if last.anomalyType != anomalyTypeSurge {
		t.Fatalf("anomaly_type=%q want SURGE, z=%v", last.anomalyType, last.zScore)
	}
}

func TestClassifyAnomalyType_Drop(t *testing.T) {
	points := []monthlyCostPoint{
		{month: "2024-01-01", cost: 100},
		{month: "2024-02-01", cost: 50},
		{month: "2024-03-01", cost: 150},
		{month: "2024-04-01", cost: 0},
	}
	last := computeMonthlyAnomalies(points)[3]
	if last.anomalyType != anomalyTypeDrop {
		t.Fatalf("anomaly_type=%q want DROP, z=%v", last.anomalyType, last.zScore)
	}
}

func TestClassifyAnomalyType_Normal(t *testing.T) {
	points := []monthlyCostPoint{
		{month: "2024-01-01", cost: 100},
		{month: "2024-02-01", cost: 110},
		{month: "2024-03-01", cost: 105},
		{month: "2024-04-01", cost: 108},
	}
	last := computeMonthlyAnomalies(points)[3]
	if last.anomalyType != anomalyTypeNormal {
		t.Fatalf("anomaly_type=%q want NORMAL", last.anomalyType)
	}
}
