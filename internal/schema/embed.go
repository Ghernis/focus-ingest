package schema

import _ "embed"

//go:embed focus_dw_sqlite.sql
var SQLiteDDL string

//go:embed focus_dw.sql
var SQLServerDDL string
