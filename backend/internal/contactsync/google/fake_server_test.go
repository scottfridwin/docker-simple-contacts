//go:build integration

package google

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

	mu       sync.Mutex
	contacts map[string]*googlePerson
	versions map[string]int
	nextID   int
	version  int
}

func newFakeGoogleServer() *fakeGoogleServer {
	s := &fakeGoogleServer{
		contacts: map[string]*googlePerson{},
		versions: map[string]int{},
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
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
	default:
		s.handleGet(w, strings.TrimPrefix(path, "/"))
	}
}

func (s *fakeGoogleServer) handleList(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("syncToken"))
	out := []googlePerson{}
	if token == "" {
		for _, p := range s.contacts {
			if !p.Metadata.Deleted {
				out = append(out, *p)
			}
		}
	} else {
		since, _ := strconv.Atoi(token)
		for name, v := range s.versions {
			if v > since {
				out = append(out, *s.contacts[name])
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
	writeJSON(w, http.StatusOK, *p)
}

func (s *fakeGoogleServer) handleCreate(w http.ResponseWriter, r *http.Request) {
	var p googlePerson
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p.ResourceName = ""
	name := s.storeLocked(&p, time.Now().UTC())
	writeJSON(w, http.StatusOK, *s.contacts[name])
}

func (s *fakeGoogleServer) handleUpdate(w http.ResponseWriter, r *http.Request, resourceName string) {
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
	writeJSON(w, http.StatusOK, *s.contacts[name])
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
