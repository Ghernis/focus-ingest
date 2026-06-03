package store

import (
	"context"

	"github.com/ghernis/focus_dt/internal/focus"
)

type BatchMeta struct {
	SourceProvider string
	FocusVersion   string
	SourceFile     string
}

type ProviderSpend struct {
	Provider         string
	TotalEffective   string
	SourceLines      int64
}

type ValidationReport struct {
	BatchID              int64
	Status               string
	StagingRows          int64
	DailyFactRows        int64
	BridgeTagRows        int64
	CommitmentDimRows    int64
	AggMonthlyRows       int64
	AggCommitmentRows    int64
	ProviderSpend        []ProviderSpend
	CommitmentByStatus   map[string]int64
}

type Store interface {
	ApplySchema(ctx context.Context) error
	BeginBatch(ctx context.Context, meta BatchMeta) (int64, error)
	InsertStaging(ctx context.Context, batchID int64, focusVersion, sourceFile string, rows []focus.StagingRow) error
	ProcessBatch(ctx context.Context, batchID int64, focusVersion string) error
	Validate(ctx context.Context, batchID int64) (ValidationReport, error)
	FindCompletedImport(ctx context.Context, sourceFile, focusVersion string) (batchID int64, found bool, err error)
	PurgeImport(ctx context.Context, batchID int64) error
	RebuildAggregates(ctx context.Context) error
	RebuildTags(ctx context.Context) error
	Close() error
	Dialect() string
}
