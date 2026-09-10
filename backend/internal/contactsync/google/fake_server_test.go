//go:build integration

package google

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// fakeGoogleServer is a minimal in-memory stand-in for the Google People API
// endpoints the adapter calls (list/get/create/update/delete contacts). It
// lets the full Adapter.Sync() flow be exercised in tests with no real
// network access or Google credentials.
//
// A monotonic version counter stands in for Google's opaque sync tokens: a
// list request with no token returns every non-deleted contact (a "full"
// sync); a list request with a token returns only contacts that changed at
// or after that version (an "incremental" sync), including tombstones for
// deletes - which is enough to drive every scenario in
// docs/testing/google-sync-test-scenarios.md without modeling the real sync
// token format.
type fakeGoogleServer struct {
	*httptest.Server

	mu             sync.Mutex
	contacts       map[string]*googlePerson
	versions       map[string]int
	nextID         int
	version        int
	failNext       map[string]int
	blockListGate  chan struct{}
	blockListReady chan struct{}

	// groups, groupsByName, and groupMembers stand in for the separate
	// contactGroups resource (Google's "Labels"): groupMembers tracks
	// membership out-of-band from the contact record itself, exactly like
	// real Google, since membership is only ever changed via
	// contactGroups.members.modify, never via people:createContact/
	// updateContact.
	groups       map[string]*googleContactGroup
	groupsByName map[string]string
	groupMembers map[string]map[string]bool
	nextGroupID  int
}

func newFakeGoogleServer() *fakeGoogleServer {
	s := &fakeGoogleServer{
		contacts:     map[string]*googlePerson{},
		versions:     map[string]int{},
		failNext:     map[string]int{},
		groups:       map[string]*googleContactGroup{},
		groupsByName: map[string]string{},
		groupMembers: map[string]map[string]bool{},
	}
	// Every real Google account already has a "starred" system group -
	// pre-seed it so tests can exercise the starred <-> is_favorite mapping.
	s.groups["contactGroups/starred"] = &googleContactGroup{ResourceName: "contactGroups/starred", Name: "starred", GroupType: "SYSTEM_CONTACT_GROUP"}
	s.groupMembers["contactGroups/starred"] = map[string]bool{}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// failNextUpdate makes the next n update requests for resourceName return a
// 500, simulating a persistent transient failure (e.g. a rate limit that
// outlived our own retries) so tests can exercise pullRemote's handling of a
// per-record merge failure without needing a real flaky network.
func (s *fakeGoogleServer) failNextUpdate(resourceName string, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNext[resourceName] = n
}

// blockNextList makes the very next list-changes request block until release
// is closed, closing ready once the request starts waiting - lets a test
// deterministically start a second Sync() call while the first is still
// mid-flight, without relying on real timing.
func (s *fakeGoogleServer) blockNextList(ready, release chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blockListGate = release
	s.blockListReady = ready
}

// seed directly inserts a contact as if it already existed in Google before
// the test started (e.g. a contact never synced from this app). Returns the
// assigned resourceName.
func (s *fakeGoogleServer) seed(p googlePerson) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.storeLocked(&p, time.Now().UTC())
}

// mutate applies fn to a stored contact (simulating an edit made directly in
// Google Contacts) and stamps it with the given "as of" time, so tests can
// deterministically control which side of a last-write-wins comparison
// wins without relying on real wall-clock timing.
func (s *fakeGoogleServer) mutate(resourceName string, at time.Time, fn func(*googlePerson)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.contacts[resourceName]
	if !ok {
		return
	}
	fn(p)
	s.storeLocked(p, at)
}

// setUpdateTime overrides the metadata timestamp Google reports for a
// contact without changing any field values.
func (s *fakeGoogleServer) setUpdateTime(resourceName string, at time.Time) {
	s.mutate(resourceName, at, func(*googlePerson) {})
}

// delete tombstones a contact as if it were deleted directly in Google
// Contacts, stamped with the given "as of" time.
func (s *fakeGoogleServer) delete(resourceName string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.contacts[resourceName]
	if !ok {
		return
	}
	p.Metadata.Deleted = true
	s.storeLocked(p, at)
}

// get returns the current stored state of a contact, for assertions.
func (s *fakeGoogleServer) get(resourceName string) (googlePerson, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.contacts[resourceName]
	if !ok {
		return googlePerson{}, false
	}
	return *p, true
}

