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

It can run behind a reverse proxy, or use the built-in Authentik OIDC
integration for account authentication.

[chi]: https://github.com/go-chi/chi
[pgx]: https://github.com/jackc/pgx
[golang-migrate]: https://github.com/golang-migrate/migrate

## Authoritative design documents

These are **binding requirements**. When in doubt, follow them. If they conflict,
the implementation-decisions file wins.

1. [docs/design/01-design-spec.md](docs/design/01-design-spec.md) — product intent.
2. [docs/design/02-development-guide.md](docs/design/02-development-guide.md) — engineering contract.
3. [docs/design/03-implementation-decisions.md](docs/design/03-implementation-decisions.md) — final decisions (**overrides** the guide on conflict).
4. [docs/design/04-sync-framework.md](docs/design/04-sync-framework.md) — sync architecture and Google baseline behavior.

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
.github/workflows/      build.yml (test + publish + promote-based release), ci.yml (deep gates)
```

## Non-negotiable decisions (v1)

- **Person fields**: `id` (UUID), `first_name`, `last_name` required;
  `middle_names` (ordered string array), `display_name` (always derived from name
  parts — not settable), `nickname`, `pronouns`, `birthdate` (ISO-8601 date string,
  YYYY-MM-DD), `emails` (ordered array of labeled entries, max 10:
  `{label, value}`, value must be a valid email address ≤ 254 chars),
  `phone_numbers` (ordered array of labeled entries, max 10: `{label, value}`,
  value ≤ 50 chars), `addresses` (ordered array of structured entries, max 10:
  `{label, street, city, region, postal_code, country}`, each string field
  ≤ 255 chars), `organization` (optional single `{name, title, department}`
  object), `notes` (optional free-text string, max 4096 chars),
  `custom_fields` (JSONB), `is_favorite` (boolean, default false),
  `created_at`, `updated_at`, `deleted_at` optional.
- **Labels**: `Person.labels` is a simple freeform string array (CATEGORIES/
  tag style, like vCard/CardDAV or Apple/Nextcloud Contacts) - not a
  first-class entity; no separate `Label` table, rename-everywhere
  operation, or dedicated CRUD API, edited via `PATCH /persons/{id}` like
  any other array field. Max 25 labels per Person, each a non-empty,
  printable string up to 64 chars. Google sync maps a label to a
  same-named `contactGroups` resource (created on first export if
  missing); importing, only **user-created** group membership becomes a
  label - the `starred` system group maps to `is_favorite` (now a synced
  field) instead, and every other system group (`myContacts`, etc.) is
  ignored. Membership changes are always applied via
  `contactGroups.members.modify`, never via `people.updateContact`'s
  `memberships` field (Google rejects that when it would leave zero
  memberships, e.g. clearing every label). Removing a label never deletes
  the underlying Google group, only the membership.
- **Relationships**: a `Person` can be related to another `Person` (or a
  free-text name, for someone not in the account's contacts) via exactly one
  of five fixed types: `parent`, `child`, `spouse`, `sibling`, `partner` — no
  custom types. One row is stored per relationship, from the creating
  person's perspective; the reverse view is computed by inverting the type
  (Parent↔Child; Spouse/Sibling/Partner are self-inverse), so there is never a
  second row to keep in sync. There is no update endpoint for a relationship —
  delete and recreate to change its type. Google sync only stores a relation
  as a free-text name (no record link); import links to an existing contact
  only on an exact, unambiguous display-name match, otherwise the
  relationship stays name-only.
- **Custom fields**: any non-empty, printable key (case-sensitive, no format
  requirement) up to 64 chars; string/number/boolean values; max 64 fields;
  string ≤ 1024 chars. `null` is rejected — omit a field to remove it.
- **Delete**: soft delete (recycle bin); a background job purges rows soft-deleted
  more than `PURGE_AFTER_DAYS` (default 30) ago.
- **List**: page size 25 (max 100); default sort `last_name, first_name asc`;
  filters `first_name`, `last_name`, `favorite`.
- **Favorites**: `Person.is_favorite` (boolean). The UI shows favorited
  contacts in an always-visible Favorites section, independent of the main
  list's page/sort/search — implemented client-side by fetching favorites
  separately (`GET /persons?favorite=true`), not by reordering the main
  list's results.
- **CORS**: explicit allow-list (dev: `http://localhost:5173`,
  `http://127.0.0.1:5173`). In the Compose setup the frontend nginx proxies
  same-origin, so CORS is not exercised locally.
