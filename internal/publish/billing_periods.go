package publish

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ghernis/focus_dt/internal/focus"
)

// BillingPeriodsReport describes billing periods found in a local SQLite database.
type BillingPeriodsReport struct {
	SQLitePath     string   `json:"sqlite_path"`
	BillingPeriods []string `json:"billing_periods"`
	PrimaryPeriod  string   `json:"primary_period"`
	PreviousPeriod string   `json:"previous_period,omitempty"`
	PublishPeriods []string `json:"publish_periods"`
	HasOverlap     bool     `json:"has_overlap"`
}

// ListBillingPeriods reads distinct billing_period_start values from fact_focus_cost_daily
// and derives publish_periods (all periods except the calendar month before primary, when present).
func ListBillingPeriods(ctx context.Context, sqlitePath string) (BillingPeriodsReport, error) {
	if sqlitePath == "" {
		return BillingPeriodsReport{}, fmt.Errorf("sqlite path required")
	}
	if _, err := os.Stat(sqlitePath); err != nil {
		return BillingPeriodsReport{}, fmt.Errorf("sqlite database: %w", err)
	}

	db, err := openSQLiteExisting(ctx, sqlitePath)
	if err != nil {
		return BillingPeriodsReport{}, err
	}
	defer db.Close()

	periods, err := DistinctBillingPeriods(ctx, db)
	if err != nil {
		return BillingPeriodsReport{}, err
	}

	primary, previous, publish, overlap := computePublishPeriods(periods)
	return BillingPeriodsReport{
		SQLitePath:     sqlitePath,
		BillingPeriods: periods,
		PrimaryPeriod:  primary,
		PreviousPeriod: previous,
		PublishPeriods: publish,
		HasOverlap:     overlap,
	}, nil
}

func computePublishPeriods(periods []string) (primary, previous string, publish []string, overlap bool) {
	if len(periods) == 0 {
		return "", "", nil, false
	}
	primary = periods[len(periods)-1]
	previous = previousBillingPeriodStart(primary)
	publish = append([]string(nil), periods...)
	if previous != "" {
		for i, p := range publish {
			if p == previous {
				publish = append(publish[:i], publish[i+1:]...)
				overlap = true
				break
			}
		}
	}
	return primary, previous, publish, overlap
}

const billingPeriodDateLayout = "2006-01-02"

func previousBillingPeriodStart(period string) string {
	t, err := time.Parse(billingPeriodDateLayout, focus.DateOnly(period))
	if err != nil {
		return ""
	}
	return t.AddDate(0, -1, 0).Format(billingPeriodDateLayout)
}
