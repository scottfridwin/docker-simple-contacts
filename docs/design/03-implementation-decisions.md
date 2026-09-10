# Contacts Project Implementation Decisions

## How to use this file
- Select one option for each decision item.
- Add clarifying notes where needed.
- Keep this file in the repository root so the coding agent can treat it as authoritative input.
- Once complete, include this file and the AI development guide in the initial implementation prompt.

## Decision Status
- Date: 08/25/2026
- Owner: Scott Fridlund
- Status: Final (with temporary defaults noted below)

## 1) Backend HTTP stack
Decision needed: API server and routing approach.

Options:
- Option A: Go standard library (net/http) + lightweight router
- Option B: Go framework (for example, Gin, Echo, Fiber)

Recommended default:
- Option A

Rationale:
- Minimal dependencies, easier long-term maintenance, better compatibility with strict linting and predictable behavior.

Selected option:
- **Option A**

Notes:
- 

## 2) Frontend framework
Decision needed: PWA frontend stack.

Options:
- Option A: React + Vite
- Option B: Vue + Vite
- Option C: SvelteKit

Recommended default:
- Option A

Rationale:
- Broadest ecosystem support, straightforward PWA plugins, mature testing and form libraries.

Selected option:
- **Option A**

Notes:
- 

## 3) Standard Person fields
Decision needed: exact standard fields and required/optional flags.

Recommended default field set:
- id (server-generated UUID, required)
- first_name (required)
- last_name (required)
- display_name (optional, derived fallback allowed)
- primary_email (optional)
- primary_phone (optional)
- notes (optional)
- created_at (server-generated, required)
- updated_at (server-generated, required)

Selected standard fields:
- id (server-generated UUID)
- first_name
- middle_names (optional ordered array of strings)
- last_name
- display_name (optional; derived fallback from name parts)
- custom_fields (JSONB)
- created_at (server-generated)
- updated_at (server-generated)

Required fields:
- id
- first_name
- last_name
- created_at
- updated_at

Notes:
- Start with just first_name and last_name as the user-settable fields. Expanding to include other useful fields can be a 1.0 goal AFTER we get a proof-of-concept running.
- Multiple middle names should be allowed, with the order being important. A composite "middle_name" value can be derived by concatenating the array of middle names in order.
- display_name is included to support default list sorting and can be auto-derived when not provided.

## 4) Custom fields policy
Decision needed: boundaries for custom field storage.

Options:
- Key format: lowercase snake_case only OR free-form labels
- Value types: string only OR string/number/boolean
- Per-person max field count
- Max key length
- Max string value length

Recommended default:
- Key format: lowercase snake_case
- Value types: string/number/boolean
- Max field count: 64
- Max key length: 64
- Max string value length: 1024

Selected policy:
- Key format: lowercase snake_case
- Value types: string/number/boolean/date
- Max field count: 64
- Max key length: 64
- Max string value length: 1024

Notes:
- Added "date" as a field type and kept all other defaults.

## 5) Delete behavior
Decision needed: hard delete vs soft delete.

Options:
- Option A: Hard delete (record removed permanently)
- Option B: Soft delete (deleted_at timestamp)

Recommended default:
- Option B

Rationale:
- Safer operational behavior and easier recovery from accidental deletion.

Selected option:
- **Option B**

Notes:
- Should include a "recycle bin" style delete mechanism so that deleted records are purged after some period of time.
- Temporary default for v1: purge soft-deleted records after 30 days.

## 6) Search and filtering depth in v1
Decision needed: list endpoint capabilities.

Options:
- Option A: Basic filtering on standard fields + sort + pagination
- Option B: Include full-text search on selected fields in v1

Recommended default:
- Option A

Rationale:
- Lower complexity for first release with clear migration path to full-text later.

Selected option:
- **Option A**

Notes:
- 

## 7) Pagination and sorting defaults
Decision needed: deterministic list behavior.

Recommended default:
- Default page size: 25
- Maximum page size: 100
- Default sort: updated_at desc

Selected defaults:
- Default page size: 25
- Maximum page size: 100
- Default sort: display_name desc

