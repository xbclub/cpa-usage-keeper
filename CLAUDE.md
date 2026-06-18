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

### Step 4.7: Upstream test sync status (done 2026-06-13) + 2 remaining divergences ⚠️

The fork had synced upstream's overview-aggregation production refactor but not the test updates, so `internal/api` + `internal/repository` test packages didn't compile. **Resolved** (commits `3bd2879`, `649ea6b`): synced the stale test files from `upstream/main` + PG-adapted:
- `usage_filter_test.go`, `usage_events_test.go`, `usage_recent_event_cache_test.go`: PG-adapted `OpenDatabase(config.Config{SQLitePath})` → `testutil.OpenTestDatabase` (36 sites); removed the undefined `closeTestDatabase` calls (`testutil.OpenTestDatabase` cleans up via `t.Cleanup`); dropped now-unused `config`/`path/filepath` imports.
- `api/usage_overview_test.go`: brought upstream's realtime/key-overview tests; re-applied fork-unique `ListOverviewModels` on the stub and allowed the fork-unique `api_key_summary` response field.
- `usage.go`: `isMissingUsageEventsTableError` now also matches PostgreSQL's "does not exist" (SQLSTATE 42P01) so the stats-only analysis path degrades gracefully (was SQLite/MySQL-only).

`internal/api` passes fully; `internal/repository` passes **except 2 fork/upstream production-code divergences** (not test rot — each needs fork-overview-architecture review):
1. `TestBuildUsageOverviewWithFilterReusesBoundaryEventsForHealth` — fork's `usage.go` boundary-events path issues **4 queries** where upstream expects **2** (the boundary-event-reuse optimization). Decide whether the extra queries are intentional (recent-cache/PG path) or a regression.
2. `TestRepositoryQueriesAvoidKnownFullEntityReads` — fork's `usage.go` lacks `Select(usageEventProjectionColumns).Order("timestamp asc")` that this guardrail demands. Decide whether the fork's boundary read uses a valid alternative projection or regressed to full-entity reads.

**Do NOT blindly `git checkout upstream/main -- internal/repository/usage.go` to fix these** — it carries fork-unique logic (recent-event cache, API-key-summary accumulator, model filter, PG adaptations). Investigate the boundary-events query path specifically.

### Step 4.8: PG test-isolation caveat (RESOLVED)

`testutil.OpenTestDatabase` originally did `CREATE SCHEMA` + `SET search_path` (session-scoped) + `AutoMigrate`. GORM pools connections, so a query landing on a non-`search_path`'d pooled connection won't see the test schema. **This was a latent bug** — sequential tests rarely hit it, but v1.11.1's first concurrent-DB-query test (`TestResetAllowsConcurrentRequestsForDifferentAuthIndexes`) exposed it: one goroutine's query landed on a pool connection without the search_path, returning `record not found` for seeded data.

**Fixed** (commit `ef16e89`): `OpenTestDatabase` now creates the schema on a bootstrap connection, then reopens gorm with `options=--search_path=<schema>` in the DSN so **every** pooled connection inherits the isolated schema. Cleanup drops the schema via a separate connection. All tests (quota/repository/api/service) pass after the fix.

### Step 4.9: v1.10.7 merge notes (2026-06-15, commit `473662b`)

Merged upstream `v1.10.7` (`92c2d1a..997495c`) — but the bulk was already in via earlier merges. Only **3 PRs** were genuinely new; verified by functional-existence checks (not `git log`, which misleads after squash merges). Lessons specific to this merge:

1. **`git log pg-migration-v2..upstream/main` over-reports unmerged commits.** It listed 73 commits because the merge-base is an old commit (`2701c96`) and squash merges break patch-id matching. **Verify by feature existence in code, not by commit ancestry.** Concretely: grep for the target constant/function/field the PR introduces (`quotaWindowAverageMonthSeconds`, `redisPullQueueKeyCandidates`, `metadataSyncRefreshDebounceDefault = time.Second`). If it's already in our source, the PR is merged regardless of what `git log` says.

