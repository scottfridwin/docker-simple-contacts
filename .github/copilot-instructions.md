# Copilot instructions

Self-hosted contact manager: **Go** REST API (`net/http` + chi, PostgreSQL via
pgx, golang-migrate) + **React/Vite/TypeScript** PWA served by nginx (which
reverse-proxies `/api` to the backend). Delivered as Docker images.

**Read [AGENTS.md](../AGENTS.md) first** — it is the source of truth for
architecture, conventions, commands, and binding decisions. The authoritative
design docs are in [docs/design/](../docs/design); the implementation-decisions
file overrides the development guide on any conflict.

## Golden rules

- Stay within **v1 scope**. Do not add auth, SSO, multi-tenancy, sync
  integrations, background queues, file attachments, or offline PWA support.
- Keep dependencies **minimal** (Renovate-friendly).
- Whenever API behavior changes, update `api/openapi.yaml`, the tests, and the
  README together.
- Migrations are **forward-only** and live in `backend/migrations`.
- Logs go to **stdout/stderr only** (JSON in prod, text in dev). Never write log
  files. Never log secrets.
- All SQL uses **parameterized queries**.

## Layout

- Backend domain: `backend/internal/person` (model, `validate.go`,
  `repository.go`, `service.go`). HTTP layer: `backend/internal/httpapi`.
  Config: `backend/internal/config`. Entrypoint: `backend/cmd/server`.
- Frontend: `frontend/src` (`api.ts`, `customFields.ts`, `components/`).
  Tests in `frontend/tests`; e2e in `frontend/e2e`.

## Key rules to preserve

- Required Person fields: `first_name`, `last_name`. `display_name` is always
  derived from name parts and is not settable. Optional fields: `nickname`,
  `pronouns`, `birthdate` (ISO-8601 date string, YYYY-MM-DD).
- Custom fields: `snake_case` keys; string/number/boolean/date values; max 64
  fields; key ≤ 64; string ≤ 1024; `null` rejected.
- Soft delete + 30-day purge. List defaults: page 25 / max 100, sort
  `display_name desc`.
- Config: `DB_PASSWORD_FILE` overrides `DB_PASSWORD`; fail startup if neither set.

## Validate before finishing

```bash
make backend-cover     # tests + 70% coverage gate
make backend-lint      # golangci-lint
make frontend-test     # Vitest
make frontend-lint     # ESLint
```

If no local toolchain, run these inside `golang:1.23-bookworm` /
`node:20-bookworm-slim` containers (see AGENTS.md).
