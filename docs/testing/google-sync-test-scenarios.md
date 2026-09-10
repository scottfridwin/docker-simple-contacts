# Google sync test scenarios

Manual/QA test plan for the Google Contacts sync adapter
(`backend/internal/contactsync/google`). Each scenario states the expected
behavior as implemented today (verified against the code, not just the
design intent), how to verify it, and any known gaps or risks worth watching
for. Timestamps referenced below are UTC.

## Automated coverage

Scenarios 1-10 (and the 404-recreation fix, exact-name matching, and
per-field merge fixes below) are exercised automatically in
`backend/internal/contactsync/google/sync_scenarios_integration_test.go`,
run via `make backend-integration` (needs `TEST_DATABASE_URL`). They run the
real `Adapter.Sync()` against a real Postgres-backed `person.Service` and a
fake in-memory Google People API (`fake_server_test.go`, an `httptest.Server`
standing in for `people.googleapis.com` - no real network access or Google
credentials needed). Building this framework is what surfaced the scenario 10
bug fixed below. Scenario 11 and the multi-account scenarios (12-16) are not
yet automated.

## How matching works (read this first)

There is **no fuzzy matching** (no name/email similarity check) anywhere in
the sync path. The only link between a local `Person` and a remote Google
contact is an explicit, machine-written tag:

- On every export (local → Google), we write a Google **userDefined** field
  `contacts_local_id` = the Person's UUID (`toGooglePerson`).
- On every import (Google → local), we read that same field back
  (`remoteToLocal` → `local_id`). If it's present and parses as a UUID, the
  remote record is treated as "this Person, possibly changed." If it's
  **absent or unparseable, the remote record is always treated as a brand
  new contact** and a new local `Person` is created - regardless of whether
  a Person with the same name/email already exists locally.

Practical implications:
- A contact that already exists in both places *before* the account was ever
  connected only gets linked together if a full export ties them by writing
  `contacts_local_id` back into that specific Google contact. Whether that
  happens for a given contact vs. creating a duplicate is exactly what
  scenario 7 below tests.
- Per-account linkage is tracked twice: (a) the `contacts_local_id`
  userDefined field on the Google side, and (b) the `sync_record_links`
  table locally (`sync_account_id`, `person_id`, `remote_id`). (b) lets the
  *same* local Person be linked to a *different* remote contact per
  connected account (needed for multi-account mirroring); (a) is what makes
  import-side matching possible at all.

## Timing model (matters for "how long until I see it")

- A local create/update/delete immediately persists a sync `Job` row
  (synchronous, in the API request), but it is only sent to the provider on
  the **next scheduler tick** (`Runner.RunLoop`, ticker interval configured
  at startup - check current `main.go`/env value; historically short, e.g.
  seconds).
- Any tick that processes a queued job for an account also runs a full
  `pullRemote` for that account as part of the same `Sync()` call - so a
  local edit "piggybacks" a remote pull even if that account isn't otherwise
  due yet.
- Independent of local edits, each connected account is pulled on its own
  `sync_frequency_minutes` schedule (`Runner.runDueAccounts`, minimum 5
  minutes, default 5 minutes) via `Sync()` with an empty `Job{}`
  (pull-only). So "new contact added only in Google, no local edits
  happening" is only picked up once that interval elapses.
- The very first sync after connecting an account (`account.SyncCursor ==
  ""`) additionally runs a one-time full `exportLocal` push of every
  existing local Person, after the initial pull.

## Conflict resolution model (2026-09-10: now partially field-aware)

[docs/design/04-sync-framework.md](../design/04-sync-framework.md) calls for
field-level merge ("if two updates touch different fields, merge them").
The implementation still resolves *which side wins* per whole record (there
is no per-field modification timestamp on either side - Postgres only
tracks one `updated_at` per row, and Google's People API only reports one
`metadata.sources[].updateTime` per contact), but `reconcileExisting` no
longer blindly copies every field from the winning side:

- `remoteUpdatedAt` = Google's own `metadata.sources[].updateTime` for the
  contact (authoritative, not something we write).
- `local.UpdatedAt` = the Person row's own `updated_at`.
- Whichever is newer wins the *record-level* comparison, but when Google
  wins, `fieldAwareUpdate` only applies a field if Google's own payload
  actually reported it (`FieldState.IsSet`) - see `toProviderRecord`, which
  leaves e.g. `organization`/`notes`/`nickname`/`birthdate` unset when
  Google has no biography/organization/etc. at all. A field Google never
  had data for no longer silently wipes out a local edit to that same
  field just because the record as a whole is newer on Google's side.
- Two *genuinely* conflicting edits to the **same** field still resolve via
  the whole-record timestamp (there's no way to merge two different values
  for one field without a real 3-way baseline) - see scenario 9.
- When local wins, the full local record is still pushed to Google
  unconditionally (`personToRecord`/`toGooglePerson`) - that direction was
  already correct, since local is authoritative for anything it currently
  holds.
- If timestamps are exactly equal, neither side is touched that cycle.

This fixes the scenario 8 data-loss case for fields Google's copy never had
any data in, but two edits to the *same* field (scenario 9) still can't be
merged without inventing a real baseline-snapshot mechanism (see the "Not
fixed" note in scenario 8 below).

---

## 1. New contact added in Google

**Setup:** Connect account, let initial sync settle. In Google Contacts, add
a brand-new contact (not previously synced).

**Expected:** On the next pull (piggybacked on any local job, or on the
account's next due interval), `mergeRemoteRecord` sees no `contacts_local_id`
→ creates a new local `Person` (`remoteToLocal` returns `localID == nil`)
→ immediately pushes it back to Google tagged with the new local UUID
(`contacts_local_id`) → records the mapping in `sync_record_links`.

**Verify:** New Person appears in the app with all mapped fields (name,
phones, emails, addresses, org, notes, nickname, birthdate, relations).
`sync_record_links` has a row for (account, new person). The Google contact
now carries a `contacts_local_id` userDefined value it didn't have before.

**Edge case to also try:** A Google contact with only a last name, or only a
first name - `remoteToLocal` defaults missing first/last name to the literal
string `"Unknown"` rather than rejecting the record.

## 2. New contact added in Contacts

**Setup:** Create a Person in the app.

**Expected:** `RecordChanged` persists a `Job{Kind: Created}`. Next tick:
`syncLocalJob` builds a record from the snapshot (`snapshotToRecord`,
including a `local_id` field), calls `UpsertRecord` with no `ExternalID` →
Google `createContact` → `linkRecord` stores the new `sync_record_links` row.
The same tick's `pullRemote` will then see this same contact come back
*from* Google in the connections list (still fine - `contacts_local_id` is
already present, so `remoteToLocal` resolves it as an existing Person and
takes the "existing" branch of `mergeRemoteRecord`, which just re-confirms
the link and compares timestamps - no duplicate is created).

**Verify:** Contact appears in Google Contacts with the same field values.
Exactly one `sync_record_links` row for it. No duplicate local Person is
created by the pull that immediately follows the push in the same tick.

## 3. Modify contact in Google

**Setup:** An already-linked contact. Edit a field in Google Contacts (e.g.
add a phone number).

**Expected:** Next pull: `remoteUpdatedAt` (Google's new `updateTime`) is
after `local.UpdatedAt` → full-record local `Update` with every field from
`remoteModel` (not just the phone number) → local record now matches Google.
Relationship reconciliation for this person is deferred to the second pass
of the same pull (see the Relationships section below).

**Verify:** Local record reflects the Google-side edit exactly. `updated_at`
on the Person row advances to "now" (the time of the local write), not to
Google's timestamp.

## 4. Modify contact in Contacts

**Setup:** An already-linked contact. Edit a field in the app.

**Expected:** `RecordChanged` queues `Job{Kind: Updated}`. Next tick:
`syncLocalJob` pushes the *entire current* record to Google via
`UpsertRecord` (full overwrite, not a partial patch) using the known
`ExternalID` from `sync_record_links`.

**Verify:** Google Contacts reflects the app-side edit. Fields you didn't
touch are preserved because the full local record (not a diff) is what gets
sent - but see scenario 8 for why this "always send everything" model
matters when Google *also* changed something in the same window.

## 5. Delete contact in Google

**Setup:** An already-linked contact. Delete it in Google Contacts.

**Expected:** Next pull: the connections list (or a tombstone via sync
token) reports `metadata.deleted = true`. `remoteUpdatedAt.After(local.UpdatedAt)`
→ local soft-delete (`people.Delete`, sync-origin context so it doesn't
re-trigger an outgoing job). If instead `local.UpdatedAt` is newer (i.e. the
local contact was edited *after* Google's delete timestamp), the code
re-pushes the local record to Google instead of honoring the delete -
**this effectively "undeletes" the contact in Google** by recreating/
updating it. Confirm this is the desired outcome; it's the same
timestamp-wins rule applied to a delete-vs-edit conflict, see scenario 10.

**Verify:** Person shows up in the recycle bin (soft-deleted), not gone from
the DB. `deleted_at` set. It is not re-created on the next pull (soft-delete
plus tombstone means `person.ErrNotFound`/no-op path in `mergeRemoteRecord`
once genuinely gone, or the "local already gone, tombstone too" branch when
`Get` returns not-found and the tombstone is also true).

## 6. Delete contact in Contacts

**Setup:** An already-linked contact. Delete it in the app (soft-delete).

**Expected:** `RecordChanged` queues `Job{Kind: Deleted}`. Next tick:
`syncLocalJob` sees `Kind == Deleted` → calls `DeleteRecord` (Google
`deleteContact`) directly, skipping the record-diff path entirely.

**Verify:** Contact is gone from Google Contacts. Local Person remains in
the recycle bin per the normal 30-day soft-delete/purge policy - purge later
should not attempt to delete it from Google again in a way that errors (see
the tombstone-for-unknown-contact handling: deleting an already-gone remote
record is a no-op, `DeleteRecord` on an empty/blank remote id also no-ops).

## 7. New contact added in both locations - what decides "same person"?

**FIXED (2026-09-10):** A new contact with no `contacts_local_id` tag now
first tries an **exact first+last name match** against existing,
not-yet-linked local contacts (`findUnlinkedMatch`/`FindByExactName`) before
creating a duplicate. Exactly one match links the two; zero or more than one
match (ambiguous) falls back to creating a new Person, same as before. This
is intentionally a plain exact string match (case-sensitive, no fuzzy/
similarity logic) - the same precedent already used for resolving Google's
free-text relationship names.

**Setup:** Add "Jane Doe" in the app. Separately (before any sync
reconciles them, or on an account not yet connected when the local one was
created) add a contact also named "Jane Doe" directly in Google Contacts,
with no relationship to the local one.

**Expected:** The next pull matches the incoming "Jane Doe" to the existing
local one instead of creating a duplicate (`reconcileExisting` runs exactly
as if `contacts_local_id` had already resolved to it), then applies the same
last-write-wins comparison as any other already-linked contact. Result:
**exactly one Jane Doe** in the app and exactly one in Google, now linked.

**Not fixed / known limitation:** matching is strictly exact ("Jane Doe" vs
"jane doe" or "Jane  Doe" with different whitespace won't match) and only
considers first+last name, not nicknames/middle names/emails. An ambiguous
case (two existing unlinked local contacts with the identical first+last
name) still falls back to creating a duplicate rather than guessing.

**Verify:** `sync_record_links` has exactly one row for the pre-existing
Person's ID; the Google contact carries `contacts_local_id` pointing at it;
no second Person or second Google contact exists.

## 8. Complementary modifications to the same contact

**Setup:** An already-linked contact. In the same sync window (before the
next pull runs), change *different, non-overlapping* fields on each side -
e.g. add a phone number in Google, and separately add a note in the app
(where Google's copy of that contact has never had a note/biography at
all).

**FIXED for this specific, common case (2026-09-10):** `fieldAwareUpdate`
only applies a field from the winning (Google) side when Google's payload
actually reported it (`FieldState.IsSet`). Since Google's copy never had a
biography, the `notes` field is left alone even though Google "wins" the
record-level comparison - so the local note survives *and* the Google-added
phone number applies. Verify both: `after.PhoneNumbers` has the new number,
`after.Notes` still has the locally-added note.

**Not fixed / still a real limitation:** if Google's copy of a field *does*
have a value (even a stale/unrelated one) and local changed that *same*
field to something else, this is a genuine same-field conflict, not a
complementary edit - see scenario 9. There is still no per-field
modification timestamp on either side (Postgres has one `updated_at` per
row; Google's API has one `metadata.updateTime` per contact), so this case
cannot be merged without a real 3-way baseline (remembering each field's
value as of the last successful sync, to tell "local changed it" apart from
"local's copy just happens to differ from Google's default") - that would
require new schema (e.g. a synced-field snapshot on `sync_record_links`) and
is a bigger follow-up, not attempted here.

**Verify:** `after.PhoneNumbers` contains the Google-added number; `after.Notes`
still equals the locally-added note (not wiped/emptied).

## 9. Conflicting modifications to the same contact

**Setup:** Same field changed differently on both sides within the same
window - e.g. app: last name → "Smith"; Google (nearly simultaneously):
last name → "Smyth".

**Expected:** Identical mechanism to scenario 8 (whole-record LWW by
timestamp) - there's no field-level "same field, most-recent-wins" logic
beyond the fact that the whole record's timestamp already determines who
wins. Whichever side's Google `metadata.updateTime` vs. local `updated_at`
comparison is later takes the entire record, including the conflicting
value. There's no logged conflict, no user-facing warning, and no "resolve
deterministically and log for review" (which the design doc calls for) -
it's silent.

**Verify:** Confirm the final value matches whichever side actually has the
later timestamp, and confirm nothing is logged/surfaced about the conflict
having occurred (current gap vs. design intent, worth deciding to fix or
accept).

## 10. Contact deleted in Google but modified in Contacts

**Setup:** Already-linked contact. Delete it in Google. Before the next
pull, edit it in the app (bumping `local.UpdatedAt` to after Google's delete
timestamp).

**CONFIRMED AND FIXED (2026-09-10):** This scenario's suspected bug was real.
`mergeRemoteRecord`'s tombstone branch used to push the local record back to
Google using the old (now-deleted) `ExternalID` whenever the local edit was
newer, which 404s and aborted the *entire account's* sync run - not just
this one record (confirmed via
`TestScenario_DeletedInGoogleButEditedLocally` and the faster companion unit
test `TestMergeRemoteRecordRecreatesAfterTombstonedResourceNameConflict`).
Fixed: on a 404 from that push, the adapter now retries with a blank
`ExternalID`, recreating the contact under a brand new resourceName and
updating `sync_record_links` accordingly - the same recovery already used
for an *unmapped* tombstone (`TestMergeRemoteRecordSkipsUnknownTombstone`).

**Verify:** The local edit survives, the contact reappears in Google under a
new resourceName, and the account's sync status stays `connected` (not
`error`) after the run.

## 11. Contact deleted in Contacts but modified in Google

**Setup:** Already-linked contact. Delete it locally (soft-delete, queues a
`Deleted` job whose `syncLocalJob` immediately calls Google `deleteContact`
on the next tick). Before that tick runs, edit the contact in Google.

**Expected:** This is a race between "which job/pull runs first," not a
timestamp comparison:
- If the local `Deleted` job's tick runs `syncLocalJob` *before* the pull
  that would have seen Google's edit, the Google contact is deleted outright
  - Google's edit is simply lost along with the contact, no comparison
    happens at all (there's no tombstone/edit-timestamp check on the
    outgoing-delete path; `syncLocalJob` for `Kind == Deleted` always calls
    `DeleteRecord` unconditionally).
- If a pull runs first and observes the Google-side edit while the local
  Person is already soft-deleted, `mergeRemoteRecord`'s "local not found /
  soft-deleted" handling applies: if the remote isn't itself a tombstone, it
  is treated as brand new (no `contacts_local_id` significance survives a
  hard-delete's cascade, but a *soft*-deleted local person is still found by
  `people.Get`... check this: `people.Get` presumably excludes soft-deleted
  rows and returns `ErrNotFound`, at which point the code checks
  `remote.Record.Tombstone.Deleted` - false here since Google's copy was
  merely edited, not deleted - so it falls through to creating a **new**
  local Person from the Google edit, effectively "reviving" the contact as a
  separate record instead of respecting the local delete.

**Verify:** Both outcomes are plausible depending on tick ordering - run this
scenario multiple times / with deliberately staggered timing to observe
both. If the "revives as a new Person" path is confirmed, decide if that's
acceptable (arguably reasonable - the user explicitly kept editing it in
Google) or should instead respect the local delete and skip re-creating it.

---

## Multi-account scenarios (2+ Google accounts on one Contacts account)

12. **Connecting a second account when local contacts already exist.** The
    first-ever sync for the new account does an `exportLocal` of *every*
    existing local Person (not just ones already linked to account 1) →
    expect brand-new Google contacts to appear in account 2 for every local
    Person, each independently linked via its own `sync_record_links` row
    and its own `contacts_local_id` tag on account 2's copy. Confirm this
    doesn't collide with or duplicate anything already linked to account 1 -
    the two Google-side copies are genuinely separate contacts owned by
    different Google accounts, both mapped to the same one local Person.

13. **Editing the same Person's data differently in each of two Google
    accounts.** Each account's pull is processed independently
    (`mergeRemoteRecord` runs per-account against the same shared
    `local.UpdatedAt`). Whichever account's sync tick happens to process
    first "wins" that tick (its `remoteUpdatedAt` compared, local updated);
    the other account's pull, whenever it runs, then compares *its* remote
    timestamp against the now-newer `local.UpdatedAt` from the first
    account's applied change - likely local wins that round and gets pushed
    back out to account 2, overwriting whatever was edited there. Net
    effect: the account processed second effectively "loses," but not
    necessarily the account whose Google-side edit was chronologically
    later - it depends entirely on scheduler ordering between the two
    accounts' due intervals. Worth confirming this is the actual observed
    behavior, since it's a subtle three-way race (account A, account B,
    local DB) rather than the simple two-way race in scenarios 8/9.

14. **Deleting a Person locally with 2 accounts connected.** Confirmed from
    `Runner.runJob`: a single sync `Job` row is fanned out to *every*
    connected account owned by that user in one pass (not one job per
    account), so a delete should call `DeleteRecord` against both accounts'
    linked remote contacts within the same tick. Note the failure mode this
    implies: if account A's `DeleteRecord` call errors, the loop returns
    immediately and account B is **not** processed in that attempt, and the
    job is marked failed (retried later) rather than partially completed -
    confirm a subsequent retry re-runs against *both* accounts again
    (including the one that already succeeded) without erroring on an
    already-deleted remote contact (should be a safe no-op given
    `DeleteRecord`'s blank/empty-id guard, but the "already deleted, delete
    again" case specifically is worth a live check).

15. **Disconnecting one account.** `DELETE /api/v1/sync-accounts/{id}`
    cascade-deletes that account's `sync_record_links` rows. Confirm this
    does **not** delete the local Person data or push any delete to the
    *other* still-connected account - the contact should remain, still
    mirrored on the remaining account(s).

16. **Reconnecting the same Google account (same `provider_account_id`)
    after disconnecting.** Confirm it's treated as the same account
    resuming (matched by `provider_account_id`, not creating a duplicate
    `sync_accounts` row) and whether `sync_record_links` needs to be
    rebuilt from scratch (since it cascade-deleted on disconnect) - expect a
    full re-import/re-export pass since `SyncCursor` would need to reset too
    (confirm whether disconnect clears the cursor).

---

## Other scenarios worth covering (not in the original list)

17. **Incremental sync token expiry (HTTP 410 Gone).** `pullRemote` catches
    this and restarts a full pull from an empty cursor. Verify this doesn't
    re-create local duplicates for contacts already linked (it shouldn't,
    since `contacts_local_id` still matches) and does correctly pick up
    anything genuinely new.

18. **Reauthorization required (revoked/expired refresh token).**
    `markAccountFailed` only sets `reconnect_required` for genuine
    auth failures (401, missing/invalid token) - verify a transient error
    (e.g. simulated 500 from Google, or a single malformed record) leaves
    the account in plain `error` status and still `connected`-eligible on
    the next tick, not forced into reconnect.

19. **Unknown tombstone (delete-before-ever-synced).** Already covered by
    `TestMergeRemoteRecordSkipsUnknownTombstone` at the unit level - worth
    one live confirmation that a contact deleted in Google before the
    account was ever connected doesn't error the whole sync run.

20. **Potential push/pull timestamp oscillation (flag for live testing,
    not yet confirmed empirically).** `_google_updated_at` is derived from
    Google's own `metadata.sources[].updateTime`, which Google updates
    server-side on *every* write, including writes that originated from us
    (e.g. scenario 4's push). Sequence to watch for:
    1. Local edit at `T0` (`local.UpdatedAt = T0`); pushed to Google at
       `T1 > T0`, so Google's own `updateTime` becomes ~`T1`.
    2. Next pull (`T2 > T1`) reads Google's `updateTime` (`T1`) and compares
       it to `local.UpdatedAt` (`T0`). Since `T1 > T0`, the code treats
       Google as "newer" and re-applies the (identical) field values via a
       local `Update` call - which bumps `local.UpdatedAt` to `T2`.
    3. The next scheduled export/compare cycle now sees `local.UpdatedAt =
       T2 > T1` and pushes again, advancing Google's `updateTime` again, and
       the cycle repeats indefinitely - with no actual content change, just
       continuous churn (extra API calls, constantly advancing
       `updated_at`, and a relationship reconciliation re-run every cycle).
    This is inferred from reading the comparison logic, not yet observed
    live - a good candidate for an explicit test: sync a contact, make no
    further edits on either side, and watch several sync cycles to confirm
    `updated_at`/Google's `updateTime` stabilize rather than keep advancing.

21. **Relationships two-pass + symmetric dedup (recently added).** Add two
    Google contacts in the same pull batch that reference each other (A:
    relation "child: B", B: relation "parent: A") where neither existed
    locally before. Confirm: (a) both resolve to *linked* relationships (not
    name-only) even though B didn't exist yet when A was first processed -
    this is what the two-pass reconciliation fixes; (b) exactly **one**
    relationship row is stored, not two (the symmetric dedup via
    `ListIncomingRelationships`/`relationshipAlreadyExists`) - check
    `person_relationships` directly if needed to confirm there isn't a
    second, redundant row.

22. **Reserved custom-field protection.** Confirm `google_resource_name`
    (legacy), `_google_updated_at`, and `contacts_local_id` never appear as
    editable custom fields in the UI for a synced contact, and that editing
    other custom fields locally survives a round-trip through Google
    (Google doesn't understand arbitrary custom fields, so anything beyond
    the mapped fields is expected to NOT sync - confirm this limitation is
    acceptable/expected, not a bug).