Notes:
- Display by name is the default for most contact management systems that I have used.

## 8) CORS policy
Decision needed: allowed origins for browser access.

Options:
- Option A: Explicit allow-list (recommended)
- Option B: Wildcard for development only

Recommended default:
- Explicit allow-list with separate development and production values.

Selected option:
- Agree with default.

Allowed origins (dev):
- http://localhost:5173
- http://127.0.0.1:5173

Allowed origins (prod):
- Set explicitly at deployment time to the reverse-proxy public origin(s).

## 9) License
Decision needed: repository license.

Options:
- Option A: MIT
- Option B: Apache-2.0

Recommended default:
- Option B

Rationale:
- Explicit patent grant and common choice for service-oriented projects.

Selected option:
- Temporary default for implementation: **Option B (Apache-2.0)**

Notes:
- I don't really care if anyone wants to copy or derive the work, only if they want to make money from it. Go with something standard for open-source projects.
- Important: Apache-2.0 permits commercial use. If non-commercial restrictions are required, replace before public release.

## 10) Versioning and release policy
Decision needed: branch and tag strategy.

Recommended default:
- Versioning: Semantic Versioning
- Release trigger: Git tag push in form vX.Y.Z
- Protected branch: main
- Image tags: vX.Y.Z, sha-<shortsha>, latest (main releases only)

Selected policy:
- Temporary default: use recommended policy in this section for v1.

Notes:
- Will be resolved at a later time
- For implementation now: Semantic Versioning, release on vX.Y.Z tags, protected main branch, image tags vX.Y.Z + sha-<shortsha> + latest (main releases only).

## 11) CI coverage threshold
Decision needed: enforce minimum line coverage or not.

Options:
- Option A: Enforce threshold in CI
- Option B: Report-only for first release

Recommended default:
- Option A with 70 percent minimum backend coverage initially.

Selected option:
- Agree with default.

Coverage threshold:
- Agree with default.

Notes:
- Whatever makes sense; it isn't a concern for me

## 12) Migration tooling
Decision needed: migration framework and execution model.

Options:
- Option A: golang-migrate
- Option B: goose

Recommended default:
- Option A

Rationale:
- Widely used, simple CLI and container-friendly workflows.

Selected option:
- **Option A (golang-migrate)**

Notes:
- Whatever makes sense; it isn't a concern for me
- Locked to Option A for implementation consistency.

## 13) PWA offline behavior
Decision needed: offline support target.

Options:
- Option A: No offline support in v1
- Option B: Read-only cache for last loaded list/detail
- Option C: Read/write queue with sync reconciliation

Recommended default:
- Option B

Rationale:
- Useful user experience gain without high synchronization complexity.

Selected option:
- **Option A**

Notes:
- For now let's not worry about offline access.

## 14) Logging format
Decision needed: runtime log format policy.

Options:
- Option A: JSON logs only in all environments
- Option B: JSON in production, human-readable in development

Recommended default:
- Option B

Selected option:
- Option B

Notes:
- When deployed, I want all logging to be routed to stdout/stderr so that it can be picked up by Docker log settings. No separate log files should be written; Docker controls the logging mechanism.

## 15) Initial performance target
Decision needed: explicit baseline for sizing and indexes.

Recommended default target:
- Dataset size: up to 100,000 Person records
- API list p95 latency: <= 300 ms for default page size
- API write p95 latency: <= 250 ms

Selected target:
- Recommended

Notes:
- If this is achievable that's great, but I am not too worried with performance initially. This is self-hosted and not intended to be enterprise-ready

## Final decisions summary (fill before implementation)
Copy completed values here so the coding agent has a compact, unambiguous input block.

