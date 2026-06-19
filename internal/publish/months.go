package publish

import (
	"context"
	"database/sql"
	"strings"

	"github.com/ghernis/focus_dt/internal/focus"
)

// DistinctBillingPeriods returns sorted distinct billing_period_start values from facts.
func DistinctBillingPeriods(ctx context.Context, db *sql.DB) ([]string, error) {
	return distinctBillingMonths(ctx, db)
}

func distinctBillingMonths(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT billing_period_start FROM fact_focus_cost_daily ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	seen := map[string]struct{}{}
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		m = focus.DateOnly(strings.TrimSpace(m))
		if m == "" {
			continue
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out, rows.Err()
}