// all returns every non-deleted contact currently stored, for assertions.
func (s *fakeGoogleServer) all() []googlePerson {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]googlePerson, 0, len(s.contacts))
	for _, p := range s.contacts {
		if !p.Metadata.Deleted {
			out = append(out, *p)
		}
	}
	return out
}

// addToGroup adds a contact to a user-created group by name (creating the
// group first if it doesn't exist yet), as if that membership already
// existed in Google before this sync ran.
func (s *fakeGoogleServer) addToGroup(resourceName, groupName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rn, ok := s.groupsByName[groupName]
	if !ok {
		s.nextGroupID++
		rn = fmt.Sprintf("contactGroups/g%d", s.nextGroupID)
		s.groups[rn] = &googleContactGroup{ResourceName: rn, Name: groupName, GroupType: "USER_CONTACT_GROUP"}
		s.groupsByName[groupName] = rn
		s.groupMembers[rn] = map[string]bool{}
	}
	s.groupMembers[rn][resourceName] = true
}

// removeFromGroup removes a contact from a named user-created group, if
// both the group and the membership exist.
func (s *fakeGoogleServer) removeFromGroup(resourceName, groupName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rn, ok := s.groupsByName[groupName]
	if !ok {
		return
	}
	delete(s.groupMembers[rn], resourceName)
}

// setStarred adds or removes a contact's membership in the pre-seeded
// "starred" system group directly, simulating a user starring/unstarring a
// contact in Google Contacts.
func (s *fakeGoogleServer) setStarred(resourceName string, starred bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if starred {
		s.groupMembers["contactGroups/starred"][resourceName] = true
	} else {
		delete(s.groupMembers["contactGroups/starred"], resourceName)
	}
}

// groupsFor returns the names of every group (including system groups like
// starred) the given contact currently belongs to, for test assertions.
func (s *fakeGoogleServer) groupsFor(resourceName string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var names []string
	for rn, members := range s.groupMembers {
		if members[resourceName] {
			names = append(names, s.groups[rn].Name)
		}
	}
	sort.Strings(names)
	return names
}

// storeLocked assigns a resourceName/etag if new, bumps the version, and
// stamps the contact's reported update time. Callers must hold s.mu.
func (s *fakeGoogleServer) storeLocked(p *googlePerson, updatedAt time.Time) string {
	s.version++
	if p.ResourceName == "" {
		s.nextID++
		p.ResourceName = fmt.Sprintf("people/c%d", s.nextID)
	}
	p.ETag = fmt.Sprintf("etag-%d", s.version)
	p.Metadata.Sources = []googleSource{{UpdateTime: updatedAt.UTC().Format(time.RFC3339Nano)}}
	s.contacts[p.ResourceName] = p
	s.versions[p.ResourceName] = s.version
	return p.ResourceName
}

func (s *fakeGoogleServer) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := r.URL.Path
	switch {
	case path == "/people/me/connections":
		s.handleList(w, r)
	case path == "/people:createContact":
		s.handleCreate(w, r)
	case strings.HasSuffix(path, ":updateContact"):
		s.handleUpdate(w, r, strings.TrimSuffix(strings.TrimPrefix(path, "/"), ":updateContact"))
	case strings.HasSuffix(path, ":deleteContact"):
		s.handleDelete(w, strings.TrimSuffix(strings.TrimPrefix(path, "/"), ":deleteContact"))
	case path == "/contactGroups" && r.Method == http.MethodPost:
		s.handleCreateGroup(w, r)
	case path == "/contactGroups":
		s.handleListGroups(w, r)
	case strings.HasSuffix(path, "/members:modify"):
		s.handleModifyGroupMembers(w, r, strings.TrimSuffix(strings.TrimPrefix(path, "/"), "/members:modify"))
	default:
		s.handleGet(w, strings.TrimPrefix(path, "/"))
	}
}

func (s *fakeGoogleServer) handleList(w http.ResponseWriter, r *http.Request) {
	if s.blockListGate != nil {
		gate, ready := s.blockListGate, s.blockListReady
		s.blockListGate, s.blockListReady = nil, nil
		if ready != nil {
			close(ready)
		}
		<-gate
	}
	token := strings.TrimSpace(r.URL.Query().Get("syncToken"))
	out := []googlePerson{}
	if token == "" {
		for _, p := range s.contacts {
			if !p.Metadata.Deleted {
				out = append(out, s.withMemberships(p))
			}
		}
	} else {
		since, _ := strconv.Atoi(token)
		for name, v := range s.versions {
			if v > since {
				out = append(out, s.withMemberships(s.contacts[name]))
			}
		}
	}
	writeJSON(w, http.StatusOK, googleConnectionsResponse{
		Connections:   out,
		NextSyncToken: strconv.Itoa(s.version),
	})
}

