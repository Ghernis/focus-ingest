package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ghernis/focus_dt/internal/sqlserver"
)

func execSQLServerMultiInsert(ctx context.Context, tx *sql.Tx, prefixSQL string, colsPerRow int, rows [][]interface{}) error {
	if len(rows) == 0 {
		return nil
	}
	chunk := sqlserver.ChunkRows(colsPerRow)
	for start := 0; start < len(rows); start += chunk {
		end := start + chunk
		if end > len(rows) {
			end = len(rows)
		}
		if err := execSQLServerInsertChunk(ctx, tx, prefixSQL, colsPerRow, rows[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func execSQLServerInsertChunk(ctx context.Context, tx *sql.Tx, prefixSQL string, colsPerRow int, rows [][]interface{}) error {
	var b strings.Builder
	b.WriteString(prefixSQL)
	args := make([]interface{}, 0, len(rows)*colsPerRow)
	n := 1
	for i, row := range rows {
		if len(row) != colsPerRow {
			return fmt.Errorf("row %d: expected %d values, got %d", i, colsPerRow, len(row))
		}
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('(')
		for c := 0; c < colsPerRow; c++ {
			if c > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "@p%d", n)
			n++
		}
		b.WriteByte(')')
		args = append(args, row...)
	}
	if err := sqlserver.CheckParamCount(len(args)); err != nil {
		return fmt.Errorf("staging insert chunk (%d rows x %d cols): %w", len(rows), colsPerRow, err)
	}
	_, err := tx.ExecContext(ctx, b.String(), args...)
	return err
}
