// Command ci_create_mssql_db creates the disposable focus_e2e database used by
// GitHub Actions SQL Server E2E. Connects to master using MSSQL_SA_PASSWORD
// (default matches .github/workflows/ci.yml).
//
// The database is created with the instance collation so it matches tempdb
// (avoids BIN2 vs CI_AS conflicts when joining #temp expression columns).
package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
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

		var collation string
		if err := db.QueryRowContext(ctx, `SELECT CONVERT(nvarchar(128), SERVERPROPERTY('Collation'))`).Scan(&collation); err != nil {
			fail(err)
		}
		collation = strings.TrimSpace(collation)
		if collation == "" {
			fail(fmt.Errorf("empty server collation"))
		}
		// Identifiers only; collation names are alphanumeric + underscore.
		for _, r := range collation {
			if !(r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
				fail(fmt.Errorf("unexpected collation name %q", collation))
			}
		}

		createSQL := fmt.Sprintf(`IF DB_ID('focus_e2e') IS NULL CREATE DATABASE focus_e2e COLLATE %s;`, collation)
		if _, err := db.ExecContext(ctx, createSQL); err != nil {
			fail(err)
		}
		fmt.Printf("database focus_e2e ready (collation=%s)\n", collation)
		return
	}
	fail(fmt.Errorf("sql server not ready: %w", last))
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "ci_create_mssql_db: %v\n", err)
	os.Exit(1)
}