- Backend HTTP stack: Option A (Go net/http + lightweight router)
- Frontend framework: Option A (React + Vite)
- Standard Person fields (required and optional): id (required), first_name (required), last_name (required), middle_names (optional ordered array), display_name (optional, derived fallback), custom_fields (optional JSONB), created_at (required), updated_at (required)
- Custom fields policy: snake_case keys; value types string/number/boolean/date; max 64 fields; max key length 64; max string length 1024
- Delete behavior: Soft delete (deleted_at) with recycle-bin behavior; temporary default purge after 30 days
- Search/filter scope: Option A (basic filtering + sort + pagination)
- Pagination/sort defaults: default page size 25; max page size 100; default sort display_name desc
- CORS policy: explicit allow-list; dev http://localhost:5173 and http://127.0.0.1:5173; prod set to reverse-proxy public origin(s)
- License: Temporary default Apache-2.0 (commercial use allowed); revisit if non-commercial restriction is required
- Versioning/release policy: Semantic Versioning; releases on vX.Y.Z tags; protected main; image tags vX.Y.Z + sha-<shortsha> + latest on main releases
- Coverage threshold: Enforce in CI; 70% minimum backend coverage
- Migration tooling: golang-migrate
- PWA offline behavior: Option A (no offline support in v1)
- Logging format: Option B (JSON in production, human-readable in development), all logs to stdout/stderr only
- Performance targets: up to 100,000 records; list p95 <= 300 ms; write p95 <= 250 ms (aspirational for v1)

## Handoff note for coding agent
Implement strictly according to:
- contacts-ai-development-guide.md
- contacts-implementation-decisions.md

If conflicts exist, values in this decisions file override recommendation defaults in the guide.

## Post-implementation decision log (2026-09-08)

These items capture decisions made during Google sync rollout so future changes
preserve expected behavior.

### A) Google sync provider scope
- Google Contacts is the first supported sync provider.
- Provider onboarding and callback endpoints are `GET /api/v1/sync/google/begin`
	and `GET /api/v1/sync/google/callback`.
- Additional providers require a design decision update before implementation.

### B) Sync account defaults
- Default `sync_frequency_minutes` for newly created sync accounts is **5**.
- UI and API should treat 5 minutes as the minimum valid periodic sync value.

### C) OAuth callback UX contract
- Browser-initiated callback requests should render a completion HTML page with
	clear user copy: authorization complete and safe to close.
- The callback page should post a completion message to the opener window and
	attempt auto-close for popup flows.
- API-oriented callers that request JSON must still receive JSON account data.

### D) Reserved sync metadata behavior
- The following `custom_fields` keys are reserved for sync internals:
	- `google_resource_name`
	- `_google_updated_at`
	- `contacts_local_id`
- Reserved keys must not appear as normal editable custom fields in generic
	list/form UI.
- Reserved keys may be shown in a separate read-only metadata area on edit
	screens, and must be preserved across updates.

### E) Public OAuth verification pages
- The root unauthenticated homepage must remain publicly accessible and explain
	the purpose of the application without requiring login.
- Privacy policy remains publicly accessible at `/privacy`.
- Rationale: required for Google OAuth app verification review.

## Post-implementation decision log (2026-09-09)

### F) Multi-account Google sync
- Multiple Google accounts can be connected simultaneously. Routing model is
  mirror-all: every connected account syncs the full contact list both ways;
  there is no per-contact assignment to a specific account.
- Google accounts are identified by the verified OAuth ID token subject
  (`sub`), not a fixed placeholder. This requires the `openid` and `email`
  scopes in addition to `contacts`.
- The verified email is stored as `sync_accounts.display_name` for UI labeling
  only; it is never used for identity matching.
- `GET /api/v1/sync/google/begin` requests `prompt=consent select_account` so
  the user can pick a different Google account without signing out of Google
  first.
- Connecting an account matches existing rows by `(provider,
  provider_account_id)`, not by provider alone, so a second Google account
  creates a new row instead of overwriting the first.
