//go:build integration

package google

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/scottfridlund/contacts/backend/internal/authn"
	"github.com/scottfridlund/contacts/backend/internal/contactsync"
	"github.com/scottfridlund/contacts/backend/internal/person"
)

// These tests run only with `-tags=integration` and require TEST_DATABASE_URL
// pointing at a migrated PostgreSQL instance. They exercise the full
// Adapter.Sync() flow (no unit-level shortcuts) against a real Postgres-backed
// person.Service and a fake in-memory Google People API
// (fake_server_test.go), so no real network access or Google credentials are
// needed. See docs/testing/google-sync-test-scenarios.md for the scenario
// list these tests are built from.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration tests")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// createTestUser inserts a real users row (persons.owner_id and
// sync_accounts.owner_id have a foreign key to it) and registers its
// cleanup - called before any per-test persons/accounts are created, so
// (thanks to t.Cleanup's LIFO order) the user row is deleted last, after
// everything referencing it.
func createTestUser(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	subject := "google-sync-scenario-" + uuid.New().String()
	var id uuid.UUID
	err := pool.QueryRow(ctx, `INSERT INTO users (subject, email, display_name) VALUES ($1, $1 || '@example.com', $1) RETURNING id`, subject).Scan(&id)
	if err != nil {
		t.Fatalf("creating test user: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", id) })
	return id
}

// scenario bundles everything one test needs: a real Postgres-backed person
// service, the real sync_accounts/sync_record_links repositories, a fake
// Google server, and an Adapter wired directly to it (bypassing NewAdapter's
// real OIDC discovery call, which needs real network access). The account
// is a real persisted row (not just an in-memory struct), since
// sync_record_links.sync_account_id has a foreign key to sync_accounts(id) -
// linking a contact to a made-up account ID silently no-ops.
//
// Every operation runs under a context scoped to a dedicated test user
// (authn.WithUserID), the same way a real request is scoped to the logged-in
// account - without this, exportLocal's unscoped List() would see every
// Person left in the shared test database by every other test, not just
// this one.
type scenario struct {
	t        *testing.T
	ctx      context.Context
	pool     *pgxpool.Pool
	people   *person.Service
	links    *contactsync.RecordLinkRepository
	server   *fakeGoogleServer
	adapter  *Adapter
	accounts *contactsync.Repository
	account  contactsync.Account
}

func newScenario(t *testing.T) *scenario {
	t.Helper()
	pool := newTestPool(t)
	ownerID := createTestUser(t, pool)
	ctx := authn.WithUserID(context.Background(), ownerID)

	server := newFakeGoogleServer()
	t.Cleanup(server.Close)

	accounts := contactsync.NewRepository(pool)
	links := contactsync.NewRecordLinkRepository(pool)
	people := person.NewService(person.NewRepository(pool))

	adapter := &Adapter{
		cfg:      Config{PeopleBaseURL: server.URL},
		http:     http.DefaultClient,
		accounts: accounts,
		people:   people,
		links:    links,
		logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}

	token := "test-access-token"
	created, err := accounts.Create(ctx, &contactsync.Account{
		Provider:             "google",
		ProviderAccountID:    fmt.Sprintf("test-subject-%s", uuid.New()),
		AccessToken:          &token,
		SyncFrequencyMinutes: 5,
		Status:               "connected",
	})
	if err != nil {
		t.Fatalf("creating sync account: %v", err)
	}
	t.Cleanup(func() {
		// sync_record_links rows cascade-delete with the account.
		_, _ = pool.Exec(context.Background(), "DELETE FROM sync_accounts WHERE id = $1", created.ID)
	})

	return &scenario{
		t:        t,
		ctx:      ctx,
		pool:     pool,
		people:   people,
		links:    links,
		server:   server,
		adapter:  adapter,
		accounts: accounts,
		account:  *created,
	}
}

