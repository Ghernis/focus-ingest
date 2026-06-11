package sqlserver

import "fmt"

// MaxParams is the target per-statement parameter budget (below the server hard limit).
const MaxParams = 2000

// HardLimit is the maximum parameters SQL Server accepts per request.
const HardLimit = 2100

// ChunkRows returns the maximum number of rows per multi-value INSERT for colsPerRow columns.
func ChunkRows(colsPerRow int) int {
	if colsPerRow <= 0 {
		return 1
	}
	n := MaxParams / colsPerRow
	if n < 1 {
		return 1
	}
	return n
}

// CheckParamCount returns an error when n exceeds HardLimit.
func CheckParamCount(n int) error {
	if n > HardLimit {
		return errTooManyParams{n: n}
	}
	return nil
}

type errTooManyParams struct{ n int }

func (e errTooManyParams) Error() string {
	return fmt.Sprintf("sql server statement uses %d parameters (limit %d)", e.n, HardLimit)
}
