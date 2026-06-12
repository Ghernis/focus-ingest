package etl

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ghernis/focus_dt/internal/focus"
)

func (p *Processor) rawApplicationExpr() string {
	return `COALESCE(NULLIF(TRIM(res.application), ''), NULLIF(TRIM(app_tag.tag_value), ''), '(Unassigned)')`
}

func (p *Processor) normalizeApplicationSQL(expr string) string {
	if p.Dialect == "sqlserver" {
		return fmt.Sprintf("UPPER(REPLACE(REPLACE(LTRIM(RTRIM(%s)), '-', '_'), ' ', '_'))", expr)
	}
	return fmt.Sprintf("UPPER(REPLACE(REPLACE(TRIM(%s), '-', '_'), ' ', '_'))", expr)
}

// canonicalApplicationSQL mirrors focus.CanonicalApplicationName on a normalized SQL expression.
func (p *Processor) canonicalApplicationSQL(normExpr string) string {
	if p.Dialect == "sqlserver" {
		return fmt.Sprintf(`CASE
		  WHEN %s = '(UNASSIGNED)' THEN '(UNASSIGNED)'
		  WHEN LEN(%s) > 1 AND RIGHT(%s, 1) = 'S' AND (
		    CHARINDEX('_', %s) = 0 OR
		    LEN(%s) - CHARINDEX('_', REVERSE(%s)) >= 1
		  ) THEN LEFT(%s, LEN(%s) - 1)
		  ELSE %s END`, normExpr, normExpr, normExpr, normExpr, normExpr, normExpr, normExpr, normExpr, normExpr)
	}
	return fmt.Sprintf(`CASE
	  WHEN %s = '(UNASSIGNED)' THEN '(UNASSIGNED)'
	  WHEN length(%s) > 1 AND substr(%s, -1) = 'S' THEN substr(%s, 1, length(%s) - 1)
	  ELSE %s END`, normExpr, normExpr, normExpr, normExpr, normExpr, normExpr)
}

func (p *Processor) applicationDimJoin() string {
	raw := p.rawApplicationExpr()
	norm := p.normalizeApplicationSQL(raw)
	canon := p.canonicalApplicationSQL(norm)
	return fmt.Sprintf("INNER JOIN dim_application da ON da.application_name = %s", canon)
}

func (p *Processor) ensureDefaultApplication(ctx context.Context, tx *sql.Tx, cache applicationAliasCache) error {
	return p.upsertApplicationAlias(ctx, tx, focus.UnassignedApplication, "", cache)
}

type applicationAliasCache map[string]string

func (p *Processor) loadApplicationAliasCache(ctx context.Context, tx *sql.Tx) (applicationAliasCache, error) {
	cache := applicationAliasCache{}
	rows, err := tx.QueryContext(ctx, p.q(`SELECT application_name, COALESCE(alias_values, '') FROM dim_application`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name, aliases string
		if err := rows.Scan(&name, &aliases); err != nil {
			return nil, err
		}
		cache[name] = aliases
	}
	return cache, rows.Err()
}

func (p *Processor) upsertApplicationAlias(ctx context.Context, tx *sql.Tx, raw, firstSeen string, cache applicationAliasCache) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = focus.UnassignedApplication
	}
	canon := focus.ResolveApplicationName(raw)
	firstSeen = focus.DateOnly(strings.TrimSpace(firstSeen))

	var existingAliases string
	var known bool
	if cache != nil {
		existingAliases, known = cache[canon]
	}
	if !known {
		var scanned sql.NullString
		err := tx.QueryRowContext(ctx, p.q(`
			SELECT alias_values FROM dim_application WHERE application_name = ?`), canon).Scan(&scanned)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			existingAliases = scanned.String
			known = true
			if cache != nil {
				cache[canon] = existingAliases
			}
		}
	}

	if known {
		merged := focus.MergeAliasValues(existingAliases, raw)
		normExisting := strings.TrimSpace(strings.ReplaceAll(existingAliases, ",", "|"))
		if merged == normExisting {
			return nil
		}
		var err error
		if p.Dialect == "sqlite" {
			_, err = tx.ExecContext(ctx, `
				UPDATE dim_application SET alias_values = ?, updated_utc = datetime('now')
				WHERE application_name = ?`, nullIfEmptyAlias(merged), canon)
		} else {
			_, err = tx.ExecContext(ctx, `
				UPDATE dim_application SET alias_values = @p1, updated_utc = SYSUTCDATETIME()
				WHERE application_name = @p2`, nullIfEmptyAlias(merged), canon)
		}
		if err == nil && cache != nil {
			cache[canon] = merged
		}
		return err
	}

	aliases := raw
	var err error
	if p.Dialect == "sqlite" {
		if firstSeen == "" {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO dim_application (application_name, alias_values, first_seen_date, created_utc, updated_utc)
				VALUES (?, ?, date('now'), datetime('now'), datetime('now'))`,
				canon, nullIfEmptyAlias(aliases))
		} else {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO dim_application (application_name, alias_values, first_seen_date, created_utc, updated_utc)
				VALUES (?, ?, ?, datetime('now'), datetime('now'))`,
				canon, nullIfEmptyAlias(aliases), firstSeen)
		}
	} else if firstSeen == "" {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO dim_application (application_name, alias_values, first_seen_date, created_utc, updated_utc)
			VALUES (@p1, @p2, CAST(SYSUTCDATETIME() AS DATE), SYSUTCDATETIME(), SYSUTCDATETIME())`,
			canon, nullIfEmptyAlias(aliases))
	} else {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO dim_application (application_name, alias_values, first_seen_date, created_utc, updated_utc)
			VALUES (@p1, @p2, @p3, SYSUTCDATETIME(), SYSUTCDATETIME())`,
			canon, nullIfEmptyAlias(aliases), firstSeen)
	}
	if err == nil && cache != nil {
		if s, ok := nullIfEmptyAlias(aliases).(string); ok {
			cache[canon] = s
		} else {
			cache[canon] = ""
		}
	}
	return err
}

