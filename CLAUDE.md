# Canopy

Control plane for coding-agent CLIs. Read `docs/SPEC.md` for the product and `docs/DECISIONS.md`
for where this repo deviates from it (DECISIONS wins).
`docs/RUNNING.md` covers running the two release binaries and every command and setting.

## Layout

- `hub/` Go API server (spec "apps/api"): REST `/api/v1`, board and runner WebSockets.
- `client/` Go runner binary `canopy` (spec "apps/runner"): orchestrator, Claude Code adapter,
  sandbox, directory sync.
- `shared/` Go module for the runner protocol; `openapi.yaml` and event schemas for the web app.
- `frontend/` Next.js app (spec "apps/web").
- `e2e/` Playwright tests per milestone.
- `deploy/` docker compose dev stack and Caddyfile.

## Commands

- `make infra` starts Postgres, Redis, RustFS (S3 on :9000, console :9001), Mailpit and Caddy
  (http://localhost:8000).
- `make hub` runs the API on :8080 (migrates on start). `make web` runs Next.js on :3000.
  Open http://localhost:8000 (Caddy). A fresh database prints a hub setup link in the hub log.
- `make test` runs Go tests (needs `make infra` for Postgres).
- `make gen` regenerates sqlc queries and TypeScript API types.
- Mail sent in dev is visible at http://localhost:8025 (Mailpit).

## Working rules

- Build milestone by milestone (SPEC "Build plan"). Write tests for a milestone's acceptance
  criteria first, then implement until they pass.
- Every authorization check goes through `hub/internal/authz`; check every REST call and every
  realtime message. Return 404, not 403, for objects in organizations the caller can't see.
- SQL lives in `hub/internal/store/queries/*.sql` (sqlc). Schema changes are new goose migrations
  in `hub/internal/store/migrations/`; never edit an applied migration.
- Never store provider keys in the database or logs; never widen a sandbox without a grant.
- UI copy: sentence case, plain verbs, curly quotes, no jargon such as "signal".