2. **The `pg-migration-v2..upstream/main` diff is dominated by fork-unique deletions.** A "204 files changed" diff mostly reflects that upstream *doesn't have* Combobox/ApiKeySummaryTable/migration-framework, so they show as "deleted". This noise obscures the ~5 real changes. **Filter the diff to source-only files and check each against current code before planning work.**

3. **DB migration must run BEFORE AutoMigrate for NOT NULL column renames.** #210 renames `redis_usage_inboxes.queue_key` → `source` (entity defines `source` as NOT NULL). On a legacy DB with existing rows, AutoMigrate's `ADD COLUMN source NOT NULL` fails (`column contains null values`). The fork has no schema-migration framework, so `migrateRedisInboxQueueKeyToSource` runs **before** AutoMigrate in `OpenDatabase` (`internal/repository/db.go`): adds nullable `source`, backfills `'redis_pull:' || queue_key`, drops `queue_key`, then AutoMigrate sets NOT NULL safely. Verified on a live DB with 20 real rows (`queue` → `redis_pull:queue`, zero data loss, idempotent on re-run).

4. **`$` dollar-quoting breaks under GORM/pgx parameter binding.** PG plpgsql function bodies written as `AS $BEGIN ... END;$` get mangled because pgx treats `# CLAUDE.md

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

### Step 4.7: Upstream test sync status (done 2026-06-13) + 2 remaining divergences ⚠️

The fork had synced upstream's overview-aggregation production refactor but not the test updates, so `internal/api` + `internal/repository` test packages didn't compile. **Resolved** (commits `3bd2879`, `649ea6b`): synced the stale test files from `upstream/main` + PG-adapted:
- `usage_filter_test.go`, `usage_events_test.go`, `usage_recent_event_cache_test.go`: PG-adapted `OpenDatabase(config.Config{SQLitePath})` → `testutil.OpenTestDatabase` (36 sites); removed the undefined `closeTestDatabase` calls (`testutil.OpenTestDatabase` cleans up via `t.Cleanup`); dropped now-unused `config`/`path/filepath` imports.
- `api/usage_overview_test.go`: brought upstream's realtime/key-overview tests; re-applied fork-unique `ListOverviewModels` on the stub and allowed the fork-unique `api_key_summary` response field.
- `usage.go`: `isMissingUsageEventsTableError` now also matches PostgreSQL's "does not exist" (SQLSTATE 42P01) so the stats-only analysis path degrades gracefully (was SQLite/MySQL-only).

`internal/api` passes fully; `internal/repository` passes **except 2 fork/upstream production-code divergences** (not test rot — each needs fork-overview-architecture review):
1. `TestBuildUsageOverviewWithFilterReusesBoundaryEventsForHealth` — fork's `usage.go` boundary-events path issues **4 queries** where upstream expects **2** (the boundary-event-reuse optimization). Decide whether the extra queries are intentional (recent-cache/PG path) or a regression.
2. `TestRepositoryQueriesAvoidKnownFullEntityReads` — fork's `usage.go` lacks `Select(usageEventProjectionColumns).Order("timestamp asc")` that this guardrail demands. Decide whether the fork's boundary read uses a valid alternative projection or regressed to full-entity reads.

**Do NOT blindly `git checkout upstream/main -- internal/repository/usage.go` to fix these** — it carries fork-unique logic (recent-event cache, API-key-summary accumulator, model filter, PG adaptations). Investigate the boundary-events query path specifically.

 as a positional-parameter prefix (the `$` collapses to `# CLAUDE.md

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

### Step 4.7: Upstream test sync status (done 2026-06-13) + 2 remaining divergences ⚠️

