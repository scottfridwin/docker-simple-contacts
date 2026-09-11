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

## Post-implementation decision log (2026-09-10, later)

### K) Sharing/sync robustness follow-ups

A round of gap-analysis found several robustness issues in the sharing and
sync features; all were fixed except three that were explicitly deferred
(see the end of this section) because they're either large, cross-cutting
changes or genuine product/UX tradeoffs rather than pure bug fixes.

- **Failed sync jobs now retry with backoff.** Previously a job that failed
  once (`MarkFailed`) was never retried by anything - the only way it could
  ever sync again was if that same Person was edited a second time.
  `JobRepository.ListPending` now also selects failed jobs whose exponential
  backoff window (`2^attempts` minutes since the last attempt) has elapsed,
  up to a new `MaxJobAttempts` (5) cap; beyond that a job is left "failed"
  permanently (a dead letter, visible via `last_error`) rather than retried
  forever.
- **Recipients can now leave a share themselves.** New
  `DELETE /persons/{id}/shares/mine`, backed by
  `Repository.DeleteShareByRecipient` (matches on the caller's own
  `authn.UserID`, not a share ID) and `Service.LeaveShare` (uses the
  permissive `GetAccessible`, not the owner-only `GetByID`, since the caller
  here is expected to be the recipient). Previously only the owner could
  revoke a share; a recipient who no longer wanted a contact shared with
  them had no self-service way to remove it.
- **Shares and relationships are now capped** at `MaxSharesPerPerson` and
  `MaxRelationshipsPerPerson` (50 each), mirroring the existing caps on
  `custom_fields`/`emails`/`phone_numbers`/`addresses`, to prevent unbounded
  growth of either table for a single Person.
- **Share-target email is now format-validated** (reusing the same
  `emailPattern` regex already used for `Person.emails`) before it ever
  reaches a database lookup, instead of only checking for non-empty.
- **Basic per-IP rate limiting** was added via a new, dependency-free
  `internal/ratelimit` package (in-memory fixed-window limiter): login
  (`/auth/login`, 20/minute) and share creation (`POST
  /persons/{id}/shares`, 20/minute) are limited per client IP (via the
  nginx-set `X-Real-IP` header, which the client can't spoof since nginx
  always overwrites it with its own view of the TCP peer), returning `429`
  over the limit.

**Explicitly deferred** (raised with the user rather than unilaterally
decided, since each is either a large cross-cutting change or a real
product/UX tradeoff):
- **Optimistic concurrency on `Person.Update`** - it's currently a blind
  last-write-wins overwrite with no version/timestamp check. Sharing makes
  genuinely concurrent edits from two different people to the same record a
  normal occurrence now, not just a single-owner edge case. Fixing this
  properly requires a real API contract change (409 on conflict) and,
  more importantly, a UX decision for what the conflict experience should
  look like (block and reload? overwrite anyway with a warning? merge?).
- **Enumeration via `CreateShare`'s differentiated error messages** - an
  owner can learn whether an arbitrary email has ever logged into this
  instance from the error returned. Fixing this by hiding the reason would
  directly undo the just-shipped fix that surfaces the *specific* validation
  reason to the user (see the "request validation failed" fix earlier this
  session) - a real UX-vs-privacy tradeoff to weigh, not a pure bug.
- **CSRF tokens** - currently relying solely on `SameSite=Lax` cookies.
  Reasonable in modern browsers, but adding a dedicated CSRF token would be
  a cross-cutting change touching every mutating frontend call; deferred
  pending a decision on whether the added complexity is worth the
  defense-in-depth given `SameSite` already covers the common case.

