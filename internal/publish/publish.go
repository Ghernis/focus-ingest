package publish

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// Options controls publish from local SQLite to SQL Server.
type Options struct {
	Connection    string
	SQLitePath    string
	BillingPeriod string // YYYY-MM-DD
	PublishFacts  bool
	SourceFile    string // optional; defaults to sqlite basename
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
	if month == "" {
		return fmt.Errorf("billing-period is required (YYYY-MM-DD)")
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

	fmt.Println("Publishing pending dimensions to SQL Server...")
	realign, err := publishPendingDims(ctx, local, server)
	if err != nil {
		return fmt.Errorf("dimensions: %w", err)
	}
	if len(realign) > 0 {
		fmt.Printf("  realigning %d dimension SK mapping(s) in local DB\n", countRealign(realign))
		if err := realignLocalSKs(ctx, local, realign); err != nil {
			return fmt.Errorf("realign: %w", err)
		}
	}

	fmt.Printf("Publishing aggregates for %s...\n", month)
	if err := publishAggregates(ctx, local, server, month); err != nil {
		return fmt.Errorf("aggregates: %w", err)
	}

	if opts.PublishFacts {
		batchID, err := ensurePublishBatch(ctx, server, month, opts.SourceFile)
		if err != nil {
			return fmt.Errorf("batch: %w", err)
		}
		fmt.Printf("Publishing facts for %s (batch %d)...\n", month, batchID)
		n, err := publishFacts(ctx, local, server, month, batchID)
		if err != nil {
			return fmt.Errorf("facts: %w", err)
		}
		fmt.Printf("  published %d fact rows\n", n)

		nb, err := publishBridge(ctx, local, server, month)
		if err != nil {
			return fmt.Errorf("bridge: %w", err)
		}
		fmt.Printf("  published %d bridge tag rows\n", nb)
	}

	if _, err := local.ExecContext(ctx, `DELETE FROM dim_sync_pending`); err != nil {
		return err
	}

	fmt.Println("Publish complete.")
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
