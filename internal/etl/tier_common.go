package etl

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/ghernis/focus_dt/internal/focus"
)

const (
	changeDownsize = "DOWNSIZE"
	changeUpsize   = "UPSIZE"
	changeNeutral  = "NEUTRAL"
	changeScopeMoM = "MOM"
	changeScopeIntra = "INTRAMONTH"
)

type resourceServiceKey struct {
	resourceSK int64
	serviceSK  int64
}

type resourceMonthMeta struct {
	provider      string
	serviceSK     int64
	applicationSK int64
	environment   string
}

type tierDailyRow struct {
	chargeDate    string
	billingMonth  string
	provider      string
	resourceSK    int64
	serviceSK     int64
	applicationSK int64
	environment   string
	tierCode      string
	tierRank      int
	tierSkuSK     int64
	tierUnitRate  float64
	tierCost      float64
	tierQty       float64
}

type serviceTierRollup struct {
	unitSavings  float64
	costDelta    float64
	momCount     int
	intraCount   int
	downsizeCnt  int
	upsizeCnt    int
}

func priorBillingMonth(month string) string {
	t, err := time.Parse("2006-01-02", focus.DateOnly(month))
	if err != nil {
		return ""
	}
	return t.AddDate(0, -1, 0).Format("2006-01-02")
}

func unitRate(cost, qty float64) float64 {
	if qty <= 0 || math.IsNaN(qty) {
		if cost <= 0 {
			return 0
		}
		return cost
	}
	return cost / qty
}

func changeDirection(priorRate, currentRate float64) string {
	const eps = 1e-9
	switch {
	case priorRate-currentRate > eps:
		return changeDownsize
	case currentRate-priorRate > eps:
		return changeUpsize
	default:
		return changeNeutral
	}
}

func parseDecimal(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

func rollupKey(provider string, serviceSK int64) string {
	return provider + "|" + strconv.FormatInt(serviceSK, 10)
}

func addTierRollup(rollups map[string]*serviceTierRollup, provider string, serviceSK int64, unitSavings, costDelta float64, dir string, mom, intra bool) {
	key := rollupKey(provider, serviceSK)
	if rollups[key] == nil {
		rollups[key] = &serviceTierRollup{}
	}
	r := rollups[key]
	r.unitSavings += unitSavings
	r.costDelta += costDelta
	if mom {
		r.momCount++
	}
	if intra {
		r.intraCount++
	}
	switch dir {
	case changeDownsize:
		r.downsizeCnt++
	case changeUpsize:
		r.upsizeCnt++
	}
}