// sync runs one Adapter.Sync() call and reloads the account afterward so
// SyncCursor/Status/etc. thread forward for the next call, the way the real
// scheduler would across ticks.
func (sc *scenario) sync(job contactsync.Job) error {
	sc.t.Helper()
	err := sc.adapter.Sync(sc.ctx, sc.account, job)
	if refreshed, getErr := sc.accounts.GetByID(sc.ctx, sc.account.ID); getErr == nil {
		sc.account = *refreshed
	}
	return err
}

// createLocal creates a local Person directly (bypassing sync), for
// scenarios that start with a local-only contact.
func (sc *scenario) createLocal(first, last string) *person.Person {
	sc.t.Helper()
	p, verrs, err := sc.people.Create(sc.ctx, person.CreateInput{FirstName: first, LastName: last})
	if err != nil || verrs.HasErrors() {
		sc.t.Fatalf("createLocal: verrs=%v err=%v", verrs, err)
	}
	sc.t.Cleanup(func() {
		_, _ = sc.pool.Exec(context.Background(), "DELETE FROM persons WHERE id = $1", p.ID)
	})
	return p
}

// linked seeds a contact directly in the fake Google server (as if it
// already existed there) and runs one sync so it gets imported and linked -
// the common starting point ("already-synced contact") for scenarios 3-11.
func (sc *scenario) linked(first, last string) (*person.Person, string) {
	sc.t.Helper()
	resourceName := sc.server.seed(googlePerson{
		Names: []googleName{{GivenName: first, FamilyName: last}},
	})
	if err := sc.sync(contactsync.Job{}); err != nil {
		sc.t.Fatalf("initial sync: %v", err)
	}
	people, _, err := sc.people.List(sc.ctx, person.ListParams{Page: 1, PageSize: 100, SortField: "created_at"})
	if err != nil {
		sc.t.Fatalf("listing persons: %v", err)
	}
	for i := range people {
		if people[i].FirstName == first && people[i].LastName == last {
			p := people[i]
			sc.t.Cleanup(func() {
				_, _ = sc.pool.Exec(context.Background(), "DELETE FROM persons WHERE id = $1", p.ID)
			})
			return &p, resourceName
		}
	}
	sc.t.Fatalf("expected %s %s to be imported", first, last)
	return nil, ""
}

func findPersonByName(t *testing.T, people []person.Person, first, last string) (person.Person, bool) {
	t.Helper()
	for _, p := range people {
		if p.FirstName == first && p.LastName == last {
			return p, true
		}
	}
	return person.Person{}, false
}

// --- Scenario 1: new contact added in Google ---

