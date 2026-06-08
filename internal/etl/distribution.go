package etl

import (
	"math"
	"sort"
)

// distributionStats summarizes billed_cost across entities at one hierarchy level.
type distributionStats struct {
	EntityCount   int
	TotalCost     float64
	MinCost       float64
	P50Cost       float64
	P75Cost       float64
	P90Cost       float64
	P95Cost       float64
	P99Cost       float64
	MaxCost       float64
	AvgCost       float64
	StdDevCost    float64
	Gini          float64
	CR5           float64
	CR10          float64
	CR20          float64
	Top10CostPct  float64
	Tail80CostPct float64
}

func computeDistribution(costs []float64) distributionStats {
	var out distributionStats
	if len(costs) == 0 {
		return out
	}
	out.EntityCount = len(costs)

	sorted := append([]float64(nil), costs...)
	sort.Float64s(sorted)

	var total float64
	for _, c := range sorted {
		total += c
	}
	out.TotalCost = total
	if total == 0 {
		return out
	}

	out.MinCost = sorted[0]
	out.MaxCost = sorted[len(sorted)-1]
	out.AvgCost = total / float64(len(sorted))
	out.StdDevCost = stdDevCost(sorted, out.AvgCost)
	out.P50Cost = percentile(sorted, 0.50)
	out.P75Cost = percentile(sorted, 0.75)
	out.P90Cost = percentile(sorted, 0.90)
	out.P95Cost = percentile(sorted, 0.95)
	out.P99Cost = percentile(sorted, 0.99)
	out.Gini = giniCoefficient(sorted)

	desc := append([]float64(nil), sorted...)
	sort.Sort(sort.Reverse(sort.Float64Slice(desc)))

	out.CR5 = concentrationRatio(desc, total, 5)
	out.CR10 = concentrationRatio(desc, total, 10)
	out.CR20 = concentrationRatio(desc, total, 20)
	out.Top10CostPct = out.CR10 * 100
	out.Tail80CostPct = tail80CostPct(sorted, total)
	return out
}

func stdDevCost(sortedAsc []float64, mean float64) float64 {
	n := len(sortedAsc)
	if n <= 1 {
		return 0
	}
	var sumSq float64
	for _, c := range sortedAsc {
		d := c - mean
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(n))
}

func percentile(sortedAsc []float64, p float64) float64 {
	n := len(sortedAsc)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sortedAsc[0]
	}
	rank := p * float64(n-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return sortedAsc[lo]
	}
	frac := rank - float64(lo)
	return sortedAsc[lo]*(1-frac) + sortedAsc[hi]*frac
}

func giniCoefficient(sortedAsc []float64) float64 {
	n := len(sortedAsc)
	if n == 0 {
		return 0
	}
	var sum, weighted float64
	for i, v := range sortedAsc {
		sum += v
		weighted += float64(i+1) * v
	}
	if sum == 0 {
		return 0
	}
	nn := float64(n)
	return (2*weighted)/(nn*sum) - (nn+1)/nn
}

func concentrationRatio(sortedDesc []float64, total float64, topN int) float64 {
	if total == 0 || topN <= 0 {
		return 0
	}
	if topN > len(sortedDesc) {
		topN = len(sortedDesc)
	}
	var top float64
	for i := 0; i < topN; i++ {
		top += sortedDesc[i]
	}
	return top / total
}

// tail80CostPct is the share of total billed cost contributed by the cheapest
// 80% of entities (by count, ascending cost).
func tail80CostPct(sortedAsc []float64, total float64) float64 {
	n := len(sortedAsc)
	if n == 0 || total == 0 {
		return 0
	}
	bottomCount := int(math.Floor(0.8 * float64(n)))
	if bottomCount <= 0 {
		return 0
	}
	var bottomSum float64
	for i := 0; i < bottomCount; i++ {
		bottomSum += sortedAsc[i]
	}
	return bottomSum / total * 100
}
