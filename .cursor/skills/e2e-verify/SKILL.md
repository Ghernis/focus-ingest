---
name: e2e-verify
description: >-
  Run end-to-end verification after major refactors, design changes, or new features.
  Asks for or uses documented fixture/dump files, runs the full pipeline, and reports
  pass/fail with SQL or CLI evidence. Use when the user finishes a large change, asks for
  E2E testing, integration validation, smoke test, or "does it work end-to-end".
---

# E2E Verify

Run a real pipeline check before treating a major change as done. Unit tests alone are not enough.

## When to run

Run this skill when **any** of these apply:

- Major refactor or architecture/design change
- New feature touching ingest, ETL, aggregates, publish, or schema
- User explicitly asks for E2E / integration / smoke validation
- Bug fix where the failure only appeared in full pipeline (not in isolated unit tests)

Skip for typo-only edits, comment-only changes, or narrow unit-test updates with no behavior change.

## Workflow

Copy and track:

```
E2E progress:
- [ ] 1. Scope the change and define success criteria
- [ ] 2. Resolve fixtures (ask user or read fixtures.md)
- [ ] 3. Run unit + integration tests
- [ ] 4. Run pipeline E2E (local SQLite preferred)
- [ ] 5. Run sanity SQL / assertions
- [ ] 6. Report results
```

### Step 1 — Scope and success criteria

Before running anything, write down:

1. **What changed** (1–3 bullets)
2. **User-visible outcome** (what should be true after ingest + rebuild)
3. **Minimum assertions** (counts, sample rows, error-free CLI exit)

If criteria are vague, ask the user to confirm before proceeding.

### Step 2 — Resolve fixtures

**First:** read [fixtures.md](fixtures.md) for project-known files and how to obtain them.

**If fixtures are missing or insufficient**, ask the user using this template (do not guess paths or invent data):

```markdown
To run E2E I need fixture data. Please provide **one** of:

1. **Staging sample** — CSV or parquet slice covering the scenario (e.g. one VM month, one billing month)
2. **DB dump** — SQLite file or SQL export of `stg_focus_cost_line` + relevant dims for one batch/month
3. **Reference catalog** — e.g. `ms_skus.json` when SKU/tier rules are involved

Also confirm:
- Billing month to assert (e.g. `2026-01-01`)
- Expected row counts or example resource/SKU IDs
- Run target: `--local` SQLite (fast, recommended) or Azure SQL connection
```

If the user cannot share files, document in `fixtures.md` (or a test comment) **how to reproduce** the fixture and proceed with synthetic minimal rows only when the scenario is simple (see existing `tier_integration_test.go` helpers).

**Never commit secrets, full production exports, or `.env` credentials.**

### Step 3 — Automated tests

Always run targeted tests first, then the package:

```bash
go test ./internal/... -count=1
```

For a focused area, run the relevant package first (e.g. `./internal/etl/...`). Fix failures before CLI E2E.

Add or extend integration tests when the scenario is repeatable — prefer `t.TempDir()` SQLite DBs over checked-in binary dumps.

### Step 4 — Pipeline E2E (CLI)

**Default: local SQLite** (seconds, not hours):

```bash
go build -o bin/focus-ingest.exe ./cmd/focus-ingest

# fresh DB
focus-ingest --local schema apply

# ingest fixture (parquet or staging path your project supports)
focus-ingest --local ingest <fixture>

# full pipeline refresh
focus-ingest --local rebuild-aggregates --full
```

Use Azure SQL only when explicitly testing SQL Server dialect behavior. Warn the user that `--full` on Azure can take a long time.

### Step 5 — Sanity assertions

Run SQL against the same DB used in step 4. Adapt queries to the feature; examples for tier/aggregate work:

```sql
SELECT COUNT(*) FROM dim_sku WHERE is_tier_meter = 1;
SELECT COUNT(*) FROM fact_resource_tier_daily;
SELECT tier_code, COUNT(*) FROM fact_resource_tier_daily GROUP BY tier_code;
```

Assert **non-zero only when the fixture should produce data**. A passing E2E with 0 rows when rows are expected is a failure.

### Step 6 — Report

Use the report template in [report-template.md](report-template.md). Include:

- Commands run (copy-pasteable)
- Test pass/fail summary
- Key SQL results or row samples
- Blockers (missing fixtures, skipped Azure run, etc.)

## Rules

- **Execute** tests and CLI yourself; do not only suggest commands.
- Prefer **local SQLite** for E2E unless SQL Server behavior is the subject.
- Ask for fixtures when real billing shape matters; use minimal synthetic rows only for well-understood cases.
- Update `fixtures.md` when a new recurring fixture is introduced.
- Do not mark the change "verified" if E2E was skipped without user acknowledgment.

## Project references

- Integration test patterns: `internal/etl/tier_integration_test.go`, `internal/store/sqlite_rebuild_test.go`
- Fixture inventory: [fixtures.md](fixtures.md)
- Report format: [report-template.md](report-template.md)