The fork had synced upstream's overview-aggregation production refactor but not the test updates, so `internal/api` + `internal/repository` test packages didn't compile. **Resolved** (commits `3bd2879`, `649ea6b`): synced the stale test files from `upstream/main` + PG-adapted:
- `usage_filter_test.go`, `usage_events_test.go`, `usage_recent_event_cache_test.go`: PG-adapted `OpenDatabase(config.Config{SQLitePath})` → `testutil.OpenTestDatabase` (36 sites); removed the undefined `closeTestDatabase` calls (`testutil.OpenTestDatabase` cleans up via `t.Cleanup`); dropped now-unused `config`/`path/filepath` imports.
- `api/usage_overview_test.go`: brought upstream's realtime/key-overview tests; re-applied fork-unique `ListOverviewModels` on the stub and allowed the fork-unique `api_key_summary` response field.
- `usage.go`: `isMissingUsageEventsTableError` now also matches PostgreSQL's "does not exist" (SQLSTATE 42P01) so the stats-only analysis path degrades gracefully (was SQLite/MySQL-only).

`internal/api` passes fully; `internal/repository` passes **except 2 fork/upstream production-code divergences** (not test rot — each needs fork-overview-architecture review):
1. `TestBuildUsageOverviewWithFilterReusesBoundaryEventsForHealth` — fork's `usage.go` boundary-events path issues **4 queries** where upstream expects **2** (the boundary-event-reuse optimization). Decide whether the extra queries are intentional (recent-cache/PG path) or a regression.
2. `TestRepositoryQueriesAvoidKnownFullEntityReads` — fork's `usage.go` lacks `Select(usageEventProjectionColumns).Order("timestamp asc")` that this guardrail demands. Decide whether the fork's boundary read uses a valid alternative projection or regressed to full-entity reads.

**Do NOT blindly `git checkout upstream/main -- internal/repository/usage.go` to fix these** — it carries fork-unique logic (recent-event cache, API-key-summary accumulator, model filter, PG adaptations). Investigate the boundary-events query path specifically.

 in the Go raw string literal before it even reaches the driver). **Use single-quote function bodies with doubled quotes for string literals inside:** `AS 'BEGIN IF NEW.status = ''processed'' THEN RAISE EXCEPTION ''...''; END IF; RETURN NEW; END;' LANGUAGE plpgsql`. This is the pattern used in `sync_test.go`'s two rollback-trigger helpers.

5. **SQLite `RAISE(ABORT)` trigger tests need PG plpgsql conversion every time they're re-checked-out.** `sync_test.go` carries two `CREATE TRIGGER ... WHEN NEW.status = 'processed' BEGIN SELECT RAISE(ABORT, ...); END;` for rollback tests. Upstream (SQLite) keeps them; every `git checkout upstream/main -- internal/service/sync_test.go` re-introduces them. Convert to the plpgsql `CREATE OR REPLACE FUNCTION` + `CREATE TRIGGER ... FOR EACH ROW EXECUTE FUNCTION` form (see step 4 above for the quoting). Locations: `TestProcessRedisUsageInboxDoesNotNotifyRecentCacheOnRollback` and `TestProcessRedisUsageInboxRollsBackEventsWhenProcessedMarkFails`.

6. **`sync_test.go` carries a dead `openSyncTestDatabaseWithLogs` function.** It's defined but never called, depends on `gorm.io/driver/sqlite` (not in fork's `go.mod`), and makes the whole `service` package fail to compile. When re-checking-out `sync_test.go`, delete `openSyncTestDatabaseWithLogs`, `closeTestDatabase`, and the now-unused `log`/`path/filepath`/`gorm.io/driver/sqlite`/`gormlogger` imports; convert `openSyncTestDatabase` to `testutil.OpenTestDatabase`.

7. **`testAppConfig` hardcodes `localhost:5432`.** `internal/app/app_test.go`'s `testAppConfig` helper hardcoded `DatabaseURL: "postgres://test:test@localhost:5432/test?sslmode=disable"`. On machines without local PG, all `TestNewWithConfig*` / `TestAppClose*` tests fail. Fix once: read `DATABASE_URL` env, fall back to localhost. This is a recurring fork test-debt — re-apply if upstream or a later merge reverts it.

