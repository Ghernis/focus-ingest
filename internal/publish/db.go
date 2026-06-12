package publish

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ghernis/focus_dt/internal/schema"
	"github.com/ghernis/focus_dt/internal/store"

	_ "github.com/microsoft/go-mssqldb"
	_ "modernc.org/sqlite"
)

func openSQLServer(ctx context.Context, connection string) (*sql.DB, error) {
	db, err := sql.Open("sqlserver", connection)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("sql server ping: %w", err)
	}
	db.SetMaxOpenConns(10)
	return db, nil
}

func openSQLite(ctx context.Context, path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func applySQLiteSchema(ctx context.Context, db *sql.DB) error {
	return execSQLScript(ctx, db, schema.SQLiteDDL)
}

func execSQLScript(ctx context.Context, db *sql.DB, script string) error {
	for _, stmt := range store.SplitSQL(script) {
		if stmt == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("schema: %w", err)
		}
	}
	return nil
}