func (s *fakeGoogleServer) handleGet(w http.ResponseWriter, resourceName string) {
	p, ok := s.contacts[resourceName]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, s.withMemberships(p))
}

func (s *fakeGoogleServer) handleCreate(w http.ResponseWriter, r *http.Request) {
	var p googlePerson
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p.ResourceName = ""
	name := s.storeLocked(&p, time.Now().UTC())
	writeJSON(w, http.StatusOK, s.withMemberships(s.contacts[name]))
}

func (s *fakeGoogleServer) handleUpdate(w http.ResponseWriter, r *http.Request, resourceName string) {
	if s.failNext[resourceName] > 0 {
		s.failNext[resourceName]--
		http.Error(w, `{"error":{"code":500,"status":"INTERNAL"}}`, http.StatusInternalServerError)
		return
	}
	existing, ok := s.contacts[resourceName]
	if !ok || existing.Metadata.Deleted {
		// Real Google 404s an update against an unknown or already-deleted
		// resourceName - this is what scenario 10 in the test scenarios doc
		// needs to exercise.
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var p googlePerson
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p.ResourceName = resourceName
	name := s.storeLocked(&p, time.Now().UTC())
	writeJSON(w, http.StatusOK, s.withMemberships(s.contacts[name]))
}

func (s *fakeGoogleServer) handleDelete(w http.ResponseWriter, resourceName string) {
	existing, ok := s.contacts[resourceName]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	existing.Metadata.Deleted = true
	s.storeLocked(existing, time.Now().UTC())
	w.WriteHeader(http.StatusOK)
}

// withMemberships returns a copy of p with Memberships populated from
// groupMembers, since (like real Google) membership is tracked separately
// from the contact record and only ever changed via
// contactGroups.members.modify. Callers must hold s.mu.
func (s *fakeGoogleServer) withMemberships(p *googlePerson) googlePerson {
	out := *p
	memberships := make([]googleMembership, 0)
	for groupRN, members := range s.groupMembers {
		if members[p.ResourceName] {
			memberships = append(memberships, googleMembership{ContactGroupMembership: &googleContactGroupMembership{ContactGroupResourceName: groupRN}})
		}
	}
	out.Memberships = memberships
	return out
}

// handleListGroups serves contactGroups.list (no pagination needed for
// tests - everything fits on one page).
func (s *fakeGoogleServer) handleListGroups(w http.ResponseWriter, r *http.Request) {
	out := make([]googleContactGroup, 0, len(s.groups))
	for _, g := range s.groups {
		out = append(out, *g)
	}
	writeJSON(w, http.StatusOK, googleContactGroupsResponse{ContactGroups: out})
}

// handleCreateGroup serves contactGroups.create, including the real API's
// 409 on a duplicate name.
func (s *fakeGoogleServer) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ContactGroup googleContactGroup `json:"contactGroup"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := body.ContactGroup.Name
	if _, exists := s.groupsByName[name]; exists {
		http.Error(w, `{"error":{"code":409,"status":"ALREADY_EXISTS"}}`, http.StatusConflict)
		return
	}
	s.nextGroupID++
	rn := fmt.Sprintf("contactGroups/g%d", s.nextGroupID)
	g := &googleContactGroup{ResourceName: rn, Name: name, GroupType: "USER_CONTACT_GROUP"}
	s.groups[rn] = g
	s.groupsByName[name] = rn
	s.groupMembers[rn] = map[string]bool{}
	writeJSON(w, http.StatusOK, *g)
}

// handleModifyGroupMembers serves contactGroups.members.modify.
func (s *fakeGoogleServer) handleModifyGroupMembers(w http.ResponseWriter, r *http.Request, groupResourceName string) {
	var body struct {
		ResourceNamesToAdd    []string `json:"resourceNamesToAdd"`
		ResourceNamesToRemove []string `json:"resourceNamesToRemove"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	members, ok := s.groupMembers[groupResourceName]
	if !ok {
		members = map[string]bool{}
		s.groupMembers[groupResourceName] = members
	}
	for _, rn := range body.ResourceNamesToAdd {
		members[rn] = true
	}
	for _, rn := range body.ResourceNamesToRemove {
		delete(members, rn)
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
