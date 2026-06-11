package store

import (
	"testing"

	"github.com/ghernis/focus_dt/internal/focus"
)

func TestStagingRowArgsMatchesInsertCols(t *testing.T) {
	args := stagingRowArgs(1, "1.2", "test.parquet", focus.StagingRow{})
	if len(args) != stgInsertCols {
		t.Fatalf("stagingRowArgs returns %d values, stgInsertCols = %d", len(args), stgInsertCols)
	}
}
