// Command ci_create_mssql_db creates the disposable focus_e2e database used by
// GitHub Actions SQL Server E2E. Connects to master using MSSQL_SA_PASSWORD
// (default matches .github/workflows/ci.yml).
//
// Uses the instance default collation — no hard-coded collation. Set-based ETL
// is collation-safe via COLLATE DATABASE_DEFAULT on joins.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"time"

	_ "github.com/microsoft/go-mssqldb"
)

func main() {
	password := os.Getenv("MSSQL_SA_PASSWORD")
	if password == "" {
		password = "Your_password123"
	}
	host := os.Getenv("MSSQL_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("MSSQL_PORT")
	if port == "" {
		port = "1433"
	}

	u := &url.URL{
		Scheme: "sqlserver",
		User:   url.UserPassword("sa", password),
		Host:   fmt.Sprintf("%s:%s", host, port),
	}
	q := u.Query()
	q.Set("database", "master")
	q.Set("encrypt", "disable")
	q.Set("TrustServerCertificate", "true")
	u.RawQuery = q.Encode()

	db, err := sql.Open("sqlserver", u.String())
	if err != nil {
		fail(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var last error
	for i := 0; i < 30; i++ {
		if err := db.PingContext(ctx); err != nil {
			last = err
			time.Sleep(2 * time.Second)
			continue
		}
		if _, err := db.ExecContext(ctx, `IF DB_ID('focus_e2e') IS NULL CREATE DATABASE focus_e2e;`); err != nil {
			fail(err)
		}
		fmt.Println("database focus_e2e ready")
		return
	}
	fail(fmt.Errorf("sql server not ready: %w", last))
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "ci_create_mssql_db: %v\n", err)
	os.Exit(1)
}