Also considered but not pursued: adding `internal/contactsync` to the
backend coverage gate (`COVERAGE_PKGS`) - its three repository files
(`job_repository.go`, `record_link.go`, `repository.go`) are 0%-covered at
the unit level (pure DB code, like `person`'s repository layer), and
unlike `person`, `contactsync` doesn't yet have the pure-helper-function
extraction (`applyOwnership`, `scanPersonAccessible`-style) needed to close
that gap without a substantial refactor - left as a follow-up rather than
rushed.

## Post-implementation decision log (2026-09-10, even later)

### L) Google People API rate limits and etag conflicts on large real syncs

Syncing a real Google account with a large contact list surfaced two
production errors the adapter didn't handle: a `429 RESOURCE_EXHAUSTED`
("Critical read requests ... per minute per user" quota, limit 90/min) and
a `400 FAILED_PRECONDITION` ("Request person.etag is different than the
current person.etag"). Both previously aborted the whole sync run on the
first occurrence.

- **429 handling**: `Adapter.do` (the single chokepoint every People API
  call goes through) now retries up to 5 attempts with exponential backoff,
  preferring Google's `Retry-After` header (seconds) over our own backoff
  when present. This applies uniformly to every call (reads and writes),
  since `UpsertRecord`'s update path issues a `getContact` read immediately
  before every write - a large export can easily produce enough "critical
  read" requests to trip the quota on its own.
- **Etag conflict handling**: `UpsertRecord`'s update path now retries
  once on a `FAILED_PRECONDITION` response by re-reading the contact's
  now-current etag and re-issuing the update, instead of failing outright.
  This is a genuinely transient condition - it means the contact changed on
  Google's side in the (usually sub-second) window between our read of its
  etag and our update using it.
- **Not pursued**: a proactive client-side pacing limiter (vs. the current
  reactive retry-on-429 approach) and caching etags locally to avoid the
  extra read-before-every-write entirely (would need a schema change to
  store an etag per `sync_record_links` row) - both would reduce quota
  pressure further but are larger changes; the reactive retry/backoff fix
  is sufficient to stop large syncs from aborting outright.

## Post-implementation decision log (2026-09-10, latest)

### M) Custom fields are not synced with Google; a per-record failure duplicated contacts

Investigating a real duplicate-contact report ("Jason Cummings" ending up
as two local Persons after a large sync) surfaced two separate findings.

**Custom fields are not part of Google sync, in either direction**, and
there's a related data-loss risk: `personToRecord`/`remoteToLocal` (the
field-mapping layer) only ever handle the fixed built-in fields (name
parts, emails, phones, addresses, organization, notes, nickname,
birthdate, relationships) - `Person.custom_fields` has no mapping at all.
Google's `userDefined` field (its own "custom field/label" concept) was
used *only* to carry our internal `contacts_local_id` tag; worse,
`toGooglePerson` unconditionally replaced the *entire* `userDefined` list
with just that one tag on every write, silently deleting any custom field
a user had set up directly in Google Contacts. Fixed the data-loss part
now (`toGooglePerson` takes the contact's existing `userDefined` list and
preserves every entry except our own reserved key) since it's a pure bug
fix; full bidirectional `custom_fields` <-> `userDefined` sync is a real
feature with its own design questions (key/value shape mapping, collision
handling with our reserved keys, one Person synced to multiple Google
accounts each with their own independent `userDefined` list) - not
implemented, would need a separate decision.

**Root cause of the duplicate**: `pullRemote` aborted its *entire* pull
and rolled the cursor back to the value from *before the run started* the
moment any single record failed to merge (e.g. a persistent 500/429 that
outlived `a.do`'s own retries). The next sync attempt then replayed the
whole batch from that stale cursor - including contacts already
successfully created *and linked* earlier in the same aborted run. Those
replayed contacts still had no fresh `contacts_local_id` tag reflected in
this replay, so `mergeRemoteRecord` fell through to `findUnlinkedMatch`,
which (correctly, for its own purpose) excludes a match already linked to
*this* account - so the replay looked like a brand-new contact and got
created a second time. Fixed by:
- A single record's merge failure is now logged and skipped rather than
  aborting the rest of the page/pull (mirrored in `exportLocal` for the
  same reason on the export side).
- On any remaining pull-level error (only `ListChanges`/page-fetch
  failures now), the cursor already advanced through completed pages is
  returned and persisted, instead of the stale pre-run cursor.

## Post-implementation decision log (2026-09-10, final)

### N) Full bidirectional custom_fields <-> Google sync

Reverses the "custom fields aren't synced" state of things from decision
M above, after a design discussion with the user. Also **relaxes the
original custom-field key-format decision** (section 4 above, "lowercase
snake_case") - superseded by this entry, per explicit user request.

- **Key format relaxed**: a custom field key is now any non-empty,
  printable string (any case, spaces and punctuation allowed) up to 64
  characters, case-sensitive, with no snake_case/normalization requirement.
  `ValidateCustomFields`'s key check (`isValidCustomFieldKey`) only checks
  trimmed-non-empty, length, and absence of control characters. The old
  `snake_case` regex and the `_date`-key-suffix date-format special case
  were both removed entirely (the latter because it was meaningless once
  keys are freeform - a "date" is just a plain string value with no
  special validation now, same as any other custom field).
- **No prefix, no per-origin distinction**: every `userDefined` entry
  except our own reserved `contacts_local_id` tag is treated as a regular
  custom field, in both directions - there's no separate "system" vs
  "user-native" bucket. A field added directly in Google Contacts, on a
  contact already linked to this app, is pulled into `Person.custom_fields`
  on the next sync exactly like a field added through this app's own UI.
  Explicit, accepted consequence: a personal custom field a user only ever
  typed into *one* connected Google account is no longer private to that
  account - it becomes part of the shared `Person.custom_fields` and then
  propagates to every other linked Google account (and the local UI) the
  next time each syncs, the same way any other field edited directly in
  one Google account already propagates everywhere.
- **Keys round-trip verbatim** - no humanize/dehumanize transform. The key
  you type in Google Contacts is exactly the key stored locally, and vice
  versa (case-sensitive, byte-for-byte).
- **Values**: stringified on export (number -> decimal string via
  `strconv.FormatFloat`, boolean -> `"true"`/`"false"`, string as-is).
  Sniffed back into a typed value on import (exact `"true"`/`"false"` ->
  bool; a cleanly-parseable finite number -> float64; otherwise stays a
  string) - `sniffCustomFieldValue`/`stringifyCustomFieldValue` in
  `adapter.go`. Accepted tradeoffs: a value like a zip code with leading
  zeros (`"02134"`) will be misread as a number and lose them once it
  round-trips; `"Inf"`/`"NaN"`-looking text is explicitly guarded to stay
  a string rather than becoming a floating-point special value.
- **Merge granularity**: `custom_fields` is treated as one whole-map field
  (a single `FieldState` in `contactsync.Record.Fields`), resolved by the
  same whole-record last-write-wins timestamp comparison as every other
  field - not per-key. This isn't actually coarser than the rest of the
  sync model: Google has no per-`userDefined`-entry timestamp either, so
  finer-grained resolution isn't achievable regardless of how the code is
  structured.
- **Import validation is lenient, never fails the sync**: new
  `person.SanitizeCustomFieldsForSync` drops (doesn't error on) any
  imported entry that doesn't fit the policy (oversized key/value,
  control characters, more than 64 fields after a deterministic
  alphabetical-key sort) - a single unusable custom field must not block
  importing the rest of a contact's data. `toGooglePerson` rebuilds
  Google's `userDefined` list *authoritatively* from `custom_fields` plus
  the reserved tag on every write now (superseding decision M's
  "preserve existing entries" approach, which is no longer meaningful
  once every non-reserved entry is a first-class synced field rather than
  opaque data to leave alone).
- **Also fixed while implementing this**: `mergeRemoteRecord`'s three
  `person.Create`/`Update` call sites were silently discarding the
  `ValidationErrors` return value - a validation failure there returns a
  nil `*Person` with a nil `error`, which would have panicked on
  `created.ID` the first time imported custom field data (now far more
  likely than before) tripped a validation rule. Both `err != nil` and
  `verrs.HasErrors()` are now checked, treating a validation failure the
  same as any other per-record sync error (logged and skipped, per
  decision M).

## Post-implementation decision log (2026-09-11)

### O) Concurrent Sync() calls for the same account duplicated contacts

A production sync of a large, real Google account produced 115+ duplicate
contact pairs (e.g. "Dan Anderson" appearing twice) starting from an
empty database. Logs showed two `"google sync starting"` entries for the
same `account_id` 17 seconds apart, with no `"google sync finished"`
between them.

**Root cause**: nothing prevented two independent trigger paths from
calling `Adapter.Sync()` for the same account at the same time -
`runBackgroundSync` (fired immediately after a Google OAuth connect) and
`Runner.runDueAccounts` (the periodic scheduler, which only considers an
account "due" based on `LastSyncedAt`, itself only written once a `Sync()`
call *finishes*). A large contact list under heavy 429 throttling can take
much longer to pull than the scheduler's tick interval, so every tick kept
re-triggering another full, overlapping pull before the first one
finished. Two simultaneous pulls each see the other's not-yet-committed
`contacts_local_id` tags as absent, and `findUnlinkedMatch` deliberately
excludes a match already linked to the current account, so the second
pull's `mergeRemoteRecord` falls through to creating a brand-new Person
for almost every contact instead of finding a match.

**Fix**: `Adapter` gained a `syncing sync.Map` keyed by account ID.
`Sync()` calls `LoadOrStore` at its very start; if a sync for that account
is already in flight, the call logs and returns `nil` immediately instead
of running (this is a normal, expected outcome under the scheduler's
overlap - not an error to surface/retry). The map entry is cleared via
`defer` when the running sync finishes.

- Covered by `TestSyncRejectsConcurrentRunsForSameAccount`
  (`sync_scenarios_integration_test.go`), using a new
  `fakeGoogleServer.blockNextList` test hook that deterministically blocks
  one in-flight list-changes request so a second `Sync()` call can be
  started while the first is still running, without relying on real
  timing/sleeps.
- **Known limitation, accepted for now**: this guard is in-process only
  (a Go `sync.Map`), so it only prevents overlap within a single backend
  replica. It does not protect against overlap if the backend is ever
  horizontally scaled to multiple replicas - that would need a
  database-level advisory lock (e.g. Postgres `pg_advisory_lock` keyed on
  account ID) instead. Not implemented since the current deployment is
  single-replica; call out explicitly if that changes.

## Post-implementation decision log (2026-09-10)

### P) Labels (freeform tags), synced with Google Contacts' own Labels

New feature: `Person.labels` is a simple freeform string array (a
CATEGORIES/tag-style feature, like vCard/CardDAV `CATEGORIES` or Apple/
Nextcloud Contacts) - **not** a first-class, ID-referenced entity. There is
no separate `Label` table, no rename-everywhere operation, and no
dedicated `/labels` CRUD API; labels are edited directly on a Person via
the existing `PATCH /persons/{id}` (`labels` field), the same way
`middle_names` is. Rationale: Google's own `contactGroups` resource *is*
a heavier, ID-referenced, per-account entity, but a portability-minded
design (in case a non-Google, CardDAV-style provider is ever added) favors
keeping the local model as plain tag strings and pushing all of the
ID-mapping complexity into the Google adapter alone.

- **Policy**: at most 25 labels per Person, each a non-empty, printable
  string up to 64 characters (case-sensitive, no format requirement) -
  same style as the custom-field key policy. `person.SanitizeLabelsForSync`
  mirrors `SanitizeCustomFieldsForSync`'s "drop, never error on, anything
  that doesn't fit" behavior for data coming from Google.
- **Google mapping**: a local label maps to a same-named Google
  `contactGroups` resource (creating one on first export if it doesn't
  exist yet, matched by exact, case-sensitive name). A Google contact's
  membership in a **user-created** group becomes a label on import;
  membership in the `starred` **system** group maps to `is_favorite`
  (bidirectionally) instead of becoming a label; every other system group
  (`myContacts`, and the deprecated `family`/`friends`/`work`, etc.) is
  ignored entirely in both directions - importing `myContacts` verbatim
  would put a near-universal label on almost every contact, and `starred`
  already has a direct local equivalent.
- **`is_favorite` is now a synced field** for the first time (previously
  local-only) - a natural consequence of mapping it to `starred`. It
  follows the same whole-record last-write-wins comparison as every other
  synced field.
- **Never delete Google groups**: removing a label (or unfavoriting) only
  removes group membership via `contactGroups.members.modify`; the
  underlying Google group itself is never deleted, even if it becomes
  empty, since it might still mean something to the user outside this app.
- **Why membership is never set via `people.updateContact`**: the People
  API returns a 400 if `memberships` is included in `updatePersonFields`
  but would leave the contact with zero memberships - which is exactly
  what happens when clearing every label. Membership changes are instead
  always applied via the separate `contactGroups.members.modify` endpoint
  (diffing desired vs. the contact's actual current memberships), which
  has no such restriction and doesn't require re-sending the whole person
  payload.
- **Per-run group cache**: `Adapter` caches one account's full
  `contactGroups.list` result for the duration of a single `Sync()` run
  (keyed by `AuthSession.ProviderAccountID`, evicted via `defer` when the
  run ends), so label <-> resourceName lookups don't re-list groups (or
  risk creating duplicate groups) once per contact processed in that run.
  A 409 (duplicate name) on group creation triggers one refresh-and-retry
  rather than failing the record outright.
- **Multi-account behavior**: since `contactGroups` are per-Google-account,
  the same local label is resolved (and created if missing) independently
  against each linked Google account's own group namespace - identical in
  spirit to how `custom_fields`/`userDefined` values are independently
  reconciled per account.

## Post-implementation decision log (2026-09-11)

### Q) A multi-page pull never terminated (pageToken sent as syncToken)

Real production incident: a sync for one account ran continuously for
over 10 hours (285+ page fetches, ~28,500+ distinct contacts processed)
and never completed - `sync_accounts.last_synced_at` stayed null the
whole time, and every single page-fetch log line reported
`has_more=true`, never once reaching `has_more=false`.

**Root cause**: `people.connections.list` requires two *different*
tokens sent via two *different* query parameters - `pageToken` to
continue a listing already in progress, and `syncToken` to resume
incremental sync on a later, separate pull once a listing has fully
completed (only the *last* page of a listing carries `nextSyncToken` at
all). `Adapter.ListChanges` conflated the two into one opaque cursor
string and always sent it via `syncToken`, regardless of which kind of
token it actually held. On the second and later pages of any pull with
more than one page of contacts, this sent the previous page's
`nextPageToken` value through the `syncToken` parameter - which Google
appears to treat as an unrecognized/invalid sync token, silently
restarting the entire listing from the beginning rather than erroring.
Since already-linked contacts just get re-matched by their
`contacts_local_id` tag (not duplicated), this didn't produce duplicate
data, but it meant a pull with more than ~100 contacts (one page) could
never terminate - it kept restarting forever, continuously burning the
same rate-limit quota that was the subject of decisions O/P's retry
work, which likely made those symptoms far worse than they otherwise
would have been.

**Fix**: introduced `contactSyncCursor{PageToken, SyncToken}`, JSON-encoded
into the same opaque `Account.SyncCursor` string column (no schema
change) so `ListChanges` can tell which kind of token it's holding and
send it via the correct query parameter - `pageToken` when continuing a
listing already in progress, `syncToken` only when starting a fresh
pull. A pre-fix cursor value (a bare string, not JSON) decodes as a bare
sync token for backward compatibility.

- **Why this went uncaught**: the test fake Google server
  (`fake_server_test.go`) never paginated - it always returned every
  matching contact in a single response - so there was never a second
  page in any test to exercise the buggy code path at all.
  `fakeGoogleServer` gained `setListPageSize`/`listCallCount` so tests
  can force and verify real multi-page pulls; added
  `TestPullRemoteTerminatesAcrossMultiplePages`, which seeds more
  contacts than fit on one page and asserts the pull completes in a
  small, bounded number of list calls instead of looping.

## Post-implementation decision log (2026-09-11, later)

### R) A long-running sync silently reverted user-edited account settings

Real production incident: the user changed `sync_frequency_minutes` from
5 to 60 via the UI, but after a redeploy it was back to 5 in the
database.

**Root cause**: `Adapter.Sync` receives its `Account` by value and holds
that in-memory snapshot for the entire run - which, per decision Q above,
could be many hours for a large/rate-limited account. Three places
persisted sync progress mid-run or at the end
(`ensureSession`'s periodic OAuth token refresh, `markAccountFailed`,
and `Sync`'s own success path) and all three called the *same* blanket
`Repository.Update`, which does `UPDATE sync_accounts SET <every
column> ...` from whatever is in the in-memory struct - including
`display_name` and `sync_frequency_minutes`. If a user edited either of
those via `PATCH /sync-accounts/{id}` while a sync was still in flight
for that account, the change would persist correctly in the moment, but
the still-running sync's later write (using its own snapshot from
*before* the user's edit) would silently overwrite it back to the old
value - a classic read-modify-write lost update, on the whole row rather
than just the fields actually being changed.

**Fix**: added `Repository.UpdateSyncState`, which only ever writes
`access_token, refresh_token, expires_at, scope, sync_cursor, status,
last_synced_at, last_error` - never `provider`, `display_name`, or
`sync_frequency_minutes`. `Adapter`'s `accountStateStore` interface now
requires `UpdateSyncState` instead of `Update`, and all three of its
internal persistence points use it exclusively. The original `Update`
is unchanged and still used by the two places that legitimately need to
write user-editable fields on a short-lived, freshly-loaded snapshot:
the `PATCH /sync-accounts/{id}` handler and the OAuth reconnect flow.

- Covered by `TestUpdateSyncStateDoesNotClobberUserSettings`
  (`contactsync` package, integration-tagged): simulates a stale
  in-flight sync's snapshot persisting its own token refresh *after* a
  concurrent "user" PATCH-style `Update` changed `display_name`/
  `sync_frequency_minutes`, and asserts both the user's settings and the
  sync's own token write all survive.

### S) GOOGLE_SYNC_DRY_RUN: a write-suppression flag for testing

New optional env var `GOOGLE_SYNC_DRY_RUN` (default `false`). When true,
`Adapter.UpsertRecord`/`DeleteRecord` log what they would have sent and
return immediately without any HTTP request to Google at all - no
`createContact`/`updateContact`/`deleteContact`, and (since they return
before ever loading the group resolver) no `contactGroups` create/modify
either. Reads (`ListChanges`, `contactGroups.list`, `getContact`) are
completely unaffected, so the *import* side of sync can be exercised
against a real, live Google account with zero risk of mutating it -
useful for validating merge/import logic before trusting export against
production Google data. A dry-run create returns an empty `ExternalID`
(so `linkRecord` never links a person to a resource that doesn't exist);
a dry-run update returns the record's existing `ExternalID` unchanged.