8. **Diverged remote ⇒ reset-and-reda beats rebase when there's no shared ancestry.** When pushing was rejected (`47795c3` had 11 commits we lacked, and our base `34a8ec7` was a different-hash copy of remote's `e0e6c32` — same message, no git common ancestor), `git rebase` would fight every overlapping file. Instead: `git branch backup`, `git reset --hard origin/pg-migration-v2`, re-apply the PRs on the new base. The new base already had test fixes (commit `649ea6b`) that obviated manual work from the first attempt. **Before redoing, diff the target files between upstream and the new base to skip already-fixed ones** (e.g. remote had already added the `UsageProvider` stub methods and repaired `usage_filter_test.go`).

### Step 4.10: v1.10.8 merge notes (2026-06-16, commit `938552d`)

Merged upstream `v1.10.8` (`997495c..1016ee6`) — 2 PRs, 19 files. A clean, small merge: model pricing sync (#216, pull prices from Models.dev with preview/confirm/error UI) + analysis composition colors (#220). No schema changes, no migration. Lessons specific to this merge:

1. **`pricing_service_test.go` needs the same SQLite→PG adaptation as every other `service` test.** Upstream uses `OpenDatabase(config.Config{SQLitePath:...})`; fork uses `testutil.OpenTestDatabase(t)`. After `git checkout upstream/main -- internal/service/pricing_service_test.go`, convert `openPricingServiceTestDatabase` to `return testutil.OpenTestDatabase(t)` and drop `config`/`path/filepath` imports. This is the same recurring pattern as `sync_test.go` (Step 4.9 #6) — **every upstream test file that opens a DB will reintroduce SQLite setup on checkout.**

2. **`pricing_service.go` upstream already uses `logrus` (not `slog`).** The fork's "logrus-only" convention (commit `b4e4fa1`) is now reflected upstream for this file, so no logging conversion was needed. **But always verify** — grep `slog` in the checked-out file before assuming; future upstream files may still carry `slog`.

3. **`PriceSettingsCard.tsx` upstream now carries `onNotice` too.** Previously fork-unique (Step 4.5 #10), upstream adopted the same prop in the pricing-sync feature. After checkout, the only fork restoration needed was the **model-name input `<Select>` → `<Combobox>`** (Step 4.5 #9) + the `import { Combobox }` line. `onNotice` no longer needs manual re-application.

4. **`UsagePage.module.scss` upstream additions are additive and non-conflicting.** The +260 lines were all `pricing*`/`sync*` classes for the sync panel — none touched fork's `signOut*`/`apiKeyFilterGroup` classes. Safe to checkout directly; just verify fork classes survived (grep after checkout).

5. **i18n fork keys get wiped on every `git checkout upstream/main -- web/src/i18n/index.ts`.** The 6 fork-unique keys (`model_filter`, `all_models`, `api_key_summary_title`, `api_key`, `clear_cache`, `clear_cache_confirm`) across 3 locales were deleted again. Re-inserted via a Python script keyed on locale-block anchors (`api_key_filter_all`, `analysis_heatmap_api_key`, `logout`). **This is now a known recurring cost of any i18n checkout** — consider extracting fork i18n keys into a separate file to avoid this.

### Step 4.11: v1.11.0 merge notes (2026-06-17, commit `c1a716c`)

Merged upstream `v1.11.0` (`1016ee6..018db26`) — 12 commits, 41 files, +2856/-260. Three features: credential health cache + UI, usage event hard-deletion cleanup, custom date range bounds refresh. No schema changes, no migration. Lessons specific to this merge:

1. **`db.go` VACUUM must be skipped — PG does not need it.** Upstream's `CleanupStorage` now ends with `db.Exec("VACUUM")` (SQLite file shrink). The fork must NOT add this line — PG's VACUUM needs exclusive lock and has different semantics (AGENTS.md: "no SQLite PRAGMA/WAL/VACUUM needed"). **When merging db.go cleanup changes, add only the pure-GORM `CleanupUsageEvents` function; skip the VACUUM line and the standalone `Vacuum` helper.** The upstream `CleanupUsageEvents` itself is PG-safe (`db.Unscoped().Where(...).Delete(...)` is dialect-agnostic).

2. **`usage_identities_service.go` gained a `recentUsage` field — checkout brings it, app.go wires it.** Upstream added `NewUsageIdentityServiceWithRecentCache(db, recentUsage)`; the old `NewUsageIdentityService(db)` now delegates to it with `nil`. **Checkout the service file (pure upstream) then change app.go's call site** from `NewUsageIdentityService(db)` → `NewUsageIdentityServiceWithRecentCache(db, recentUsageCache)`. `recentUsageCache` is already in scope in `NewWithConfig`.

3. **`git apply --3way` is the fastest path for large fork-divergent files like `UsagePage.tsx`.** A 12-hunk, 205-line diff against a file the fork has heavily modified (model filter, ApiKeySummary, reset button) cannot `git checkout` wholesale and is painful to hand-apply. `git diff 1016ee6..upstream/main -- UsagePage.tsx > patch; git apply --3way patch` applied 10/12 hunks automatically and left only 2 conflict regions — both the same pattern: fork's `isOverviewTab`/`model:` props interleaved with upstream's `effectiveCustomTimeRange`/`activeTab === 'overview'`. **Resolution: keep both (fork's guard + upstream's effective range).** This is far less error-prone than manual block-by-block insertion.

4. **`usage_recent_event_cache_test.go` gained 5 credential-health tests using SQLite — checkout + batch PG-adapt works.** The perl one-liner `s/db, err := OpenDatabase\(config.Config{SQLitePath:.*\}\)\n.*\n.*\n.*\n/db := testutil.OpenTestDatabase(t)/g` + deleting `closeTestDatabase` calls + import swap handles it. **One gotcha:** a test that used `err =` (assignment, not declaration) after the DB-open block breaks because the `err` binding from `OpenDatabase` is gone — fix `err =` → `err :=` at that one call site.

5. **`db_test.go` tests `OpenDatabase` itself (SQLite runtime config) — do NOT checkout.** Upstream's `db_test.go` has `TestOpenDatabaseConfiguresSQLiteRuntime` and similar SQLite-specific tests. Fork's `db_test.go` is PG-adapted. **Restore fork version, then append only the new upstream test functions** (the 3 `TestCleanupStorageCleansUsageEvents*` tests) — they use `openTestDatabase(t)` which fork already provides, and are pure GORM. Extract via `git show upstream/main:db_test.go | sed -n '/^func Test.*CleanupUsageEvents/,/^func <next-helper>/p'`.

6. **`maintenance.go` upstream now uses logrus (not slog).** The fork's logrus-only convention is reflected upstream for this file (cleanup time changed 03:00→04:30). Safe to checkout directly — but always grep `slog` first.

### Step 4.12: v1.11.1 merge notes (2026-06-18, commit `fc813e0`)

Merged upstream `v1.11.1` (`018db26..ea90ca4`) — 4 commits, 30 files, +1746/-191. Single feature: Codex quota reset credits (#229 — POST /quota/reset endpoint + Auth Files UI button). No schema changes, no migration. Lessons specific to this merge:

1. **Initial file scan missed ALL 11 backend Go files.** The first pass only looked at frontend files (`git diff --stat` output was frontend-heavy). The entire backend stack (quota/reset.go NEW, service.go, codex.go, types.go, config.go, payloads.go, errors.go, api/quota.go, api/quota_test.go, router.go, service_test.go) was absent from the initial plan. **Always run `git diff --name-only` on the full upstream range and cross-check against your plan's file list before starting.** The "规划前再次检查" step caught this — without it the feature would have been dead (404 on /quota/reset).

2. **`router.go` checkout silently drops `registerUsageModelsRoute`.** Upstream's router.go doesn't have the fork-unique `registerUsageModelsRoute(adminProtected, usageProvider)` line (fork feature). A `git checkout upstream/main -- internal/api/router.go` succeeds (no compile error — Go allows unused functions), but the `/usage/models` route vanishes. **After any router.go checkout, grep for `registerUsageModelsRoute` and re-add if missing.** Alternatively, manual-merge by adding only the upstream's `Reset` method to the `QuotaProvider` interface.

3. **i18n python insert script broke AGAIN (third time).** The "first `'全部'` = zh, second = zh-TW" assumption is fragile — it breaks every time upstream adds i18n keys that shift line positions. This time it inserted 繁体 `model_filter` into the zh (simplified) block AND left zh-TW block without it (TS1117 duplicate key + missing key). **The script MUST verify the locale block context after insertion, not just count occurrences.** Until the script is fixed, manually verify each locale's `model_filter` value after every i18n checkout: en=Model, zh=模型筛选 (simplified), zh-TW=模型篩選 (traditional).

4. **Concurrent test `TestResetAllowsConcurrentRequestsForDifferentAuthIndexes` exposed a latent testutil bug.** Initially misdiagnosed as "cross-network PG latency"; the real root cause is `testutil.OpenTestDatabase`'s `SET search_path` being session-scoped — concurrent goroutines hitting a different pooled connection query the `public` schema and miss seeded rows (`record not found`). **Fixed by pinning search_path via DSN `options=--search_path=<schema>`** so every pooled connection inherits it. This was the Step 4.8 caveat manifesting — the first concurrent-DB-query test (v1.11.1) exposed it. All tests pass after the fix. **Lesson: verify DB connectivity and query results before attributing failures to "environment".**

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
- `internal/config/config.go` — Adds 5 HTTP/shutdown timeout fields (`HTTPReadHeaderTimeout`, `HTTPReadTimeout`, `HTTPWriteTimeout`, `HTTPIdleTimeout`, `ShutdownTimeout`) and 4 DB pool fields (`DBMaxOpenConns`, `DBMaxIdleConns`, `DBConnMaxLifetime`, `DBConnMaxIdleTime`), their `*Default` constants, and `Load()` parsing + validation (commits `e0817a6`, `c351633`). Step 3's `git checkout upstream/main -- internal/config/` overwrites this — re-apply after checkout. See Step 4.5 #16–17. **Also: as of v1.10.7 (#210) the `RedisQueueKey` config field/constant/`Load()` assignment were REMOVED** (queue key is auto-probed by `RedisPullSource` now); upstream still has them — re-delete after any checkout.
- `internal/app/app.go` — Graceful shutdown: `serveUntilShutdown`/`notifyShutdown`/`buildHTTPServer`, `stopBackgroundTasks` split into `cancelBackground`+`waitBackground`, `httpServer`+`shutdownSignal` fields, defensive `httpServer.Shutdown` in `Close()`, imports `os`/`os/signal`/`syscall` (commit `e0817a6`). Upstream blocks on plain `ListenAndServe`. Not in Step 3's checkout list (usually safe); never do a broad `git checkout upstream/main -- internal/app/`. See Step 4.5 #15. **Also: as of v1.10.7 (#210) this file no longer passes `RedisQueue`/`RedisQueueKey` into `SyncServiceOptions`, no longer passes `QueueKey` into `RedisQueueOptions`, and calls `NewRedisInboxWriter(db)` (no queueKey arg)** — re-apply these three edits if a checkout reverts them.
- `internal/repository/db.go` — **As of v1.10.7 (#210)**, `OpenDatabase` runs `migrateRedisInboxQueueKeyToSource(db)` BEFORE `AutoMigrate` (see Step 4.9 #3 for why ordering matters). This migration function is fork-only — never checkout `db.go` from upstream.

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
