# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

CPA Usage Keeper is a usage persistence and visualization service for [CLIProxyAPI (CPA)](https://github.com/router-for-me/CLIProxyAPI). It consumes CPA usage events from a Redis queue, persists them to SQLite, provides aggregation APIs, and serves a React dashboard.

This repository is a fork of [Willxup/cpa-usage-keeper](https://github.com/Willxup/cpa-usage-keeper). The primary goal of this fork is to **remove SQLite support and replace it with PostgreSQL**.

## Build & Development Commands

```bash
# Full verification (backend tests + frontend lint/test/typecheck/build)
make verify

# Backend only
go test ./cmd/... ./internal/...
go run ./cmd/server/main.go              # Run locally

# Frontend only (from repo root)
npm --prefix ./web ci                    # Install deps
npm --prefix ./web run dev               # Dev server (Vite)
npm --prefix ./web run test              # Vitest
npm --prefix ./web run lint              # ESLint
npm --prefix ./web run typecheck         # tsc --noEmit
npm --prefix ./web run build             # Production build

# Run a single Go test
go test ./internal/service/ -run TestFunctionName -v

# Docker build verification
make verify-docker
```

## Architecture

**Stack:** Go 1.22+ backend (Gin + GORM + SQLite), React 19 + TypeScript frontend (Vite + Zustand + Recharts/Chart.js).

### Data Flow

```
CPA → Redis Queue → Poller (pull/process runners) → SQLite → Service layer → REST API → React Dashboard
```

### Package Layout (`internal/`)

| Package | Role |
|---|---|
| `config` | Env-based configuration loading (`.env` file) |
| `app` | Application assembly — wires all dependencies, manages background runners |
| `api` | Gin HTTP routes, auth middleware, handlers |
| `auth` | In-memory session management |
| `entities` | GORM models (10 entities: UsageEvent, RedisUsageInbox, ModelPriceSetting, etc.) |
| `repository` | SQLite access via GORM, aggregation queries, schema migrations |
| `service` | Business logic — usage, pricing, identity, sync services |
| `poller` | Background Redis queue drain (RedisDrain with separate pull/process runners) |
| `quota` | Quota cache and refresh from CPA |
| `cpa` | HTTP client for CPA management API |
| `backup` | SQLite database backup management |
| `redact` | Field redaction for API responses |
| `timeutil` | Timezone normalization (configurable via `TZ`, default Asia/Shanghai) |
| `updatecheck` | GitHub release update checker |

### Background Runners (started by `app.StartBackground`)

- **RedisPull** / **RedisProcess** — separate runners to decouple remote queue pulling from local SQLite processing
- **MetadataSync** — periodic sync of CPA auth files, API keys, pricing
- **Maintenance** — daily cleanup of processed inbox records and VACUUM
- **BackupMaintenance** — periodic SQLite backup (optional, controlled by `BACKUP_ENABLED`)

### Aggregation System

Overview stats use incremental aggregation with checkpoint tracking (`UsageOverviewAggregationCheckpoint`). Three stat tables: hourly, daily, and health. Aggregation catch-up runs at startup before background runners start, so the first page load doesn't trigger full table scans.

### Frontend (`web/`)

Single-page app with login page and usage dashboard. Uses Zustand for state, i18next for i18n, React Router for navigation. Embedded into the Go binary via `web/embed.go`.

## Key Design Decisions

- **SQLite with WAL mode** — simplified deployment, busy timeout 5000ms, single connection pool
- **Interface-based dependencies** — services accept interfaces (e.g., `MetadataFetcher`, `RedisBatchSyncer`) for testability
- **Field redaction at API level** — sensitive data masked in responses, not at storage layer
- **Code comments are in Chinese** — domain terms and inline docs use Chinese alongside English identifiers

## Configuration

Environment-based via `.env` file. Required: `CPA_BASE_URL`, `CPA_MANAGEMENT_KEY`. All settings documented in `internal/config/config.go`.

## API Structure

- Public: `GET /healthz`, `GET /api/v1/ping`
- Auth: `POST /api/v1/auth/login`, `GET /api/v1/auth/session`
- Protected: `/api/v1/usage/*`, `/api/v1/pricing`, `/api/v1/quota`, `/api/v1/cpa-api-keys`, `/api/v1/status`

## Testing Patterns

- Go tests colocated with source (`*_test.go`)
- Services use interface mocks (e.g., `MetadataFetcher` interface for CPA client)
- Repository tests use real SQLite (in-memory or temp files)
- Frontend uses Vitest

## CI/CD

GitHub Actions: multi-platform testing (Ubuntu, macOS, Windows), automated binary releases, Docker image builds to GHCR. Dev binaries publishable from non-main branches via workflow dispatch.
