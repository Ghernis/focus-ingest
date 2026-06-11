package config

import (
	"fmt"
	"os"
	"path/filepath"
)

var DefaultSQLitePath = func() string {
	// Avoid putting SQLite DBs under OneDrive-synced folders by default.
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "focus", "focus_local.db")
	}
	return "data/focus_local.db"
}()

type Config struct {
	Local        bool
	SQLitePath   string
	Connection   string
	FocusVersion string
	BatchSize    int
	BatchID      int64
	SkipTags       bool
	SkipAggregates bool
	UseGoETL       bool // SQL Server only: row-by-row Go ETL instead of set-based SQL
}

func FromFlags(local, sqlite bool, sqlitePath, connection, focusVersion string, batchSize int, batchID int64, skipTags, skipAggregates, useGoETL bool) (Config, error) {
	cfg := Config{
		Local:          local || sqlite,
		SQLitePath:     sqlitePath,
		Connection:     connection,
		FocusVersion:   focusVersion,
		BatchSize:      batchSize,
		BatchID:        batchID,
		SkipTags:       skipTags,
		SkipAggregates: skipAggregates,
		UseGoETL:       useGoETL,
	}
	if cfg.SQLitePath == "" {
		cfg.SQLitePath = DefaultSQLitePath
	}
	if cfg.FocusVersion == "" {
		cfg.FocusVersion = "1.2"
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 5000
	}
	if !cfg.Local && cfg.Connection == "" {
		cfg.Connection = os.Getenv("FOCUS_DATABASE_URL")
	}
	if !cfg.Local && cfg.Connection == "" {
		return cfg, fmt.Errorf("SQL Server connection required: use --connection or set FOCUS_DATABASE_URL (or pass --local for SQLite)")
	}
	return cfg, nil
}