- Per-account remote-record mapping moved from the single-valued
  `Person.custom_fields.google_resource_name` to a dedicated
  `sync_record_links` table (`sync_account_id`, `person_id`, `remote_id`),
  since one Person can now be linked to a different remote contact on each
  connected account. See
  [04-sync-framework.md](04-sync-framework.md#multi-account-support-2026-09-09)
  for the full rationale.
- Fixed alongside this: `contactsync.Account` previously had no `json` tags,
  so the API emitted PascalCase field names instead of the snake_case the
  frontend expects (broke Save, the status badge, and the syncing indicator).
  `AccessToken`/`RefreshToken` are now `json:"-"` so OAuth tokens are never
  sent to the browser.

## Post-implementation decision log (2026-09-10)

### G) Expanded contact fields for real-world export fidelity
Real Google/Outlook/vCard contact exports carry more structure than the v1
Person model supported (a single string for phone numbers, no email, address,
company, or notes fields at all). To let synced/imported contacts round-trip
without data loss, the following first-class fields were added:

- `emails`: array of labeled entries (`{label, value}`), max 10, each value a
  valid email address ≤ 254 chars, label ≤ 50 chars. Mirrors the existing
  phone number policy shape.
- `phone_numbers`: **breaking change** — upgraded from a plain string array to
  the same labeled-entry shape as emails (`{label, value}`), so a number's
  type (mobile/home/work) survives round-trips through Google. Existing plain
  strings are migrated to `{label: '', value: <string>}` in
  `000008_add_contact_fields.up.sql`.
- `addresses`: array of structured entries (`{label, street, city, region,
  postal_code, country}`), max 10 entries, each string field ≤ 255 chars.
- `organization`: a single optional `{name, title, department}` object (not a
  list — Google supports multiple organizations per contact, but v1 only
  keeps the first one to match the "one current job" mental model most users
  have).
- `notes`: a single optional free-text field, ≤ 4096 chars.

Explicitly deferred (not implemented, revisit only with a new design
decision): contact relations (e.g. spouse/child), non-birthday events (e.g.
anniversary), Google-only contact group/label sync, name prefix/suffix,
phonetic names.

Also fixed as part of this work: `nickname` and `birthdate` already existed on
the Person model but were never mapped to/from Google's People API
(`nicknames` and `birthdays` fields respectively) — this was a pre-existing
gap, not a new field, so it's now wired up alongside the fields above since
both appear in real contact exports.

Storage: `emails`, `phone_numbers`, and `addresses` are `JSONB NOT NULL
DEFAULT '[]'`; `organization` is nullable `JSONB`; `notes` is nullable `TEXT`.

## Post-implementation decision log (2026-09-09, later)

### H) Relationships, favorites, and default sort

Relationships: implemented Person-to-Person relationships with a fixed type
enum (`parent`, `child`, `spouse`, `sibling`, `partner`) - **no custom
relationship types in v1**; the original GitHub issue proposed allowing
custom types, but that was explicitly descoped by the user ("no custom
relationships need to be supported, we can add more as the app evolves").

- Storage: one row per relationship (`person_relationships` table,
  `person_id` -> `related_person_id`/`related_person_name`, `type`), from the
  creating person's perspective. The reverse side (e.g. Child, for a Parent
  relationship) is computed at read time by inverting the type - Parent/Child
  are a directional pair, Spouse/Sibling/Partner are self-inverse. This
  avoids ever storing two rows that could drift out of sync.
- `related_person_id` is nullable to support a relationship that's just a
  free-text name (no link), needed both for manual entry (someone not in your
  contacts) and for Google sync, since Google's `relations` field only stores
  a name string with no link to an actual record.
- **No update/PATCH endpoint for a relationship** - only create and delete.
  To change a relationship's type, delete and recreate. This was a scope cut
  to avoid the complexity of "editing from the computed-reverse side needs a
  second type inversion before writing back to the canonical row"; not
  explicitly requested by the user but a reasonable simplification given nothing
  in the original request calls for in-place editing.
- Google sync: importing a Google relation resolves its free-text name to a
  local contact only on an **exact, unambiguous display-name match** (link if
  exactly one match, otherwise keep it name-only) - per explicit user
  decision, to avoid fuzzy-matching false positives. Google relation types we
  don't support (friend, relative, manager, assistant, referredBy, colleague,
  etc.) are silently skipped on import, consistent with no custom-type
  support.

Favorites: added `Person.is_favorite` (boolean). Per explicit user decision,
favorited contacts are shown in a **separate, persistent Favorites section**
in the UI (always visible regardless of the main list's current page, sort,
or search) rather than being pinned to the top of the main list via sort
order.

Default sort: changed from `display_name desc` to `last_name, first_name asc`
(a compound sort - ties on last name break by first name) per explicit user
request that the previous default was wrong for a contacts list.