- **Config**: env vars `PORT`, `LOG_LEVEL`, `ENV`, `DB_HOST`, `DB_PORT`,
  `DB_USER`, `DB_PASSWORD`, `DB_PASSWORD_FILE`, `DB_NAME` (default `postgres`),
  `DB_SSLMODE`, `CORS_ALLOWED_ORIGINS`, `PURGE_AFTER_DAYS`. `DB_PASSWORD_FILE`
  takes precedence over `DB_PASSWORD`; if neither is set, **startup fails**.
- **Logging**: JSON in production, human-readable in development, stdout/stderr
  only — never write log files.
- **License**: Apache-2.0. **Versioning**: SemVer. CI publishes `main` and
  `sha-<shortsha>` images on every push to `main`; releases are cut by running
  the *Build and Publish Docker Images* workflow with a `vX.Y.Z` input, which
  promotes (re-tags) the current `main` image to `vX.Y.Z`/`X.Y.Z`/`latest` — no
  rebuild. See the README release section.
- **Coverage**: CI enforces ≥ 70% on `config`, `httpapi`, `person`.
- **Scope**: authentication is limited to Authentik OIDC SSO and account-owned
  or account-shared Person records (see **Sharing** below). Google Contacts
  sync is supported through the existing sync framework and Google adapter
  only, including **multiple Google accounts** connected at once (each
  mirrors the full contact list both ways; no per-contact routing). Do NOT
  add passwords, local accounts, SSO providers other than Authentik, new sync
  *providers* without a design decision, background queues, file attachments,
  or offline PWA support.
- **Multi-account identity**: Google sync accounts are identified by the
  verified OAuth ID token subject (`sync_accounts.provider_account_id`), not
  by provider alone — never match/overwrite an existing account by provider
  name only. `sync_accounts.display_name` (the connected email) is for UI
  labeling only, never for identity matching.
- **Sync UX rules**: keep sync metadata keys (`google_resource_name`
  (legacy), `_google_updated_at`, `contacts_local_id`) reserved for system
  use; do not expose them as generic editable custom fields in list/detail
  UI. New Google syncs track remote record ids per account via the
  `sync_record_links` table, not `Person.custom_fields`. `Person.custom_fields`
  **is synced with Google bidirectionally** via `userDefined` entries: keys
  are used verbatim as the Google label (no snake_case/humanization), values
  are stringified on export and type-sniffed (number/boolean/string) on
  import. There is no origin distinction - a field added directly in Google
  Contacts is treated the same as one added locally, and propagates to every
  other Google account the Person is linked to. `toGooglePerson` rebuilds
  `userDefined` authoritatively from `custom_fields` plus the reserved
  `contacts_local_id` tag on every write (no longer merges with whatever was
  there before).
