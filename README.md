# Contacts

A self-hosted contact management system. It exposes a RESTful Go API backed by
PostgreSQL and ships an installable React PWA for managing contacts. The primary
deployment artifact is a set of container images.

> v1 scope: a single `Person` entity with standard fields plus user-defined
> custom fields, full CRUD via API and UI, and containerized delivery. There is
> no built-in authentication — deploy behind a reverse proxy that handles
> authn/authz.

## Architecture

```mermaid
flowchart LR
  U[Browser / PWA] -->|/api/v1| RP[Reverse proxy]
  RP --> API[Go API (net/http + chi)]
  API --> DB[(PostgreSQL)]
  RP --> FE[Static PWA (nginx)]
```

- **Backend** — Go (`net/http` + [chi] router), [pgx] for PostgreSQL access,
  [golang-migrate] for schema migrations. Structured logging via `slog`.
- **Frontend** — React + Vite + TypeScript, installable PWA (no offline support
  in v1), served by nginx.
- **Database** — PostgreSQL, a single database with a `persons` table. Custom
  fields are stored in a `JSONB` column.
- **Delivery** — Docker images for backend and frontend; Docker Compose for
  local development; GitHub Actions for CI and tagged releases.

[chi]: https://github.com/go-chi/chi
[pgx]: https://github.com/jackc/pgx
[golang-migrate]: https://github.com/golang-migrate/migrate

## Quickstart

### Option A — Dev container (recommended)

1. Open the repository in VS Code and choose **Reopen in Container**.
2. The container installs the Go toolchain, Node, linters, and migration
   tooling automatically (`.devcontainer/post-create.sh`).
3. Start the stack:

   ```bash
   cp .env.example .env
   make up
   ```

### Option B — Docker Compose only

```bash
cp .env.example .env
docker compose up --build
```

Services:

- API: http://localhost:8080 (health at `/healthz`, readiness at `/readyz`)
- Frontend: http://localhost:5173
- PostgreSQL: localhost:5432

## Configuration reference

The backend is configured entirely through environment variables.

| Variable               | Default     | Description                                             |
| ---------------------- | ----------- | ------------------------------------------------------- |
| `PORT`                 | `8080`      | HTTP listen port.                                       |
| `LOG_LEVEL`            | `info`      | `debug`, `info`, `warn`, or `error`.                    |
| `ENV`                  | `production`| `production` → JSON logs; anything else → text logs.    |
| `DB_HOST`              | _(required)_| PostgreSQL host.                                        |
| `DB_PORT`              | `5432`      | PostgreSQL port.                                        |
| `DB_USER`              | _(required)_| PostgreSQL user.                                        |
| `DB_PASSWORD`          | —           | PostgreSQL password (see precedence below).             |
| `DB_PASSWORD_FILE`     | —           | Path to a file with the password (Docker secrets).      |
| `DB_NAME`              | `postgres`  | Database name.                                          |
| `DB_SSLMODE`           | `disable`   | `libpq` sslmode.                                        |
| `CORS_ALLOWED_ORIGINS` | dev origins | Comma-separated allow-list of browser origins.          |
| `PURGE_AFTER_DAYS`     | `30`        | Recycle-bin retention before soft-deleted rows purge.   |

Frontend build-time variable:

| Variable            | Default | Description                              |
| ------------------- | ------- | ---------------------------------------- |
| `VITE_API_BASE_URL` | `""`    | Base URL of the API (empty = same host). |

**Secret precedence:** if both `DB_PASSWORD_FILE` and `DB_PASSWORD` are set, the
file wins. If neither is set, startup fails with an explicit error. All logs go
to stdout/stderr only — the container runtime owns log collection.

## Data model

A `Person` has:

- `id` (server-generated UUID), `first_name`, `last_name` (required)
- `middle_names` (optional ordered array of strings)
- `display_name` (optional; derived from the name parts when blank)
- `custom_fields` (JSONB map)
- `created_at`, `updated_at`, `deleted_at` (soft delete)

**Custom fields policy:** lowercase `snake_case` keys; scalar values of type
string, number, boolean, or date; max 64 fields; key max 64 chars; string value
max 1024 chars. JSON has no date type, so dates are ISO-8601 strings
(`YYYY-MM-DD` or RFC 3339). `null` values are rejected — omit a field to remove
it.

**Delete behavior:** soft delete with recycle-bin semantics. Deleted records are
excluded from reads and permanently purged after `PURGE_AFTER_DAYS` (default 30)
by a background job.

## API

- Base path: `/api/v1`
- Endpoints: `POST/GET /persons`, `GET/PATCH/DELETE /persons/{id}`
- List defaults: page size 25 (max 100), default sort `display_name desc`,
  filters `first_name` and `last_name`.
- OpenAPI specification: [api/openapi.yaml](api/openapi.yaml) (validated in CI).

## Development commands

Run `make help` for the full list.

| Command                  | Description                                    |
| ------------------------ | ---------------------------------------------- |
| `make up` / `make down`  | Start / stop the full stack.                   |
| `make backend-test`      | Backend unit tests.                            |
| `make backend-cover`     | Backend tests with the 70% coverage gate.      |
| `make backend-integration` | Integration tests (needs `TEST_DATABASE_URL`). |
| `make backend-lint`      | Run `golangci-lint`.                            |
| `make backend-fmt`       | Format Go code.                                 |
| `make migrate-up`        | Apply migrations (uses `DATABASE_URL`).         |
| `make frontend-test`     | Frontend unit tests (Vitest).                   |
| `make frontend-lint`     | Lint the frontend (ESLint).                     |
| `make frontend-build`    | Production build.                               |
| `make e2e`               | Playwright create/edit end-to-end test.         |

## Testing

- **Backend:** unit tests for config, validation, and service logic; endpoint
  tests over the full router; PostgreSQL integration tests behind the
  `integration` build tag. CI enforces a 70% coverage minimum on the business
  packages (`config`, `httpapi`, `person`).
- **Frontend:** Vitest component and validation tests; a Playwright end-to-end
  create → edit flow.

## Release process

Versioning follows [Semantic Versioning](https://semver.org/). The pipeline uses
a **build-once, promote** model (workflow: `.github/workflows/build.yml`):

1. **Every push to `main`** builds multi-arch images and publishes them to GitHub
   Container Registry tagged `main` and `sha-<shortsha>`:
   - `ghcr.io/scottfridwin/contacts-backend`
   - `ghcr.io/scottfridwin/contacts-frontend`
2. **To cut a release**, run the *Build and Publish Docker Images* workflow
   manually (Actions → Run workflow) from `main` with a `release_tag` like
   `v1.0.0`. This **promotes the current `main` image** (re-tags the existing
   manifest with `crane` — no rebuild) to `vX.Y.Z`, `X.Y.Z`, and `latest`, then
   creates the matching git tag and a GitHub Release.

Releases are intentionally decoupled from rebuilds so the exact artifact tested
on `main` is the one shipped. Pull requests run tests only; images are never
published from a PR.


## License

[Apache-2.0](LICENSE). Commercial use is permitted.
