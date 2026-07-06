# E2E verification report

## Summary

**Change:** [one-line description]  
**Result:** PASS | FAIL | PARTIAL (explain)  
**Run target:** local SQLite | Azure SQL  
**Date:** [ISO date]

## Success criteria

1. [criterion]
2. [criterion]

## Fixtures used

| File | Source |
|------|--------|
| [name] | [user-provided / fixtures.md / synthetic] |

If fixtures were missing, note what was requested and what was substituted.

## Commands run

```bash
# paste exact commands
```

## Test results

| Suite | Result | Notes |
|-------|--------|-------|
| `go test ./internal/etl/...` | pass/fail | |
| CLI ingest + rebuild | pass/fail | |

## Assertions

| Check | Expected | Actual |
|-------|----------|--------|
| [table/query] | [value] | [value] |

```sql
-- paste key queries
```

## Issues / follow-ups

- [blocker or debt]

## Sign-off

- [ ] User acknowledged skipped steps (if any)
- [ ] Ready to merge / tag / publish
