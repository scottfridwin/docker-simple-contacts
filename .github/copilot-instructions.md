# Copilot instructions

Self-hosted contact manager: **Go** REST API (`net/http` + chi, PostgreSQL via
pgx, golang-migrate) + **React/Vite/TypeScript** PWA served by nginx (which
reverse-proxies `/api` to the backend). Delivered as Docker images.

**Read [AGENTS.md](../AGENTS.md) first** — it is the source of truth for
architecture, conventions, commands, and binding decisions. The authoritative
design docs are in [docs/design/](../docs/design), including
[04-sync-framework.md](../docs/design/04-sync-framework.md) for sync behavior;
the implementation-decisions file overrides the development guide on any
conflict.

## Golden rules

- Stay within current scope. Authentication is Authentik OIDC only. Google
  Contacts sync is supported through the existing sync framework and adapter,
  including multiple connected Google accounts (each mirrors the full contact
  list both ways; no per-contact routing). Sharing individual contacts
  between accounts is supported (see below); do not add passwords, local
  accounts, alternate SSO providers, new sync providers without design
  approval, background queues, file attachments, or offline PWA support.
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
  `pronouns`, `birthdate` (ISO-8601 date string, YYYY-MM-DD), `emails` and
  `phone_numbers` (arrays of labeled entries `{label, value}`, max 10 each),
  `addresses` (array of structured entries `{label, street, city, region,
  postal_code, country}`, max 10), `organization` (single `{name, title,
  department}` object), `notes` (free-text string, max 4096 chars),
  `is_favorite` (boolean, default false).
- Relationships: `Person` to `Person` (or free-text name) links via exactly
  one of `parent`, `child`, `spouse`, `sibling`, `partner` (no custom types).
  One row per relationship, from the creating person's side; the reverse
  (e.g. Child for a Parent link) is computed by inverting the type, never
  stored twice. No update endpoint - delete and recreate to change the type.
  Managed via `GET/POST /persons/{id}/relationships` and
  `DELETE /persons/{id}/relationships/{relationshipId}`.
- Custom fields: any non-empty, printable key (case-sensitive, no format
  requirement) up to 64 chars; string/number/boolean values; max 64 fields;
  string ≤ 1024; `null` rejected.
- Soft delete + 30-day purge. List defaults: page 25 / max 100, sort
  `last_name, first_name asc`, filters `first_name`/`last_name`/`favorite`.
  Favorites are shown in an always-visible UI section (separate fetch with
  `favorite=true`), not by reordering the main list.
- Config: `DB_PASSWORD_FILE` overrides `DB_PASSWORD`; fail startup if neither set.
- Auth config uses `AUTHENTIK_*` and `SESSION_SECRET(_FILE)`; file secrets take
  precedence over inline values. Authenticated Person queries must be account
  scoped.
- Google sync accounts are identified by the verified OAuth ID token subject
  (`provider_account_id`), never by provider name alone; `display_name` (the
  connected email) is for UI labeling only, not identity matching.
- Google sync metadata keys (`google_resource_name` (legacy), `_google_updated_at`,
  `contacts_local_id`) are reserved system fields and should not be exposed as
  normal editable custom fields. Per-account remote record ids live in the
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
- Sync robustness: a single record failing to merge/export is logged and
  skipped, not treated as fatal for the whole pull/export - and the cursor
  returned reflects whatever progress was actually made, never a stale
  pre-run value, since replaying already-linked records can duplicate them
  (`findUnlinkedMatch` won't reuse a match already linked to the current
  account).
- Google sync matching/merge: a Google contact with no `contacts_local_id` tag
  matches an existing local contact only on an exact, unambiguous first+last
  name match (no fuzzy matching), otherwise it's created as a new Person. When
  Google's copy wins the record-level last-write-wins comparison, only fields
  Google's payload actually reported (`FieldState.IsSet`) are applied, so a
  field Google never had data for doesn't overwrite a local edit to it; two
  edits to the same field still resolve by whichever timestamp is newer.
- Keep public verification pages available without login: home page with app
  purpose and privacy policy at `/privacy`.
- Sharing: an owner can share an individual Person with another account
  (exact email match; recipient must have logged in at least once) via
  `person_shares`, granting view+edit access to the same record. Delete,
  restore/hard-delete, relationship management, and share management stay
  owner-only. A recipient can remove their own access via
  `DELETE /persons/{id}/shares/mine`. Shared contacts merge into the
  recipient's list (`is_owner`/`owner_display_name` on the API response) and
  are included in the recipient's own Google sync export. Shares and
  relationships are capped at 50 per Person. Managed via
  `GET/POST /persons/{id}/shares`, `DELETE /persons/{id}/shares/{shareId}`,
  and `DELETE /persons/{id}/shares/mine`.
- Failed sync jobs retry with exponential backoff up to `MaxJobAttempts`
  (5), then stay `failed` (dead letter) - see `contactsync.JobRepository`.
- Login and share creation are rate-limited per client IP (20/minute) via
  `internal/ratelimit`.

## Validate before finishing

```bash
make backend-cover     # tests + 70% coverage gate
make backend-lint      # golangci-lint
make frontend-test     # Vitest
make frontend-lint     # ESLint
```

If no local toolchain, run these inside `golang:1.26-bookworm` /
`node:22-bookworm-slim` containers (see AGENTS.md).
