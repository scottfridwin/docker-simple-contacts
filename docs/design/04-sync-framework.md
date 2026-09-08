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

- one connected external account per provider adapter, with a clear reauthorize
  path when credentials expire
- bi-directional record sync
- last-write-wins conflict handling at the record field level
- automatic sync after a local person save, limited to the changed record
- configurable sync frequency for periodic reconciliation
- an abstraction layer that can support future Google and Apple adapters

## Out of Scope for v1

The first iteration should not require:

- a polished multi-account UI
- simultaneous active syncing to multiple providers
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
