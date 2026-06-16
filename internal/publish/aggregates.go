package publish

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ghernis/focus_dt/internal/etl"
)

type aggCopySpec struct {
	table       string
	localWhere  string
	serverCols  string
	localCols   string
	colCount    int
}

func publishAggregates(ctx context.Context, local, server *sql.DB, month string, maps skMaps) error {
	tx, err := server.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	proc := &etl.Processor{DB: server, Dialect: "sqlserver"}
	if err := proc.DeleteAggregatesForMonth(ctx, tx, month, true); err != nil {
		return fmt.Errorf("delete server aggs: %w", err)
	}

	for _, spec := range aggSpecs(month) {
		n, err := copyAggTableRemapped(ctx, local, tx, spec, maps)
		if err != nil {
			return err
		}
		fmt.Printf("  published %d rows to %s\n", n, spec.table)
	}

	return tx.Commit()
}
