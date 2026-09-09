# Sync Framework Design

## Purpose

This feature adds a provider-agnostic sync core for contact synchronization.
Google, Apple, CardDAV, or other third-party contact providers will be plugged
in through adapters that translate to and from the app's canonical `Person`
model.

The sync core owns common behavior:

- account connection lifecycle
- token/session state
- sync scheduling
- change detection
- conflict resolution
- local sync metadata

Provider adapters own provider-specific concerns:

- OAuth or equivalent account authorization
- remote resource listing and fetching
- remote create/update/delete operations
- provider cursors / etags / sync tokens
- field mapping quirks

## v1 Scope

The first iteration of sync should support:

- multiple connected external accounts, including multiple accounts of the
  same provider (e.g. two Google accounts), with a clear reauthorize path when
  credentials expire
- bi-directional record sync
- last-write-wins conflict handling at the record field level
- automatic sync after a local person save, limited to the changed record
- configurable sync frequency for periodic reconciliation
- an abstraction layer that can support future Google and Apple adapters

## Out of Scope for v1

The first iteration should not require:

- per-contact routing to a specific account (every connected account mirrors
  the full contact list both ways; see the multi-account model below)
- simultaneous active syncing to multiple *providers* in one release (multiple
  accounts of the *same* provider are supported)
- background queue retries beyond simple retryable failures
- webhook/push sync from providers
- group, photo, label, or relationship sync
- shared contact books
- perfect field-level three-way merges for every data type
- offline sync queues

## Core Scenarios

1. Initial connect

   A user authorizes a provider account. The backend stores the resulting
   session/token material securely and links it to a sync account record.

2. Initial import or export

   The first sync can pull remote contacts into the app or push local contacts
   out, but the chosen direction must be explicit.

3. Incremental local change sync

   When a `Person` changes in the app, the sync engine can enqueue a sync only
   for that record.

4. Incremental remote change sync

   A scheduled sync can fetch provider changes since the last cursor and apply
   them locally.

5. Conflict resolution

   If the same record changes on both sides, the newest update wins. When the
   timestamps are compatible, the engine should merge non-overlapping field
   changes instead of discarding unrelated data.

6. Token refresh and reauthorization

   Expired credentials should be refreshed automatically when possible. If not,
   the sync account should move into a reconnect-required state.

## Sync Abstraction Contract

The implementation should expose a common provider adapter contract with these
capabilities:

- begin authorization
- complete authorization
- refresh credentials
- read remote changes by cursor
- fetch one remote record
- upsert one remote record
- delete one remote record
- report provider capabilities

The sync core should also define a normalized record model with:

- a stable remote identifier
- a deletion marker
- per-field timestamps
- field-level values in a provider-neutral format

## Conflict Rules

- Use last-write-wins as the baseline rule.
- Prefer field-level resolution over whole-record replacement when possible.
- If two updates touch different fields, merge them.
- If two updates touch the same field, the most recent timestamp wins.
- If timestamps are equal and values differ, resolve deterministically and log
  the conflict for later review.

## Acceptance Criteria

- A sync adapter can be implemented without changing the sync core contract.
- A person update can trigger a record-scoped sync request.
- Sync state can track provider cursors and reconnect status.
- The merge logic can preserve non-overlapping local and remote edits.

## Implemented baseline (2026-09-08)

- First production adapter: Google Contacts.
- OAuth endpoints: `GET /api/v1/sync/google/begin` and
   `GET /api/v1/sync/google/callback`.
- New sync account default interval: 5 minutes.
- UI minimum sync interval: 5 minutes.

### Callback response behavior

- Browser callback requests return an HTML completion page and try to notify the
   opener window (`postMessage`) before auto-closing.
- Programmatic/API callback clients requesting JSON continue to receive JSON.

### Reserved sync metadata keys (legacy)

Earlier versions of the Google adapter stored `google_resource_name` in
`Person.custom_fields` to remember the linked remote contact. As of the
multi-account change below, this is replaced by the `sync_record_links` table
and is no longer written. Contacts synced before that change may still carry
the old key; the frontend continues to hide it from generic custom-field
editing as a defensive/backward-compatible measure:

- `google_resource_name` (legacy, no longer written)
- `_google_updated_at` (never a Person field; derived from Google's own
  per-contact metadata)
- `contacts_local_id` (never a Person field; a Google-side userDefined field
  that stores our local person id on that specific remote contact)

UI guidance:
- Do not expose `google_resource_name` as a generic editable custom field if
  present on legacy data.

## Multi-account support (2026-09-09)

### Decisions

- **Routing model**: mirror-all. Every connected account (including multiple
  accounts of the same provider) syncs the entire local contact list, both
  directions. There is no per-contact assignment to a specific account.
- **Account identity**: each Google sync account is identified by the OAuth
  ID token subject (`sub`), verified against Google's OIDC issuer
  (`https://accounts.google.com`) using the same `go-oidc` library already
  used for Authentik. This requires the `openid` and `email` scopes in
  addition to `https://www.googleapis.com/auth/contacts`.
- **Display identity**: the verified email claim is stored as
  `sync_accounts.display_name` purely for UI labeling (e.g. distinguishing
  "alice@example.com" from "bob@example.com"). It is never used for identity
  matching, since email addresses can change; `provider_account_id` (the
  subject) is the durable key.
- **Connecting an account**: `GET /api/v1/sync/google/begin` requests
  `prompt=consent select_account`, so Google always shows the account chooser,
  letting a user pick a different Google account without first signing out of
  Google. The callback matches existing accounts by `(provider,
  provider_account_id)`, not by provider alone, so connecting a second Google
  account creates a new row instead of overwriting the first.
- **Per-account remote-record mapping**: a Person can be linked to a different
  remote contact per connected account. A single `Person.custom_fields` value
  cannot hold more than one remote id, so a dedicated `sync_record_links`
  table (`sync_account_id`, `person_id`, `remote_id`, unique per pair) replaces
  the old flat `google_resource_name` custom field for outgoing sync. Remote
  contacts already carry a `local_id` userDefined field pointing back to the
  local person, so remote-to-local matching remains per-account safe without
  any additional schema.
- **Disconnecting**: `DELETE /api/v1/sync-accounts/{id}` removes one account;
  `sync_record_links` rows for that account cascade-delete automatically.

### Why a mapping table instead of Person.custom_fields

Storing the remote id on `Person.custom_fields.google_resource_name` only
supports one linked account per contact. With mirror-all routing, the same
contact is expected to be linked to every connected Google account
simultaneously, so a shared single-value field would cause the adapter to
lose track of one account's remote id every time it wrote the other account's
id, leading to duplicate contact creation on every sync cycle. The
`sync_record_links` table keys the mapping by `(sync_account_id, person_id)`
so each account keeps its own record independently.