- **Sync robustness**: a single record failing to merge/export is logged
  and skipped, not treated as fatal for the whole pull/export - and the
  cursor returned reflects whatever progress was actually made, never a
  stale pre-run value, since replaying already-linked records can
  duplicate them (`findUnlinkedMatch` won't reuse a match already linked
  to the current account). `Adapter.Sync` also rejects a second concurrent
  call for the same account (in-process `sync.Map` guard) instead of
  letting two overlapping pulls race and duplicate contacts - the periodic
  scheduler and the post-OAuth-connect trigger can otherwise both fire for
  the same account while a large/rate-limited pull is still in flight.
  In-process only; would need a DB advisory lock if ever scaled to
  multiple backend replicas. A 429 retry (`Adapter.do`) prefers Google's
  `Retry-After` header, then a `window_start_time`-derived wait for its
  per-minute "Critical read requests" quota (which our own exponential
  backoff alone, capped well under a minute, can't reliably outlast),
  falling back to backoff otherwise; 6 attempts, backoff capped at 30s.
- A single Sync() run only refreshed the OAuth access token once, at the
  very top, before pulling/exporting - a large/slow pull (heavy 429
  backoff) can outlive the token's remaining lifetime and then fail
  partway through with 401 UNAUTHENTICATED even though the refresh token
  is fine. `pullRemote`/`exportLocal` now re-check (and refresh if needed)
  before every page, not just once per run.
- A pull spanning more than one page never terminated (ran 10+ hours in
  production): the second and later page requests sent Google's
  `nextPageToken` through the `syncToken` query parameter instead of
  `pageToken` - two different tokens for two different purposes - which
  Google silently treats as invalid and restarts the whole listing from
  scratch, so `has_more` never reached false. `ListChanges` now encodes
  which kind of token its cursor holds (`contactSyncCursor`, JSON in the
  same opaque `SyncCursor` string column) and sends it via the correct
  parameter. The test fake Google server never paginated before this, so
  no test could have caught it; it now supports forced pagination
  (`setListPageSize`/`listCallCount`) and there's a regression test
  asserting a multi-page pull terminates in a bounded number of calls.
- `Adapter.Sync` held its own in-memory `Account` snapshot for the whole
  run (which, per the bug above, could be hours) and, via `ensureSession`'s
  periodic token refresh or its own completion/failure handling, wrote
  that ENTIRE stale snapshot back with a blanket `UPDATE ... SET` - so a
  user changing `sync_frequency_minutes` (or `display_name`) via
  `PATCH /sync-accounts/{id}` while a sync was still in flight for that
  account got silently reverted back to whatever the sync's own snapshot
  had at the start of the run. `Repository.UpdateSyncState` now persists
  only the fields a sync itself owns (tokens, cursor, status,
  last-synced/error); `Adapter` uses it exclusively and never touches
  `display_name`/`sync_frequency_minutes`. The full `Update` (still used
  by the HTTP handlers for user-initiated changes) is unchanged.
- **Sync matching/merge**: a Google contact with no `contacts_local_id` tag
  matches an existing local contact only on an exact, unambiguous
  first+last name match (`person.FindByExactName`), otherwise it's created
  as a new Person — no fuzzy matching. When Google's copy of an
  already-linked contact wins the record-level last-write-wins comparison,
  only fields Google's own payload actually reported are applied
  (`FieldState.IsSet`); a field Google never had data for doesn't overwrite
  a local edit to that same field. Two edits to the *same* field still
  resolve by whichever side's timestamp is newer — there's no per-field
  timestamp on either side to do a real 3-way merge.
- **OAuth verification pages**: unauthenticated users must be able to view a
  public home page describing app purpose, and a public privacy policy page at
  `/privacy`.
- **Sharing**: an owner can share an individual Person with another account
  (looked up by exact email match; the recipient must have logged in at
  least once) via `person_shares`, granting that account view+edit access to
  the same record (not a copy). Deletion, restore/hard-delete, relationship
  management, and share management on that Person remain **owner-only** -
  sharing only extends to `GET`/`PATCH` on the base Person. Shared contacts
  are merged into the recipient's own list (`is_owner`/`owner_display_name`
  on the API response drive a "Shared by X" badge) and are included in the
  recipient's own Google sync export, since `person.List`/`GetAccessible`
  include both owned and shared-with-me rows for the current account. A
  recipient can remove their own access via `DELETE /persons/{id}/shares/mine`
  without the owner's involvement. Shares and relationships are each capped
  at 50 per Person. Share creation and login are rate-limited per IP
  (20/minute) via `internal/ratelimit`.

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
docker run --rm -v "$PWD/backend":/src -w /src golang:1.26-bookworm go test ./...
docker run --rm -v "$PWD/frontend":/app -w /app node:22-bookworm-slim sh -c "npm ci && npm test"
```

## Definition of done for a change

1. Code compiles; `go vet` and `golangci-lint` are clean.
2. Backend unit tests pass and coverage stays ≥ 70%; frontend tests pass.
3. `api/openapi.yaml` and `README.md` updated if behavior/commands changed.
4. Migrations are forward-only and apply cleanly (`up` then `down`).
5. No secrets, `.env`, `node_modules`, build output, or coverage files committed
   (see `.gitignore`).
6. Changes stay within the released v1 data model and the v2 Authentik SSO
   feature scope.

## Safety / operational notes

- Never commit real secrets. Local config lives in `.env` (git-ignored);
  `.env.example` documents every variable.
- Migrations are destructive if written carelessly — review `down` scripts.
- Prefer small, reviewable commits with SemVer-aware messages.