func nullIfEmptyAlias(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func (p *Processor) syncApplicationsFromRows(ctx context.Context, tx *sql.Tx, rows []normRow) error {
	cache, err := p.loadApplicationAliasCache(ctx, tx)
	if err != nil {
		return err
	}
	if err := p.ensureDefaultApplication(ctx, tx, cache); err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, r := range rows {
		raw := strings.TrimSpace(tagFromJSON(r.RawTagsJSON, "Application"))
		if raw == "" {
			continue
		}
		key := raw + "|" + r.ChargeDate
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if err := p.upsertApplicationAlias(ctx, tx, raw, r.ChargeDate, cache); err != nil {
			return err
		}
	}
	return nil
}

func (p *Processor) syncApplicationsFromFactsForMonth(ctx context.Context, tx *sql.Tx, month string) error {
	joins := p.appContextJoins()
	q := fmt.Sprintf(`
		SELECT %s AS raw_application, MIN(f.charge_date) AS first_seen_date
		FROM fact_focus_cost_daily f
		%s
		WHERE %s
		GROUP BY %s`, p.rawApplicationExpr(), joins, monthEq("f.billing_period_start", month), p.rawApplicationExpr())
	return p.syncApplicationsFromQuery(ctx, tx, q)
}

func (p *Processor) syncApplicationsFromFacts(ctx context.Context, tx *sql.Tx) error {
	joins := p.appContextJoins()
	q := fmt.Sprintf(`
		SELECT %s AS raw_application, MIN(f.charge_date) AS first_seen_date
		FROM fact_focus_cost_daily f
		%s
		GROUP BY %s`, p.rawApplicationExpr(), joins, p.rawApplicationExpr())
	return p.syncApplicationsFromQuery(ctx, tx, q)
}

func (p *Processor) syncApplicationsFromQuery(ctx context.Context, tx *sql.Tx, q string) error {
	cache, err := p.loadApplicationAliasCache(ctx, tx)
	if err != nil {
		return err
	}
	if err := p.ensureDefaultApplication(ctx, tx, cache); err != nil {
		return err
	}

	rows, err := tx.QueryContext(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var raw, firstSeen string
		if err := rows.Scan(&raw, &firstSeen); err != nil {
			return err
		}
		if err := p.upsertApplicationAlias(ctx, tx, raw, firstSeen, cache); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	resQ := `SELECT application, MIN(valid_from) AS first_seen_date
		FROM dim_resource
		WHERE valid_to IS NULL AND NULLIF(TRIM(application), '') IS NOT NULL
		GROUP BY application`
	if p.Dialect == "sqlite" {
		resQ = `SELECT application, MIN(valid_from) AS first_seen_date
			FROM dim_resource
			WHERE is_current = 1 AND NULLIF(TRIM(application), '') IS NOT NULL
			GROUP BY application`
	}
	rows, err = tx.QueryContext(ctx, resQ)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw, firstSeen string
		if err := rows.Scan(&raw, &firstSeen); err != nil {
			return err
		}
		if err := p.upsertApplicationAlias(ctx, tx, raw, firstSeen, cache); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (p *Processor) lookupApplicationSK(ctx context.Context, tx *sql.Tx, raw string) (*int64, error) {
	canon := focus.ResolveApplicationName(raw)
	var sk int64
	err := tx.QueryRowContext(ctx, p.q(`SELECT application_sk FROM dim_application WHERE application_name = ?`), canon).Scan(&sk)
	if err == sql.ErrNoRows {
		if err := p.upsertApplicationAlias(ctx, tx, raw, "", nil); err != nil {
			return nil, err
		}
		err = tx.QueryRowContext(ctx, p.q(`SELECT application_sk FROM dim_application WHERE application_name = ?`), canon).Scan(&sk)
	}
	if err != nil {
		return nil, err
	}
	return &sk, nil
}
