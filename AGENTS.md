# AGENTS.md

Guidance for AI coding agents (and humans) working in this repository. Read this
before making changes. It captures the architecture, conventions, commands, and
the binding decisions that govern this project.

## What this project is

A self-hosted **contact management system**:

- **Backend** — Go REST API (`net/http` + [chi] router), PostgreSQL via [pgx],
  schema migrations via [golang-migrate].
- **Frontend** — React + Vite + TypeScript, installable PWA (no offline in v1),
  served by nginx which also reverse-proxies `/api` to the backend.
- **Delivery** — Docker images for backend and frontend; Docker Compose for local
  development; GitHub Actions for CI and tagged releases.

It is designed to run behind a reverse proxy that handles authentication — the
API itself has **no auth** in v1.

[chi]: https://github.com/go-chi/chi
[pgx]: https://github.com/jackc/pgx
[golang-migrate]: https://github.com/golang-migrate/migrate

## Authoritative design documents

These are **binding requirements**. When in doubt, follow them. If they conflict,
the implementation-decisions file wins.

1. [docs/design/01-design-spec.md](docs/design/01-design-spec.md) — product intent.
2. [docs/design/02-development-guide.md](docs/design/02-development-guide.md) — engineering contract.
3. [docs/design/03-implementation-decisions.md](docs/design/03-implementation-decisions.md) — final decisions (**overrides** the guide on conflict).

## Repository layout

```
backend/                Go API
  cmd/server/           main entrypoint (config load, migrate, serve, purge loop)
  internal/config/      env-based configuration + validation
  internal/logging/     slog setup (JSON in prod, text in dev)
  internal/db/          pgx pool + migration runner
  internal/httpapi/     router, middleware, handlers, error envelope
  internal/person/      domain: model, validation, repository, service
  migrations/           golang-migrate SQL files (embedded)
frontend/               React + Vite PWA
  src/                  app, api client, components, custom-field logic
  tests/                Vitest unit/component tests
  e2e/                  Playwright end-to-end tests
  nginx.conf            serves SPA + proxies /api to the backend
api/openapi.yaml        OpenAPI 3 contract (validated in CI)
docs/design/            authoritative design documents
.github/workflows/      ci.yml (gates) and release.yml (tagged images)
```

## Non-negotiable decisions (v1)

- **Person fields**: `id` (UUID), `first_name`, `last_name` required;
  `middle_names` (ordered string array), `display_name` (derived when blank),
  `custom_fields` (JSONB), `created_at`, `updated_at`, `deleted_at` optional.
- **Custom fields**: lowercase `snake_case` keys; scalar values of type
  string / number / boolean / date; max 64 fields; key ≤ 64 chars; string ≤ 1024
  chars. Dates are ISO-8601 strings (JSON has no date type). `null` is rejected —
  omit a field to remove it.
- **Delete**: soft delete (recycle bin); a background job purges rows soft-deleted
  more than `PURGE_AFTER_DAYS` (default 30) ago.
- **List**: page size 25 (max 100); default sort `display_name desc`; filters
  `first_name`, `last_name`.
- **CORS**: explicit allow-list (dev: `http://localhost:5173`,
  `http://127.0.0.1:5173`). In the Compose setup the frontend nginx proxies
  same-origin, so CORS is not exercised locally.
- **Config**: env vars `PORT`, `LOG_LEVEL`, `ENV`, `DB_HOST`, `DB_PORT`,
  `DB_USER`, `DB_PASSWORD`, `DB_PASSWORD_FILE`, `DB_NAME` (default `postgres`),
  `DB_SSLMODE`, `CORS_ALLOWED_ORIGINS`, `PURGE_AFTER_DAYS`. `DB_PASSWORD_FILE`
  takes precedence over `DB_PASSWORD`; if neither is set, **startup fails**.
- **Logging**: JSON in production, human-readable in development, stdout/stderr
  only — never write log files.
- **License**: Apache-2.0. **Versioning**: SemVer; release on `vX.Y.Z` tags.
- **Coverage**: CI enforces ≥ 70% on `config`, `httpapi`, `person`.
- **Scope**: v1 only. Do NOT add auth, SSO, multi-tenancy, sync integrations,
  background queues, file attachments, or offline PWA support.

## Conventions

### Backend (Go)
- Keep dependencies minimal (Renovate-friendly). Prefer stdlib + chi.
- All SQL uses parameterized queries (never string-concatenate user input).
- Handlers return the standard error envelope `{ "error": { code, message, details? } }`.
- Validation lives in `internal/person/validate.go`; keep API and UI rules aligned.
- Business logic in `service.go`; persistence in `repository.go`; HTTP only in `internal/httpapi`.
- Format with `gofmt`/`goimports`; lint with `golangci-lint` (config in `backend/.golangci.yml`).
- Use `errors.Is` for sentinel comparisons; wrap errors with `%w`.

### Frontend (TypeScript/React)
- Strict TypeScript. Lint with ESLint (`frontend/eslint.config.js`); format with Prettier.
- API access goes through `src/api.ts`; custom-field validation/coercion through `src/customFields.ts`.
- Keep the API base URL empty by default (same-origin via the nginx proxy).

### API contract
- Any behavior change MUST update `api/openapi.yaml` and the relevant tests.
- Update the README when configuration or commands change.

## Common commands

Run `make help` for the full list. Key targets:

```bash
make up                  # build + start full stack (postgres, app, frontend)
make down                # stop and remove volumes

make backend-test        # Go unit tests
make backend-cover       # unit tests + 70% coverage gate
make backend-integration # integration tests (needs TEST_DATABASE_URL)
make backend-lint        # golangci-lint
make migrate-up          # apply migrations (needs DATABASE_URL)

make frontend-test       # Vitest
make frontend-lint       # ESLint
make frontend-build      # production build
make e2e                 # Playwright create/edit flow
```

No Go/Node toolchain locally? Every command above can be run inside the official
Docker images, e.g.:

```bash
docker run --rm -v "$PWD/backend":/src -w /src golang:1.23-bookworm go test ./...
docker run --rm -v "$PWD/frontend":/app -w /app node:20-bookworm-slim sh -c "npm ci && npm test"
```

## Definition of done for a change

1. Code compiles; `go vet` and `golangci-lint` are clean.
2. Backend unit tests pass and coverage stays ≥ 70%; frontend tests pass.
3. `api/openapi.yaml` and `README.md` updated if behavior/commands changed.
4. Migrations are forward-only and apply cleanly (`up` then `down`).
5. No secrets, `.env`, `node_modules`, build output, or coverage files committed
   (see `.gitignore`).
6. Changes stay within v1 scope.

## Safety / operational notes

- Never commit real secrets. Local config lives in `.env` (git-ignored);
  `.env.example` documents every variable.
- Migrations are destructive if written carelessly — review `down` scripts.
- Prefer small, reviewable commits with SemVer-aware messages.
