# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

CPA Usage Keeper is a usage persistence and visualization service for [CLIProxyAPI (CPA)](https://github.com/router-for-me/CLIProxyAPI). It consumes CPA usage events from a Redis queue, persists them to PostgreSQL, provides aggregation APIs, and serves a React dashboard.

This repository is a fork of [Willxup/cpa-usage-keeper](https://github.com/Willxup/cpa-usage-keeper). The primary difference from upstream is **PostgreSQL replaces SQLite** as the database backend.

## Build & Development Commands

```bash
# Full verification (backend tests + frontend lint/test/typecheck/build)
make verify

# Backend only (requires running PostgreSQL with DATABASE_URL set)
DATABASE_URL=postgres://test:test@localhost:5432/cpa_usage_keeper_test?sslmode=disable go test ./cmd/... ./internal/...
go run ./cmd/server/main.go              # Run locally

# Frontend only (from repo root)
npm --prefix ./web ci                    # Install deps
npm --prefix ./web run dev               # Dev server (Vite)
npm --prefix ./web run test              # Vitest
npm --prefix ./web run lint              # ESLint
npm --prefix ./web run typecheck         # tsc --noEmit
npm --prefix ./web run build             # Production build

# Run a single Go test
DATABASE_URL=postgres://test:test@localhost:5432/test?sslmode=disable go test ./internal/service/ -run TestFunctionName -v

# Docker build verification
make verify-docker
```

## Architecture

**Stack:** Go 1.24+ backend (Gin + GORM + PostgreSQL), React 19 + TypeScript frontend (Vite + Zustand + Recharts/Chart.js).

### Data Flow

```
CPA → Redis Queue → Poller (pull/process runners) → PostgreSQL → Service layer → REST API → React Dashboard
```

### Package Layout (`internal/`)

| Package | Role |
|---|---|
| `config` | Env-based configuration loading (`.env` file) |
| `app` | Application assembly — wires all dependencies, manages background runners |
| `api` | Gin HTTP routes, auth middleware, handlers |
| `auth` | In-memory session management |
| `entities` | GORM models (10 entities: UsageEvent, RedisUsageInbox, ModelPriceSetting, etc.) |
| `repository` | PostgreSQL access via GORM, aggregation queries |
| `service` | Business logic — usage, pricing, identity, sync services |
| `poller` | Background Redis queue drain (RedisIngest with separate pull/process runners) |
| `quota` | Quota cache and refresh from CPA |
| `cpa` | HTTP client for CPA management API |
| `redact` | Field redaction for API responses |
| `timeutil` | Timezone normalization (configurable via `TZ`, default Asia/Shanghai) |
| `updatecheck` | GitHub release update checker |

### Background Runners (started by `app.StartBackground`)

- **RedisIngest** / **RedisProcess** — separate runners to decouple remote queue pulling from local processing
- **MetadataSync** — periodic sync of CPA auth files, API keys, pricing
- **Maintenance** — daily cleanup of processed inbox records and health stats
- **QuotaAutoRefresh** — periodic quota refresh for auth files (optional)

### Aggregation System

Overview stats use incremental aggregation with checkpoint tracking (`UsageOverviewAggregationCheckpoint`). Three stat tables: hourly, daily, and health. Aggregation catch-up runs at startup before background runners start, so the first page load doesn't trigger full table scans.

### Frontend (`web/`)

Single-page app with login page and usage dashboard. Uses Zustand for state, i18next for i18n, React Router for navigation. Embedded into the Go binary via `web/embed.go`.

## Key Design Decisions

- **PostgreSQL via pgx** — connection pooling (MaxOpenConns=10, MaxIdleConns=5), no SQLite PRAGMA/WAL/VACUUM needed
- **Interface-based dependencies** — services accept interfaces (e.g., `MetadataFetcher`, `RedisBatchSyncer`) for testability
- **Field redaction at API level** — sensitive data masked in responses, not at storage layer
- **Code comments are in Chinese** — domain terms and inline docs use Chinese alongside English identifiers
- **No backup runner** — PostgreSQL backup should be handled externally (pg_dump, cloud snapshots, etc.)
- **Logging via logrus only** — the fork uses logrus uniformly; `log/slog` is not used. When merging upstream `internal/api/` (Step 3 checks it out), convert any `slog` calls back to logrus (e.g., `slog.Error(msg,"error",err)` → `logrus.WithError(err).Error(msg)`). See commit `b4e4fa1`.

## Configuration

Environment-based via `.env` file. Required: `CPA_BASE_URL`, `CPA_MANAGEMENT_KEY`, `DATABASE_URL`. All settings documented in `internal/config/config.go`.

## API Structure

- Public: `GET /healthz`, `GET /api/v1/ping`
- Auth: `POST /api/v1/auth/login`, `GET /api/v1/auth/session`
- Protected: `/api/v1/usage/*`, `/api/v1/pricing`, `/api/v1/quota`, `/api/v1/cpa-api-keys`, `/api/v1/status`

## Testing Patterns

- Go tests colocated with source (`*_test.go`)
- Services use interface mocks (e.g., `MetadataFetcher` interface for CPA client)
- Repository tests use isolated PostgreSQL schemas via `internal/testutil/database.go`
- Frontend uses Vitest

## CI/CD

GitHub Actions: Linux testing with PostgreSQL service container, automated binary releases, Docker image builds to GHCR. Dev binaries publishable from non-main branches via workflow dispatch.

## Upstream Merge Procedure

Upstream (`Willxup/cpa-usage-keeper`) uses SQLite; our branch (`pg-migration-v2`) uses PostgreSQL. A direct `git merge` is not possible — use selective cherry-pick with manual adaptation.

### Prerequisites

```bash
git remote add upstream https://github.com/Willxup/cpa-usage-keeper.git  # once
```

### Step 1: Detect New Upstream Commits

```bash
git fetch upstream
git log --oneline pg-migration-v2..upstream/main
```

### Step 2: Categorize Changed Files

For each upstream commit, classify files into four buckets:

| Category | Action | Examples |
|---|---|---|
| **PG-safe backend** | `git checkout upstream/main -- <file>` | API handlers (`internal/api/`), service logic, DTOs, `internal/cpa/`, `internal/quota/` |
| **PG-safe frontend** | `git checkout upstream/main -- <file>` | i18n (`web/src/i18n/`), types (`web/src/lib/types.ts`), API client (`web/src/lib/api.ts`), store (`web/src/stores/`), styles (`web/src/pages/*.module.scss`) — **but NOT files listed in Step 5** |
| **SQLite→PG adaptation** | Checkout then manually adapt | `batch.go`, `*_test.go` (test DB setup, trigger syntax), `repository/*.go` (batch size calls) |
| **Skip entirely** | Do not checkout | `db.go`, `backup_runner.go`, `backup.go`, `migration/*`, `db_test.go`, `README.md`, `README.zh.md` |

### Step 3: Checkout PG-safe Files

**Backend (safe to checkout directly):**

```bash
git checkout upstream/main -- internal/api/ internal/service/dto/ internal/cpa/ internal/quota/ internal/config/ internal/timeutil/
```

> ⚠️ `internal/config/config.go` is **fork-modified** (5 HTTP/shutdown timeout fields + 4 DB pool fields + their `Load()` parsing; commits `e0817a6` and `c351633`). The checkout above overwrites it — after checkout, re-apply those fields (`HTTPReadHeaderTimeout`, `HTTPReadTimeout`, `HTTPWriteTimeout`, `HTTPIdleTimeout`, `ShutdownTimeout`, `DBMaxOpenConns`, `DBMaxIdleConns`, `DBConnMaxLifetime`, `DBConnMaxIdleTime`), their `*Default` constants, and the `Load()` parsing/validation, or restore with `git show e0817a6 c351633 -- internal/config/config.go`. See Step 4.5 #16–17 and Step 6.

**Frontend (checkout individually, skip files in Step 5):**

```bash
# Generally safe — i18n, types, API client, store, hooks
git checkout upstream/main -- web/src/i18n/ web/src/lib/types.ts web/src/lib/api.ts web/src/stores/

# Per-file checkout for pages/components — review diff first, skip files we own
git checkout upstream/main -- web/src/pages/UsagePage.tsx web/src/pages/UsagePage.module.scss
git checkout upstream/main -- web/src/components/usage/hooks/
```

### Step 4: Resolve Frontend Conflicts

The frontend has diverged from upstream. Our branch has unique components that upstream does not. After checking out upstream frontend files, manually verify these areas:

1. **Select vs Combobox** — We extracted `useDropdownPosition` hook and `_dropdown-panel.scss` partial. Upstream keeps positioning logic inline in `Select.tsx` and styles inline in `Select.module.scss`. We also added `title={opt.label}` tooltip on option labels. **Do NOT checkout `Select.tsx` / `Select.module.scss` from upstream** — preserve our refactored versions with tooltip.

2. **PriceSettingsCard** — We use `<Combobox>` for model name input (dropdown + free text). Upstream uses `<Select>`. After checking out upstream changes, re-apply our Combobox integration: replace `<Select>` with `<Combobox>` for the model name field.

3. **Component integration** — After checkout, verify all imports still resolve. Upstream may add new props to shared components (`Select`, `Input`, `Modal`) that our `Combobox` also needs.

### Step 4.5: Post-Merge Restoration Checklist ⚠️ CRITICAL

Upstream may remove or alter features that our fork depends on. After every merge, verify **each item** below and restore if missing. Do NOT skip this step — previous merges lost multiple features.

| # | Feature | What to check | Restoration if missing |
|---|---|---|---|
| 1 | **ApiKeySummaryTable** | `web/src/components/usage/ApiKeySummaryTable.tsx` exists; imported in `UsagePage.tsx`; rendered in overview tab | Restore from `git show <pre-merge-commit>:web/src/components/usage/ApiKeySummaryTable.tsx`; add to `index.ts` exports; add to `UsagePage.tsx` imports and JSX after `ServiceHealthCard` |
| 2 | **API Key Summary backend chain** | `usageOverviewResponse` in `usage_overview.go` has `api_key_summary` field; `buildUsageOverviewAPIKeySummary` function exists; `UsageOverviewSnapshot` in service/dto has `APIKeySummary`; service layer passes `overview.APIKeySummary`; **`apiKeySummaryAccumulator` is actually called** in `buildUsageOverviewFromStats` (not just defined) | Wire accumulator: create in `buildUsageOverviewFromStats`, call `.accumulateHourlyStat/.accumulateDailyStat/.accumulateEvent` at every stat processing site, assign `.toSlice()` before `finalizeUsageOverview` |
| 3 | **Overview model filter** | `overviewModelFilter` state in `UsagePage.tsx`; model Select in toolbar (own `apiKeyFilterGroup` div, NOT inside API key group); `isOverviewTab` guard; `model` param passed to `useUsageData`; `fetchUsageOverview` has `model` param | Restore state, add separate `<div className={styles.apiKeyFilterGroup}>` with `isOverviewTab && (...)` guard, pass `model: isOverviewTab ? overviewModelFilter : undefined` to `useUsageData` |
| 4 | **Model filter backend** | `filter.Model` passed to `UsageQueryFilter` in `service/usage.go` `GetUsageOverview`; hourly/daily/boundary events queries filter by `model` (**NOT health stats** — `UsageOverviewHealthStat` has no `model` column) | Add `Model: filter.Model` to `UsageQueryFilter` construction; add model filter to hourly stats, daily stats, boundary events queries; **do NOT add to health stats query** |
| 5 | **Dedicated /usage/models endpoint** | `fetchOverviewModels` in `api.ts`; `loadOverviewModels` callback in `UsagePage.tsx`; `overviewModelNames` is **independent state** (NOT derived from overview data); backend route `GET /api/v1/usage/models`; `ListOverviewModels` on `UsageProvider` interface; `ListOverviewModelNamesWithFilter` in repository | This endpoint must exist separately so model list doesn't shrink when a model filter is active. Model list comes from `DISTINCT model` on `usage_events`, not from overview data. |
| 6 | **i18n default language** | `DEFAULT_LANGUAGE` in `web/src/i18n/index.ts` is `'zh'` (not `'en'`) | Change `const DEFAULT_LANGUAGE = 'en'` back to `'zh'` |
| 7 | **i18n keys for model filter + summary** | `model_filter`, `all_models`, `api_key_summary_title`, `api_key` keys exist in all three locales (en, zh, zh-TW) in `web/src/i18n/index.ts` | Add after `api_key_filter_all` in each locale block |
| 8 | **Model filter reset on change** | `setOverviewModelFilter('')` called when `timeRange`, `customTimeRange`, or `selectedApiKeyId` changes | Add to the `useEffect` that calls `setEventsPage(1)` |
| 9 | **Combobox in PriceSettingsCard** | `PriceSettingsCard.tsx` imports and uses `<Combobox>` (not `<Select>`) for model name input | Replace upstream's `<Select>` with our `<Combobox>` for the model name field |
| 10 | **onNotice prop** | `PriceSettingsCard` has `onNotice` prop; `ApiKeySettingsCard` has `onNotice` prop; both called with toast messages | Re-add `onNotice?: (kind: 'success' \| 'info' \| 'error', message: string) => void` prop and notification calls |
| 11 | **Select disabled option** | `Select.tsx` has `disabled?: boolean` on `SelectOption`; keyboard nav skips disabled; `.optionDisabled` style exists | Restore from our pre-merge version — upstream may not have this |
| 12 | **API key redaction** | `buildUsageOverviewAPIKeySummary` in `usage_overview.go` uses `helper.RedactSensitiveValue(item.APIGroupKey)` for the `api_key` field; frontend displays `row.api_key` directly (backend already redacted) | Use existing `helper.RedactSensitiveValue` — do NOT write custom frontend masking |
| 13 | **Select option tooltip** | `Select.tsx` option label `<span>` has `title={opt.label}` attribute for showing full text on hover when truncated | Re-add `title={opt.label}` to the `<span className={styles.optionLabel}>` in `Select.tsx` |
| 14 | **Reset preferences button** | `UsagePage.tsx` topBar has a "重置/Reset" pill button between theme switcher and update check button; uses `signOutPill` style; `onClick` calls `localStorage.clear()` + `window.location.reload()` after `confirm()`; i18n keys `common.clear_cache` and `common.clear_cache_confirm` exist in all three locales | Restore button JSX in topBarActions between theme switcher and update check; restore i18n keys in all three locale blocks |
| 15 | **Graceful shutdown** | `App.Run()` in `internal/app/app.go` calls `serveUntilShutdown`/`notifyShutdown`/`buildHTTPServer` (NOT plain `ListenAndServe`); `stopBackgroundTasks` is split into `cancelBackground`+`waitBackground`; `App` struct has `httpServer` + `shutdownSignal` fields; `Close()` calls `httpServer.Shutdown`; imports `os`/`os/signal`/`syscall` | Re-apply on top of upstream's `app.go` (upstream blocks on plain `ListenAndServe`); see commit `e0817a6`. `internal/app/` is NOT in Step 3's checkout list so usually preserved — only at risk if someone runs a broad `git checkout upstream/main -- internal/app/`. |
| 16 | **HTTP server timeouts** | `internal/config/config.go` has fields `HTTPReadHeaderTimeout`/`HTTPReadTimeout`/`HTTPWriteTimeout`/`HTTPIdleTimeout`/`ShutdownTimeout` + their `*Default` constants + `Load()` parsing & positive validation; `App.buildHTTPServer()` applies them to `http.Server`; documented in `.env.example` | ⚠️ Step 3 checks out `internal/config/` from upstream which **WIPES these**. After checkout, re-add the 5 fields, constants, and `Load()` parsing (or `git show e0817a6 -- internal/config/config.go`); verify `buildHTTPServer` sets `ReadHeaderTimeout`/`ReadTimeout`/`WriteTimeout`/`IdleTimeout`. |
| 17 | **DB connection pool** | `internal/config/config.go` has fields `DBMaxOpenConns`/`DBMaxIdleConns`/`DBConnMaxLifetime`/`DBConnMaxIdleTime` + `*Default` constants (open=25, idle=10, lifetime=30m, idleTime=10m) + `Load()` parsing/validation (idle clamped to open); `repository.OpenDatabase` (`db.go`) applies them via `SetMaxOpenConns`/`SetMaxIdleConns`/`SetConnMaxLifetime`/`SetConnMaxIdleTime` | ⚠️ Same as #16 — Step 3's checkout wipes config.go. After checkout, re-add the 4 DB fields, constants, and `Load()` parsing (or `git show c351633 -- internal/config/config.go`); verify `db.go` still reads `cfg.DBMaxOpenConns` etc. (db.go itself is in Step 2's "skip" list, so it's usually preserved). |

### Step 4.6: Merge Lessons Learned 📌

These are concrete mistakes made during merges that caused production issues. **Do not repeat them.**

1. **Upstream deleted it ≠ you should delete it.** Upstream removed ApiKeySummaryTable and the model filter because they don't have those features. We do. Before removing anything upstream removed, check if it exists in our pre-merge code. Use `git log --oneline pg-migration-v2` to find our commits that added fork-unique features.

2. **Backend code exists ≠ backend code is wired.** `apiKeySummaryAccumulator` was fully implemented but never called. After restoring any backend feature, trace the full data path: is the accumulator/function actually invoked? Does the service layer pass the field? Does the API layer serialize it?

3. **Frontend param added ≠ backend receives it.** Added `model` param to `fetchUsageOverview` but forgot to pass `filter.Model` in the service layer `GetUsageOverview`. Always trace parameters end-to-end: API client → store → hook → API handler → service → repository → SQL query.

4. **Adding SQL filters blindly breaks queries.** Python script added `WHERE model = ?` to every query including `UsageOverviewHealthStat` which has no `model` column. **Always verify the target table has the column before adding a filter.** Check the entity struct first.

5. **Don't reinvent existing utilities.** Wrote a custom `maskApiKey()` in the frontend when the backend already had `helper.RedactSensitiveValue()` used everywhere else. **Search for existing redaction/masking/formatting utilities before writing new ones.** Check `internal/helper/`, `internal/redact/`, and existing components for patterns to reuse.

6. **Model list must be independent of filtered data.** Using `getOverviewModelNames(usage)` means the model dropdown shrinks to one entry when a model filter is active. The dedicated `/usage/models` endpoint exists specifically to avoid this — it queries `DISTINCT model` from `usage_events` without model filtering. **Never derive the filter's option list from the filtered data itself.**

7. **Check all config values after merge.** `DEFAULT_LANGUAGE` was silently changed from `'zh'` to `'en'` by upstream's `i18n/index.ts` checkout. After any `git checkout upstream/main --` command, diff the result against our version to catch config overrides.

8. **Test with the actual parameter before declaring done.** The model filter looked correct in code but returned 500 on production because health stats query hit a missing column. **Run `curl` with the actual query parameters** (including model filter) against the running server before marking complete.

### Step 4.7: Known test-package sync debt ⚠️ (as of 2026-06-13)

The fork synced upstream's overview-aggregation production refactor (commits like "Remove legacy usage snapshot path", "refactor: centralize usage cost calculation", "Add cached overview realtime backend") but **did not sync the corresponding test updates**. Result: `go test ./internal/api/ ./internal/repository/` does not compile. Specifically these test files are stale vs `upstream/main`:

- `internal/repository/usage_filter_test.go` — references removed API (`latestHourlySeriesStart`, `loadUsageOverviewBoundaryEventsWithFilter`, `loadUsageOverviewEventsWithFilter`, `UsageOverviewRecord.HourlySeries`/`DailySeries`); upstream's version is current (~1098 lines ahead).
- `internal/repository/usage_events_test.go` — uses removed `UsageQueryFilter.Source` (upstream renamed the test to `...AuthIndexAndResultFilters`).
- `internal/api/usage_overview_test.go` — stale fixtures (`StatisticsSnapshot.RequestsByHour`/`TokensByHour`, `UsageOverviewSeries.InputTokens` etc.); upstream's version has the realtime/key-overview tests the fork lacks.

**Fix = sync from upstream + PG-adapt** (do this with PostgreSQL running so tests execute):
1. `git checkout upstream/main -- internal/repository/usage_filter_test.go internal/repository/usage_events_test.go internal/api/usage_overview_test.go`
2. PG-adapt each: replace `db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "x.db")})` + its `if err != nil` block with `db := testutil.OpenTestDatabase(t)` (~36 sites total).
3. `closeTestDatabase` is **undefined in the fork** (it lived in upstream's `db_test.go`, which the fork's PG-adapt removed) but is referenced in these files AND in `internal/repository/usage_recent_event_cache_test.go`. Remove all `closeTestDatabase(t, db)` calls — `testutil.OpenTestDatabase` already drops the schema + closes via `t.Cleanup`. Fix imports (drop `config`/`path/filepath`, add `internal/testutil`).
4. Re-apply fork-unique: the fork's `service.UsageProvider` has `ListOverviewModels` (upstream's does not) — add a no-op `ListOverviewModels` to every `UsageProvider` stub the checkout brings (see Step 4.5 #5).

### Step 5: Adapt SQLite→PG Files

Common adaptations needed:

1. **`batch.go`** — `sqliteVariableLimit=999` → `pgVariableLimit=65535`; `insertBatchSize()` → `insertBatchSize(model)` (dynamic per-column)
2. **`*_test.go`** — Replace `repository.OpenDatabase(config.Config{SQLitePath:...})` with `testutil.OpenTestDatabase(t)`; convert SQLite trigger syntax (`RAISE(ABORT, ...)`) to PostgreSQL plpgsql (`RAISE EXCEPTION ...`)
3. **Call sites** — Update all `insertBatchSize()` calls to pass model type: `insertBatchSize(entities.UsageEvent{})`
4. **Imports** — Remove `gorm.io/driver/sqlite`, `path/filepath`, add `cpa-usage-keeper/internal/testutil`

### Step 6: Files to Always Preserve

These files are PG-specific or fork-unique and must **never** be overwritten by upstream:

**Backend (PG-specific):**
- `internal/testutil/database.go` — PostgreSQL test isolation (random schema per test)
- `internal/repository/batch.go` — `pgVariableLimit` instead of SQLite limit
- `internal/repository/usage_apikey_summary.go` — API key summary accumulator (fork-unique)
- `CLAUDE.md` — This file, with PG-specific documentation
- `.github/workflows/ci.yml` — PostgreSQL service container config

**Backend (fork-modified — checkout upstream then re-apply our parts, never blindly overwrite):**
- `internal/config/config.go` — Adds 5 HTTP/shutdown timeout fields (`HTTPReadHeaderTimeout`, `HTTPReadTimeout`, `HTTPWriteTimeout`, `HTTPIdleTimeout`, `ShutdownTimeout`) and 4 DB pool fields (`DBMaxOpenConns`, `DBMaxIdleConns`, `DBConnMaxLifetime`, `DBConnMaxIdleTime`), their `*Default` constants, and `Load()` parsing + validation (commits `e0817a6`, `c351633`). Step 3's `git checkout upstream/main -- internal/config/` overwrites this — re-apply after checkout. See Step 4.5 #16–17.
- `internal/app/app.go` — Graceful shutdown: `serveUntilShutdown`/`notifyShutdown`/`buildHTTPServer`, `stopBackgroundTasks` split into `cancelBackground`+`waitBackground`, `httpServer`+`shutdownSignal` fields, defensive `httpServer.Shutdown` in `Close()`, imports `os`/`os/signal`/`syscall` (commit `e0817a6`). Upstream blocks on plain `ListenAndServe`. Not in Step 3's checkout list (usually safe); never do a broad `git checkout upstream/main -- internal/app/`. See Step 4.5 #15.

**Frontend (fork-unique components):**
- `web/src/components/ui/Combobox.tsx` / `Combobox.module.scss` — Our custom Combobox (dropdown + free-text input)
- `web/src/components/ui/useDropdownPosition.ts` — Shared dropdown positioning hook (extracted from Select)
- `web/src/styles/_dropdown-panel.scss` — Shared dropdown/option SCSS partial (used by Select and Combobox)
- `web/src/components/usage/ApiKeySummaryTable.tsx` / `ApiKeySummaryTable.module.scss` — Our custom API key summary table

**Frontend (fork-modified files — do NOT blindly checkout from upstream):**
- `web/src/components/ui/Select.tsx` — Fork-modified: extracted `useDropdownPosition`, added `title={opt.label}` tooltip, added `disabled` option support
- `web/src/pages/UsagePage.tsx` — Fork-modified: model filter, API key summary, reset preferences button in topBar, overview model names endpoint
- `web/src/i18n/index.ts` — Fork-modified: `DEFAULT_LANGUAGE='zh'`, extra i18n keys for model filter, API key summary, reset preferences

### Step 7: Test & Verify

```bash
DATABASE_URL="<your-test-db-url>" go test ./internal/... -v
```

After testing, clean up leftover test schemas:

```bash
# List test schemas
psql $DATABASE_URL -c "SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'test_%';"
# Drop each one
psql $DATABASE_URL -c "DROP SCHEMA test_xxx CASCADE;"
```

### Step 8: Commit & Push

```bash
git add -A
git commit -m "feat: merge upstream vX.Y.Z - <summary of changes>"
git push origin pg-migration-v2
```