## Post-implementation decision log (2026-09-09, later still)

### I) Sharing contacts between accounts

Reverses the v1/v2 "sharing contacts between accounts" out-of-scope
decision (see [02-development-guide.md](02-development-guide.md)) - the user
explicitly asked for this as the last feature needed before a v2.0 release.
Scoped down via clarifying questions before implementation:

- **Granularity**: individual contacts only, chosen one at a time by the
  owner - no "share my whole address book" toggle.
- **Access level**: view **and edit** the shared Person record itself
  (name, contact info, notes, custom fields, favorite flag) - edits are
  visible to the owner too, since it's the same underlying row, not a copy.
  Deliberately **not** extended to: deleting/restoring/hard-deleting the
  Person, managing its relationships, or managing its own shares (only the
  owner can share, re-share, or revoke) - avoids a real bug class where
  `person_relationships.owner_id` is stamped from whoever's currently acting,
  which would silently hide a recipient-created relationship from the actual
  owner.
- **Recipient lookup**: by exact (case-insensitive) email match against the
  `users` table - the recipient must already have logged in at least once
  via Authentik for a `users` row to exist. No invite-by-email-before-they-
  exist flow, and no user search/directory endpoint (avoids letting any user
  enumerate other accounts' emails).
- **Google sync**: shared contacts sync like owned ones - `person.List`
  (used by `exportLocal`) includes both owned and shared-with-me Persons for
  the current account context, so a recipient's own connected Google
  account(s) will also mirror contacts shared with them. No adapter-level
  change was needed for this; it falls out of the repository-level access
  change automatically.
- **Storage**: new `person_shares` table (`person_id`, `shared_with_user_id`,
  unique per pair) - a grant, not a copy. `Repository.GetAccessible`/`List`/
  `Update` were extended to allow `owner_id = viewer OR EXISTS (... a
  person_shares row for viewer)`; the existing strict, owner-only `GetByID`
  is untouched and still used by relationship management, delete/restore/
  hard-delete, and share management themselves, which is what keeps those
  operations owner-only.
- **API surface**: `Person` responses gained `is_owner` (bool) and
  `owner_display_name` (set only when `is_owner` is false), so the UI can
  show a "Shared by X" indicator without a separate lookup. New endpoints:
  `GET/POST /persons/{id}/shares`, `DELETE /persons/{id}/shares/{shareId}`
  (all owner-only).
- **UI**: shared contacts are merged into the main list (not a separate
  section) with a small badge; the owner manages sharing from a "Sharing"
  panel on the edit form (alongside Relationships), which - along with the
  Relationships panel - is hidden entirely when viewing a contact you don't
  own, since those operations 404 for a non-owner by design.
- **Not implemented** (explicitly out of scope for this pass, candidates for
  a later follow-up): per-recipient favorite state (favoriting a shared
  contact currently flips the single shared `is_favorite` column, visible to
  the owner too - there's no separate "my favorite of your contact" concept
  yet), and any notification to the recipient when a contact is first shared
  with them (they simply see it appear on next list load).

## Post-implementation decision log (2026-09-10)

### J) Cross-owner sync fan-out for shared contacts

Fixes a gap found while reasoning through a multi-account sharing + sync
scenario: a shared `Person` can independently be mirrored to more than one
Google account - the owner's own connection(s) and, separately, a
recipient's own connection(s) (see decision I). Before this fix, a local
edit only fanned out to sync accounts owned by whoever made the edit
(`Job.OwnerID`, sourced from the acting user), so an owner's edit never
reached the recipient's mirror (and vice versa) once the initial one-time
export had happened.

`Runner.runJob` now unions the acting user's own accounts with every
account already linked to that specific person via `sync_record_links`
(new `Repository.ListLinkedToPerson`, deliberately unscoped like
`ListDue`), deduplicated by account ID, and processes **each account under
its own owner's context** (not the triggering job's owner) so owner-scoped
lookups inside the provider adapter still resolve correctly per account.
This keeps every mirror of a shared contact converging on every edit,
regardless of which side made it.




