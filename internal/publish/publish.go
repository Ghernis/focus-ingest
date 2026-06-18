package publish

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ghernis/focus_dt/internal/etl"
	"github.com/ghernis/focus_dt/internal/focus"
)

// Options controls publish from local SQLite to SQL Server.
type Options struct {
	Connection         string
	SQLitePath         string
	BillingPeriod      string // YYYY-MM-DD; optional when AllBillingPeriods is true
	AllBillingPeriods  bool
	ForcePeriods       bool // replace all local periods even when server has more complete data
	PublishFacts       bool
	SourceFile         string // optional; defaults to sqlite basename
}

// Publish uploads new dimensions, month aggregates, and optionally facts to SQL Server.
func Publish(ctx context.Context, opts Options) error {
	if opts.Connection == "" {
		return fmt.Errorf("SQL Server connection required")
	}
	if opts.SQLitePath == "" {
		return fmt.Errorf("sqlite path required")
	}
	month := strings.TrimSpace(opts.BillingPeriod)
	if month == "" && !opts.AllBillingPeriods {
		return fmt.Errorf("billing-period is required (YYYY-MM-DD) unless --all-billing-periods is set")
	}
	if opts.SourceFile == "" {
		opts.SourceFile = "publish:" + filepath.Base(opts.SQLitePath)
	}

	server, err := openSQLServer(ctx, opts.Connection)
	if err != nil {
		return err
	}
	defer server.Close()

	local, err := openSQLite(ctx, opts.SQLitePath)
	if err != nil {
		return err
	}
	defer local.Close()

	seeded, err := DimsSeeded(ctx, opts.SQLitePath)
	if err != nil {
		return err
	}
	if !seeded {
		return fmt.Errorf("local database is not seeded — run sync-dims --connection <azure> --sqlite-path <db> --fresh (or schema apply --local) before publish")
	}

	months, err := resolvePublishMonths(ctx, local, month, opts.AllBillingPeriods)
	if err != nil {
		return err
	}
	if len(months) == 0 {
		return fmt.Errorf("no billing periods found in local database")
	}

	plans, err := planPublishMonths(ctx, local, server, months, opts.ForcePeriods)
	if err != nil {
		return err
	}

	fmt.Println("Publishing pending dimensions to SQL Server...")
	skMap, err := publishPendingDims(ctx, local, server)
	if err != nil {
		return fmt.Errorf("dimensions: %w", err)
	}
	if n := countRealign(skMap); n > 0 {
		fmt.Printf("  %d local SK(s) differ from server — remapping at publish time (no local DB rewrite)\n", n)
	}

	publishedMonths := make([]string, 0, len(plans))
	for _, plan := range plans {
		publishedMonths = append(publishedMonths, plan.month)
		switch plan.mode {
		case publishMerge:
			fmt.Printf("Publishing aggregates (merge overlap) for %s...\n", plan.month)
			if err := publishAggregatesMerge(ctx, local, server, plan.month, skMap); err != nil {
				return fmt.Errorf("aggregates merge %s: %w", plan.month, err)
			}
		default:
			fmt.Printf("Publishing aggregates for %s...\n", plan.month)
			if err := publishAggregates(ctx, local, server, plan.month, skMap); err != nil {
				return fmt.Errorf("aggregates %s: %w", plan.month, err)
			}
		}

		if opts.PublishFacts {
			if plan.mode == publishMerge {
				fmt.Printf("  skipping facts for %s: overlap merge keeps prior-month facts unchanged (adjustments are in aggregates)\n", plan.month)
				continue
			}
			batchID, err := ensurePublishBatch(ctx, server, plan.month, opts.SourceFile)
			if err != nil {
				return fmt.Errorf("batch %s: %w", plan.month, err)
			}
			fmt.Printf("Publishing facts for %s (batch %d)...\n", plan.month, batchID)
			n, err := publishFacts(ctx, local, server, plan.month, batchID, skMap)
			if err != nil {
				return fmt.Errorf("facts %s: %w", plan.month, err)
			}
			fmt.Printf("  published %d fact rows\n", n)

			nb, err := publishBridge(ctx, local, server, plan.month, skMap)
			if err != nil {
				return fmt.Errorf("bridge %s: %w", plan.month, err)
			}
			fmt.Printf("  published %d bridge tag rows\n", nb)
		}
	}

	fmt.Println("Rebuilding cost anomalies on SQL Server...")
	if err := rebuildServerAnomalies(ctx, server, publishedMonths); err != nil {
		return fmt.Errorf("anomalies: %w", err)
	}

	if _, err := local.ExecContext(ctx, `DELETE FROM dim_sync_pending`); err != nil {
		return err
	}

	fmt.Println("Publish complete.")
	return nil
}

func resolvePublishMonths(ctx context.Context, local *sql.DB, single string, all bool) ([]string, error) {
	if !all {
		return []string{focus.DateOnly(strings.TrimSpace(single))}, nil
	}
	return distinctBillingMonths(ctx, local)
}

func rebuildServerAnomalies(ctx context.Context, server *sql.DB, months []string) error {
	proc := &etl.Processor{DB: server, Dialect: "sqlserver"}
	for _, m := range months {
		tx, err := server.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := proc.RebuildCostAnomaliesForMonth(ctx, tx, m); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("%s: %w", m, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func countRealign(m map[string]map[int64]int64) int {
	n := 0
	for _, inner := range m {
		n += len(inner)
	}
	return n
}

// AutoSyncDimsIfEmpty runs sync-dims when local SQLite has no dimension rows.
func AutoSyncDimsIfEmpty(ctx context.Context, connection, sqlitePath string) error {
	seeded, err := DimsSeeded(ctx, sqlitePath)
	if err != nil {
		return err
	}
	if seeded {
		return nil
	}
	if connection == "" {
		return fmt.Errorf("local database not seeded: run sync-dims --connection <azure> --sqlite-path <db> --fresh first, or pass --connection (or FOCUS_DATABASE_URL) to auto sync-dims on import")
	}
	fmt.Println("Local database not seeded — running sync-dims from SQL Server...")
	return SyncDims(ctx, SyncDimsOptions{Connection: connection, SQLitePath: sqlitePath, Fresh: true})
}
