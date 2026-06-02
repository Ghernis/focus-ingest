package store

import (
	"context"
	"fmt"

	"github.com/ghernis/focus_dt/internal/config"
)

func New(cfg config.Config) (Store, error) {
	if cfg.Local {
		return OpenSQLite(cfg.SQLitePath, cfg.SkipTags, cfg.SkipAggregates)
	}
	return OpenSQLServer(cfg.Connection, cfg.SkipTags, cfg.SkipAggregates)
}

func MustNew(cfg config.Config) Store {
	s, err := New(cfg)
	if err != nil {
		panic(err)
	}
	return s
}

func PrintValidation(rep ValidationReport) {
	fmt.Printf("Batch %d status: %s\n", rep.BatchID, rep.Status)
	fmt.Printf("  staging rows:     %d\n", rep.StagingRows)
	fmt.Printf("  daily fact rows:  %d\n", rep.DailyFactRows)
	fmt.Printf("  bridge tag rows:  %d\n", rep.BridgeTagRows)
	fmt.Printf("  commitment dims:  %d\n", rep.CommitmentDimRows)
	fmt.Printf("  agg monthly:      %d\n", rep.AggMonthlyRows)
	fmt.Printf("  agg commitment:   %d\n", rep.AggCommitmentRows)
	for _, ps := range rep.ProviderSpend {
		fmt.Printf("  provider %s effective_cost=%s lines=%d\n", ps.Provider, ps.TotalEffective, ps.SourceLines)
	}
	for status, cnt := range rep.CommitmentByStatus {
		fmt.Printf("  commitment status %s: %d agg rows\n", status, cnt)
	}
}

var _ = context.Background
