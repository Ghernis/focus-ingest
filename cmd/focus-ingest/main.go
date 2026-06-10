package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/ghernis/focus_dt/internal/config"
	"github.com/ghernis/focus_dt/internal/focus"
	pqread "github.com/ghernis/focus_dt/internal/parquet"
	"github.com/ghernis/focus_dt/internal/store"
)

var (
	useLocal       bool
	useSQLite      bool
	sqlitePath     string
	connection     string
	focusVersion   string
	batchSize      int
	batchID        int64
	skipTags       bool
	skipAggregates bool
	forceImport    bool
	rebuildTags    bool
	rebuildAggs    bool
)

func main() {
	root := &cobra.Command{
		Use:   "focus-ingest",
		Short: "FOCUS cost data importer for SQL Server and local SQLite",
	}

	root.PersistentFlags().BoolVar(&useLocal, "local", false, "Use local SQLite database")
	root.PersistentFlags().BoolVar(&useSQLite, "sqlite", false, "Alias for --local")
	root.PersistentFlags().StringVar(&sqlitePath, "sqlite-path", config.DefaultSQLitePath, "SQLite database file path")
	root.PersistentFlags().StringVar(&connection, "connection", "", "SQL Server connection string (default: $FOCUS_DATABASE_URL)")
	root.PersistentFlags().StringVar(&focusVersion, "focus-version", "1.2", "FOCUS export version metadata")
	root.PersistentFlags().IntVar(&batchSize, "batch-size", 5000, "Parquet read / insert batch size")
	root.PersistentFlags().Int64Var(&batchID, "batch-id", 0, "Ingestion batch id (validate command)")
	root.PersistentFlags().BoolVar(&skipTags, "skip-tags", false, "Skip tag bridge during import (use rebuild tags after bulk load)")
	root.PersistentFlags().BoolVar(&skipAggregates, "skip-aggregates", false, "Skip rebuilding aggregate tables during import (use rebuild aggregates after bulk load)")
	root.PersistentFlags().BoolVar(&forceImport, "force", false, "Re-import a file even if it was already processed successfully")

	root.AddCommand(schemaCmd())
	root.AddCommand(importCmd())
	root.AddCommand(rebuildCmd())
	root.AddCommand(validateCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func loadConfig() (config.Config, error) {
	return config.FromFlags(useLocal || useSQLite, useSQLite, sqlitePath, connection, focusVersion, batchSize, batchID, skipTags, skipAggregates)
}

func openStore() (store.Store, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	return store.New(cfg)
}

func schemaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Database schema operations",
	}
	apply := &cobra.Command{
		Use:   "apply",
		Short: "Apply FOCUS warehouse DDL",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()
			if err := s.ApplySchema(cmd.Context()); err != nil {
				return err
			}
			fmt.Printf("Schema applied (%s)\n", s.Dialect())
			return nil
		},
	}
	cmd.AddCommand(apply)
	return cmd
}

func importCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import [parquet files...]",
		Short: "Import FOCUS parquet files into staging and run ETL",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			for _, path := range args {
				abs, _ := filepath.Abs(path)
				sourceFile := filepath.Base(path)
				if !forceImport {
					if prevID, found, err := s.FindCompletedImport(ctx, sourceFile, focusVersion); err != nil {
						return err
					} else if found {
						fmt.Printf("Skipping %s (already imported as batch %d; use --force to re-import)\n", abs, prevID)
						continue
					}
				} else if prevID, found, err := s.FindCompletedImport(ctx, sourceFile, focusVersion); err != nil {
					return err
				} else if found {
					fmt.Printf("Purging previous import of %s (batch %d)\n", sourceFile, prevID)
					if err := s.PurgeImport(ctx, prevID); err != nil {
						return err
					}
				}

				if n, err := s.PurgeStaleLoading(ctx, sourceFile, focusVersion); err != nil {
					return err
				} else if n > 0 {
					fmt.Printf("Purged %d incomplete LOADING batch(es) for %s\n", n, sourceFile)
				}

				meta := store.BatchMeta{
					SourceProvider: "MIXED",
					FocusVersion:   focusVersion,
					SourceFile:     sourceFile,
				}
				id, err := s.BeginBatch(ctx, meta)
				if err != nil {
					return err
				}
				fmt.Printf("Loading %s -> batch %d\n", abs, id)

				t0 := time.Now()
				total, err := pqread.ReadFile(path, batchSize, func(rows []focus.StagingRow) error {
					return s.InsertStaging(ctx, id, focusVersion, meta.SourceFile, rows)
				})
				if err != nil {
					return fmt.Errorf("%s: %w", path, err)
				}
				fmt.Printf("  staged %d rows in %s\n", total, time.Since(t0).Round(time.Millisecond))

				t1 := time.Now()
				if err := s.ProcessBatch(ctx, id, focusVersion); err != nil {
					_ = s.MarkBatchFailed(ctx, id)
					return fmt.Errorf("etl batch %d: %w", id, err)
				}
				fmt.Printf("  ETL complete for batch %d in %s\n", id, time.Since(t1).Round(time.Millisecond))

				rep, err := s.Validate(ctx, id)
				if err != nil {
					return err
				}
				store.PrintValidation(rep)
			}
			return nil
		},
	}
}

func rebuildCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rebuild",
		Short: "Rebuild aggregate tables and/or tag bridges after bulk import",
	}
	rebuildTags = true
	rebuildAggs = true
	cmd.PersistentFlags().BoolVar(&rebuildTags, "tags", true, "Rebuild tag bridges from staging")
	cmd.PersistentFlags().BoolVar(&rebuildAggs, "aggregates", true, "Rebuild aggregate tables from daily facts")

	run := func(label string, fn func() error) error {
		t0 := time.Now()
		if err := fn(); err != nil {
			return err
		}
		fmt.Printf("%s complete in %s\n", label, time.Since(t0).Round(time.Millisecond))
		return nil
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if !rebuildTags && !rebuildAggs {
			return fmt.Errorf("nothing to rebuild: enable --tags and/or --aggregates")
		}
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		// Tags before aggregates so Application/Environment tags feed app-level aggs.
		if rebuildTags {
			if err := run("Tag bridge rebuild", func() error { return s.RebuildTags(ctx) }); err != nil {
				return err
			}
		}
		if rebuildAggs {
			if err := run("Aggregate rebuild", func() error { return s.RebuildAggregates(ctx) }); err != nil {
				return err
			}
		}
		return nil
	}
	return cmd
}

func validateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate an ingestion batch",
		RunE: func(cmd *cobra.Command, args []string) error {
			if batchID <= 0 {
				return fmt.Errorf("--batch-id is required")
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()
			rep, err := s.Validate(cmd.Context(), batchID)
			if err != nil {
				return err
			}
			store.PrintValidation(rep)
			return nil
		},
	}
}