func TestScenario_NewContactInGoogle(t *testing.T) {
	sc := newScenario(t)
	resourceName := sc.server.seed(googlePerson{
		Names: []googleName{{GivenName: "Grace", FamilyName: "Hopper"}},
	})

	if err := sc.sync(contactsync.Job{}); err != nil {
		t.Fatalf("sync: %v", err)
	}

	people, _, err := sc.people.List(sc.ctx, person.ListParams{Page: 1, PageSize: 100, SortField: "created_at"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	imported, ok := findPersonByName(t, people, "Grace", "Hopper")
	if !ok {
		t.Fatalf("expected Grace Hopper to be imported from Google, got %+v", people)
	}
	t.Cleanup(func() { _, _ = sc.pool.Exec(context.Background(), "DELETE FROM persons WHERE id = $1", imported.ID) })

	link, err := sc.links.Get(sc.ctx, sc.account.ID, imported.ID)
	if err != nil || link.RemoteID != resourceName {
		t.Fatalf("expected sync_record_links row for %s -> %s, got %+v err=%v", imported.ID, resourceName, link, err)
	}

	remote, ok := sc.server.get(resourceName)
	if !ok {
		t.Fatal("expected the Google contact to still exist")
	}
	found := false
	for _, ud := range remote.UserDefined {
		if ud.Key == localIDUserDefinedKey && ud.Value == imported.ID.String() {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Google contact to be tagged back with contacts_local_id=%s, got %+v", imported.ID, remote.UserDefined)
	}
}

// TestPullRemoteDoesNotDuplicateOnRetryAfterAPartialFailure guards a real
// production incident: syncing an account with many contacts, one record's
// write-back to Google kept failing (a persistent error outliving a.do's
// own retries). The old pullRemote aborted the whole pull on that single
// failure and rolled the cursor back to before the run started, so the
// next sync attempt replayed the entire batch - including contacts that had
// already been successfully created and linked - and duplicated them,
// since findUnlinkedMatch won't reuse a match already linked to this same
// account (by design, to avoid cross-linking a different resourceName onto
// it) and so treats the replay as brand new.
func TestPullRemoteDoesNotDuplicateOnRetryAfterAPartialFailure(t *testing.T) {
	sc := newScenario(t)
	sc.server.seed(googlePerson{Names: []googleName{{GivenName: "Jason", FamilyName: "Cummings"}}})
	badName := sc.server.seed(googlePerson{Names: []googleName{{GivenName: "Bad", FamilyName: "Record"}}})
	sc.server.failNextUpdate(badName, 20)

	if err := sc.sync(contactsync.Job{}); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if sc.account.SyncCursor == "" {
		t.Fatal("expected the sync cursor to advance despite the other record's persistent failure")
	}

	people, _, err := sc.people.List(sc.ctx, person.ListParams{Page: 1, PageSize: 100, SortField: "created_at"})
	if err != nil {
		t.Fatalf("list after first sync: %v", err)
	}
	jason, ok := findPersonByName(t, people, "Jason", "Cummings")
	if !ok {
		t.Fatalf("expected Jason Cummings to be imported despite the other record's failure, got %+v", people)
	}
	t.Cleanup(func() { _, _ = sc.pool.Exec(context.Background(), "DELETE FROM persons WHERE id = $1", jason.ID) })
	if bad, ok := findPersonByName(t, people, "Bad", "Record"); ok {
		t.Cleanup(func() { _, _ = sc.pool.Exec(context.Background(), "DELETE FROM persons WHERE id = $1", bad.ID) })
	}

	// A second sync attempt (still failing the same record) must not
	// re-create Jason Cummings.
	sc.server.failNextUpdate(badName, 20)
	if err := sc.sync(contactsync.Job{}); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	people, _, err = sc.people.List(sc.ctx, person.ListParams{Page: 1, PageSize: 100, SortField: "created_at"})
	if err != nil {
		t.Fatalf("list after second sync: %v", err)
	}
	count := 0
	for _, p := range people {
		if p.FirstName == "Jason" && p.LastName == "Cummings" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 Jason Cummings after a second sync, got %d: %+v", count, people)
	}
}

// --- Scenario 2: new contact added in Contacts ---

func TestScenario_NewContactInContacts(t *testing.T) {
	sc := newScenario(t)
	local := sc.createLocal("Ada", "Lovelace")

	job := contactsync.Job{PersonID: local.ID, Kind: contactsync.ChangeKindCreated, Snapshot: local.Snapshot(nil)}
	if err := sc.sync(job); err != nil {
		t.Fatalf("sync: %v", err)
	}

	all := sc.server.all()
	if len(all) != 1 {
		t.Fatalf("expected exactly 1 Google contact (no duplicate from the same-tick pull), got %d: %+v", len(all), all)
	}
	if all[0].Names[0].GivenName != "Ada" || all[0].Names[0].FamilyName != "Lovelace" {
		t.Errorf("unexpected exported contact: %+v", all[0])
	}
}

// --- Scenario 3: modify contact in Google ---

func TestScenario_ModifyInGoogle(t *testing.T) {
	sc := newScenario(t)
	_, resourceName := sc.linked("Marie", "Curie")

	sc.server.mutate(resourceName, time.Now().Add(time.Hour), func(p *googlePerson) {
		p.PhoneNumbers = []googlePhoneNumber{{Value: "+1-555-0100", Type: "mobile"}}
	})

	if err := sc.sync(contactsync.Job{}); err != nil {
		t.Fatalf("sync: %v", err)
	}

	people, _, err := sc.people.List(sc.ctx, person.ListParams{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	updated, ok := findPersonByName(t, people, "Marie", "Curie")
	if !ok {
		t.Fatal("expected Marie Curie to still exist")
	}
	if len(updated.PhoneNumbers) != 1 || updated.PhoneNumbers[0].Value != "+1-555-0100" {
		t.Errorf("expected the Google-side phone number to be applied locally, got %+v", updated.PhoneNumbers)
	}
}

// --- Scenario 4: modify contact in Contacts ---

func TestScenario_ModifyInContacts(t *testing.T) {
	sc := newScenario(t)
	local, resourceName := sc.linked("Alan", "Turing")

	notes := "Met at a conference."
	updated, verrs, err := sc.people.Update(sc.ctx, local.ID, person.UpdateInput{Notes: &notes, NotesSet: true})
	if err != nil || verrs.HasErrors() {
		t.Fatalf("update: verrs=%v err=%v", verrs, err)
	}

	job := contactsync.Job{PersonID: local.ID, Kind: contactsync.ChangeKindUpdated, Snapshot: updated.Snapshot(nil)}
	if err := sc.sync(job); err != nil {
		t.Fatalf("sync: %v", err)
	}

	remote, ok := sc.server.get(resourceName)
	if !ok {
		t.Fatal("expected the Google contact to still exist")
	}
	if len(remote.Biographies) != 1 || remote.Biographies[0].Value != notes {
		t.Errorf("expected the local note to be pushed to Google, got %+v", remote.Biographies)
	}
}

// TestUpsertRecordPreservesExistingUserDefinedFields guards a real data-loss
// bug: pushing a local edit to Google used to always overwrite the entire
// userDefined field with just our own reserved contacts_local_id entry,
// silently wiping out any custom field the user had set up directly in
// Google Contacts (our own app-level custom_fields are a separate, still
// unsynced concept - see docs/design/03-implementation-decisions.md).
func TestUpsertRecordPreservesExistingUserDefinedFields(t *testing.T) {
	sc := newScenario(t)
	local, resourceName := sc.linked("Katherine", "Johnson")

	// Simulate the user adding their own custom field directly in Google
	// Contacts, after this contact was already linked to our system.
	sc.server.mutate(resourceName, time.Now(), func(p *googlePerson) {
		p.UserDefined = append(p.UserDefined, googleUserDefined{Key: "anniversary", Value: "2020-01-01"})
	})

	notes := "Pushing a local edit."
	updated, verrs, err := sc.people.Update(sc.ctx, local.ID, person.UpdateInput{Notes: &notes, NotesSet: true})
	if err != nil || verrs.HasErrors() {
		t.Fatalf("update: verrs=%v err=%v", verrs, err)
	}
	job := contactsync.Job{PersonID: local.ID, Kind: contactsync.ChangeKindUpdated, Snapshot: updated.Snapshot(nil)}
	if err := sc.sync(job); err != nil {
		t.Fatalf("sync: %v", err)
	}

	remote, ok := sc.server.get(resourceName)
	if !ok {
		t.Fatal("expected the Google contact to still exist")
	}
	found := false
	for _, ud := range remote.UserDefined {
		if ud.Key == "anniversary" && ud.Value == "2020-01-01" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the user's own Google-side custom field to survive our update, got %+v", remote.UserDefined)
	}
}

// --- Scenario 5: delete contact in Google ---

func TestScenario_DeleteInGoogle(t *testing.T) {
	sc := newScenario(t)
	local, resourceName := sc.linked("Rosalind", "Franklin")

	sc.server.delete(resourceName, time.Now().Add(time.Hour))

	if err := sc.sync(contactsync.Job{}); err != nil {
		t.Fatalf("sync: %v", err)
	}

	var deletedAt *time.Time
	if err := sc.pool.QueryRow(context.Background(), "SELECT deleted_at FROM persons WHERE id = $1", local.ID).Scan(&deletedAt); err != nil {
		t.Fatalf("query: %v", err)
	}
	if deletedAt == nil {
		t.Error("expected the Google-side delete to soft-delete the local Person")
	}
}

// --- Scenario 6: delete contact in Contacts ---

func TestScenario_DeleteInContacts(t *testing.T) {
	sc := newScenario(t)
	local, resourceName := sc.linked("Katherine", "Johnson")

	if err := sc.people.Delete(sc.ctx, local.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	job := contactsync.Job{PersonID: local.ID, Kind: contactsync.ChangeKindDeleted, Snapshot: local.Snapshot(nil)}
	if err := sc.sync(job); err != nil {
		t.Fatalf("sync: %v", err)
	}

	remote, ok := sc.server.get(resourceName)
	if !ok || !remote.Metadata.Deleted {
		t.Errorf("expected the Google contact to be deleted, got ok=%v remote=%+v", ok, remote)
	}
}

// --- Scenario 7: new contact added in both locations (matched by exact name) ---

// TestScenario_NewInBothLocationsMatchesByExactName guards the fix: a
// contact that already exists on both sides (added independently, before
// either was ever linked) is matched by an exact first+last name match
// instead of creating a duplicate.
func TestScenario_NewInBothLocationsMatchesByExactName(t *testing.T) {
	sc := newScenario(t)
	local := sc.createLocal("Jane", "Doe")
	resourceName := sc.server.seed(googlePerson{Names: []googleName{{GivenName: "Jane", FamilyName: "Doe"}}})

	// One full sync pass: pulls the seeded Google contact, matches it to the
	// pre-existing local Person by exact name instead of creating a second
	// one, then (since this is the account's first-ever sync) exports every
	// local Person - which now finds the just-linked one already has a
	// remote id, so it's updated in place rather than duplicated.
	if err := sc.sync(contactsync.Job{}); err != nil {
		t.Fatalf("sync: %v", err)
	}

	people, _, err := sc.people.List(sc.ctx, person.ListParams{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	janeCount := 0
	for _, p := range people {
		if p.FirstName == "Jane" && p.LastName == "Doe" {
			janeCount++
			if p.ID != local.ID {
				t.Errorf("expected the matched Person to be the pre-existing one (%s), got a different id %s", local.ID, p.ID)
			}
		}
	}
	if janeCount != 1 {
		t.Errorf("expected exact-name matching to dedup to 1 local Jane Doe, got %d", janeCount)
	}

	remoteCount := 0
	for _, p := range sc.server.all() {
		if len(p.Names) > 0 && p.Names[0].GivenName == "Jane" && p.Names[0].FamilyName == "Doe" {
			remoteCount++
		}
	}
	if remoteCount != 1 {
		t.Errorf("expected exact-name matching to dedup to 1 Google Jane Doe, got %d", remoteCount)
	}

	link, err := sc.links.Get(sc.ctx, sc.account.ID, local.ID)
	if err != nil || link.RemoteID != resourceName {
		t.Errorf("expected the pre-existing Person to be linked to the seeded contact %s, got %+v err=%v", resourceName, link, err)
	}
}

// --- Scenario 8: complementary modifications are merged per field ---

// TestScenario_ComplementaryEditsAreMergedPerField guards the fix: a field
// Google's own payload never reported (IsSet false) must not wipe out a
// local edit to that same field just because the record as a whole is
// newer on Google's side. Fields Google *did* report still apply normally.
func TestScenario_ComplementaryEditsAreMergedPerField(t *testing.T) {
	sc := newScenario(t)
	local, resourceName := sc.linked("Hedy", "Lamarr")

	notes := "Added locally."
	locallyUpdated, verrs, err := sc.people.Update(sc.ctx, local.ID, person.UpdateInput{Notes: &notes, NotesSet: true})
	if err != nil || verrs.HasErrors() {
		t.Fatalf("local update: verrs=%v err=%v", verrs, err)
	}
	if locallyUpdated.Notes == nil || *locallyUpdated.Notes != notes {
		t.Fatalf("local note wasn't saved before the sync even ran")
	}

	// Complementary Google-side edit (a field Google never had a note on),
	// stamped clearly newer so it "wins" the whole-record comparison.
	sc.server.mutate(resourceName, time.Now().Add(time.Hour), func(p *googlePerson) {
		p.PhoneNumbers = []googlePhoneNumber{{Value: "+1-555-0199", Type: "work"}}
	})

	if err := sc.sync(contactsync.Job{}); err != nil {
		t.Fatalf("sync: %v", err)
	}

	after, err := sc.people.Get(sc.ctx, local.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(after.PhoneNumbers) != 1 || after.PhoneNumbers[0].Value != "+1-555-0199" {
		t.Errorf("expected the Google-side phone number to apply, got %+v", after.PhoneNumbers)
	}
	if after.Notes == nil || *after.Notes != notes {
		t.Errorf("expected the local note to survive the merge (Google never set notes), got %v", after.Notes)
	}
}

// --- Scenario 9: conflicting modifications to the same field ---

func TestScenario_ConflictingEditsWholeRecordLastWriteWins(t *testing.T) {
	sc := newScenario(t)
	local, resourceName := sc.linked("Grace", "Hopper")

	newLocalLast := "Hopperfield"
	if _, verrs, err := sc.people.Update(sc.ctx, local.ID, person.UpdateInput{LastName: &newLocalLast, LastNameSet: true}); err != nil || verrs.HasErrors() {
		t.Fatalf("local update: verrs=%v err=%v", verrs, err)
	}

	sc.server.mutate(resourceName, time.Now().Add(time.Hour), func(p *googlePerson) {
		p.Names[0].FamilyName = "Hoppersmith"
	})

	if err := sc.sync(contactsync.Job{}); err != nil {
		t.Fatalf("sync: %v", err)
	}

	after, err := sc.people.Get(sc.ctx, local.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.LastName != "Hoppersmith" {
		t.Errorf("expected the newer (Google-side) edit to win the conflict, got last_name=%q", after.LastName)
	}
}

// --- Scenario 10: deleted in Google but modified locally ---

// TestScenario_DeletedInGoogleButEditedLocally guards a real bug found via
// this scenario (see docs/testing/google-sync-test-scenarios.md, scenario
// 10): a newer local edit used to be pushed back to the resourceName Google
// had already tombstoned, which 404s and used to abort the whole account's
// sync run. mergeRemoteRecord now recreates the contact instead, the same
// way an *unmapped* tombstone is already handled
// (TestMergeRemoteRecordSkipsUnknownTombstone).
func TestScenario_DeletedInGoogleButEditedLocally(t *testing.T) {
	sc := newScenario(t)
	local, resourceName := sc.linked("Barbara", "Liskov")

	// Google-side delete, deliberately stamped OLDER than the local edit
	// below so the local edit "wins" the tombstone-vs-edit comparison.
	sc.server.delete(resourceName, time.Now().Add(-time.Hour))

	newNickname := "Barb"
	if _, verrs, err := sc.people.Update(sc.ctx, local.ID, person.UpdateInput{Nickname: &newNickname, NicknameSet: true}); err != nil || verrs.HasErrors() {
		t.Fatalf("local update: verrs=%v err=%v", verrs, err)
	}

	if err := sc.sync(contactsync.Job{}); err != nil {
		t.Fatalf("sync failed (the tombstoned-resourceName 404 regressed): %v", err)
	}

	after, err := sc.people.Get(sc.ctx, local.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.Nickname == nil || *after.Nickname != newNickname {
		t.Errorf("expected the local edit to survive, got nickname=%v", after.Nickname)
	}

	link, err := sc.links.Get(sc.ctx, sc.account.ID, local.ID)
	if err != nil {
		t.Fatalf("expected a new remote link to be recorded, got: %v", err)
	}
	if link.RemoteID == "" || link.RemoteID == resourceName {
		t.Errorf("expected the contact to be recreated under a new resourceName, got %q (old was %q)", link.RemoteID, resourceName)
	}
	if _, ok := sc.server.get(link.RemoteID); !ok {
		t.Errorf("expected the new resourceName %q to exist in Google", link.RemoteID)
	}
}

// memLinkStore is a minimal in-memory recordLinkStore fake, keyed by
// (accountID, personID) - used to exercise mergeRemoteRecord directly
// without a real Postgres, for a faster companion to the full scenario test
// above.
type memLinkStore struct {
	links map[string]*contactsync.RecordLink
}

func newMemLinkStore() *memLinkStore {
	return &memLinkStore{links: map[string]*contactsync.RecordLink{}}
}

func linkKey(accountID, personID uuid.UUID) string {
	return accountID.String() + "|" + personID.String()
}

func (s *memLinkStore) Get(_ context.Context, accountID, personID uuid.UUID) (*contactsync.RecordLink, error) {
	link, ok := s.links[linkKey(accountID, personID)]
	if !ok {
		return nil, contactsync.ErrLinkNotFound
	}
	return link, nil
}

func (s *memLinkStore) Upsert(_ context.Context, link *contactsync.RecordLink) error {
	s.links[linkKey(link.SyncAccountID, link.PersonID)] = link
	return nil
}

// TestMergeRemoteRecordRecreatesAfterTombstonedResourceNameConflict is a
// faster, DB-free companion to TestScenario_DeletedInGoogleButEditedLocally,
// exercising mergeRemoteRecord directly against an in-memory personService
// fake instead of a real Postgres-backed one.
func TestMergeRemoteRecordRecreatesAfterTombstonedResourceNameConflict(t *testing.T) {
	server := newFakeGoogleServer()
	defer server.Close()

	localID := uuid.New()
	local := &person.Person{ID: localID, FirstName: "Barbara", LastName: "Liskov", UpdatedAt: time.Now()}
	people := &relPersonService{stubPersonService: stubPersonService{t: t}, getResult: local}
	links := newMemLinkStore()
	accountID := uuid.New()

	resourceName := server.seed(googlePerson{Names: []googleName{{GivenName: "Barbara", FamilyName: "Liskov"}}})
	oldTime := time.Now().Add(-time.Hour)
	server.delete(resourceName, oldTime)

	adapter := &Adapter{
		cfg:    Config{PeopleBaseURL: server.URL},
		http:   http.DefaultClient,
		people: people,
		links:  links,
		logger: slog.Default(),
	}

	remote := contactsync.ProviderRecord{
		Record: contactsync.Record{
			ExternalID: resourceName,
			Tombstone:  contactsync.Tombstone{Deleted: true, UpdatedAt: oldTime},
			Fields: map[string]contactsync.FieldState{
				"local_id":           {IsSet: true, Value: localID.String()},
				"_google_updated_at": {IsSet: true, Value: oldTime.UTC().Format(time.RFC3339Nano)},
			},
		},
	}

	var pending []pendingRelationship
	if err := adapter.mergeRemoteRecord(context.Background(), accountID, contactsync.AuthSession{}, remote, &pending); err != nil {
		t.Fatalf("mergeRemoteRecord: %v", err)
	}

	link, err := links.Get(context.Background(), accountID, localID)
	if err != nil {
		t.Fatalf("expected a new link to be recorded: %v", err)
	}
	if link.RemoteID == "" || link.RemoteID == resourceName {
		t.Errorf("expected a new resourceName, got %q (old was %q)", link.RemoteID, resourceName)
	}
	if _, ok := server.get(link.RemoteID); !ok {
		t.Errorf("expected the new resourceName %q to exist", link.RemoteID)
	}
}
