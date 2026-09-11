package google

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/scottfridlund/contacts/backend/internal/contactsync"
	"github.com/scottfridlund/contacts/backend/internal/person"
)

const (
	localIDUserDefinedKey = "contacts_local_id"
	// pronounsUserDefinedKey stores pronouns as a Google custom field, since
	// the People API has no native pronouns concept - reserved the same way
	// localIDUserDefinedKey is, so it's excluded from the user's own
	// custom_fields and not shown as a normal editable field.
	pronounsUserDefinedKey = "contacts_pronouns"
	remoteUpdatedFieldKey  = "_google_updated_at"
	googleIssuer           = "https://accounts.google.com"
)

// Config controls OAuth and API endpoints for Google sync.
type Config struct {
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	AuthURL       string
	TokenURL      string
	PeopleBaseURL string
	Scopes        []string
	// DryRun, when true, suppresses every write to Google (createContact,
	// updateContact, deleteContact, contactGroups create/modify) - reads
	// (listing/pulling contacts and groups) are unaffected, so the import
	// side of sync can be exercised against a real account with zero risk
	// of mutating it. Intended for testing sync behavior before trusting
	// it against production Google data.
	DryRun bool
}

type accountStateStore interface {
	// UpdateSyncState persists only the fields Sync itself manages (OAuth
	// tokens, cursor, status, last-synced/error) - never user-editable
	// settings like display_name/sync_frequency_minutes, which a
	// long-running sync must not clobber with its own stale snapshot.
	UpdateSyncState(context.Context, *contactsync.Account) (*contactsync.Account, error)
}

// recordLinkStore maps a local Person to its remote record on one specific
// sync account, so the same Person can be linked to several accounts (even
// several accounts of the same provider) without colliding on a shared field.
type recordLinkStore interface {
	Get(ctx context.Context, syncAccountID, personID uuid.UUID) (*contactsync.RecordLink, error)
	GetByRemoteID(ctx context.Context, syncAccountID uuid.UUID, remoteID string) (*contactsync.RecordLink, error)
	Upsert(ctx context.Context, link *contactsync.RecordLink) error
}

type personService interface {
	Get(context.Context, uuid.UUID) (*person.Person, error)
	Create(context.Context, person.CreateInput) (*person.Person, person.ValidationErrors, error)
	Update(context.Context, uuid.UUID, person.UpdateInput) (*person.Person, person.ValidationErrors, error)
	Delete(context.Context, uuid.UUID) error
	List(context.Context, person.ListParams) ([]person.Person, int, error)
	ListRelationships(context.Context, uuid.UUID) ([]person.RelationshipView, error)
	ListIncomingRelationships(context.Context, uuid.UUID) ([]person.RelationshipView, error)
	ReplaceRelationships(context.Context, uuid.UUID, []person.RelationshipInput) error
	FindByDisplayName(context.Context, string) ([]person.Person, error)
	FindByExactName(context.Context, string, string) ([]person.Person, error)
}

// Adapter implements OAuth and People API operations for Google Contacts.
type Adapter struct {
	cfg      Config
	http     *http.Client
	accounts accountStateStore
	people   personService
	links    recordLinkStore
	verifier *oidc.IDTokenVerifier
	logger   *slog.Logger
	syncing  sync.Map // account ID -> struct{}, accounts with a Sync in flight
	// groupCaches holds one *groupResolver per Google account (keyed by
	// AuthSession.ProviderAccountID) for the life of a single sync run, so
	// label <-> contactGroups.resourceName lookups don't re-list an
	// account's groups (or risk creating duplicate groups) on every contact
	// processed in that run. Populated lazily and evicted at the end of
	// Sync(); entries are never shared across different Google accounts.
	groupCaches sync.Map
}

// NewAdapter constructs a Google adapter. It performs OIDC discovery against
// Google's issuer so ID tokens can be verified during CompleteAuthorization.
// A nil logger falls back to slog.Default().
func NewAdapter(ctx context.Context, cfg Config, accounts accountStateStore, people personService, links recordLinkStore, client *http.Client, logger *slog.Logger) (*Adapter, error) {
	if cfg.AuthURL == "" {
		cfg.AuthURL = "https://accounts.google.com/o/oauth2/v2/auth"
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = "https://oauth2.googleapis.com/token"
	}
	if cfg.PeopleBaseURL == "" {
		cfg.PeopleBaseURL = "https://people.googleapis.com/v1"
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"https://www.googleapis.com/auth/contacts", oidc.ScopeOpenID, "email"}
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	if logger == nil {
		logger = slog.Default()
	}
	provider, err := oidc.NewProvider(ctx, googleIssuer)
	if err != nil {
		return nil, fmt.Errorf("discovering google oidc provider: %w", err)
	}
	return &Adapter{
		cfg:      cfg,
		accounts: accounts,
		people:   people,
		links:    links,
		http:     client,
		logger:   logger,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
	}, nil
}

// ProviderName returns the provider key used by the sync registry.
func (a *Adapter) ProviderName() string { return "google" }

// Capabilities reports the initial Google sync capabilities.
func (a *Adapter) Capabilities() contactsync.Capabilities {
	return contactsync.Capabilities{
		SupportsTwoWaySync:      true,
		SupportsDeletes:         true,
		SupportsIncrementalSync: true,
		SupportsPartialMerge:    true,
	}
}

func (a *Adapter) oauthConfig(redirectURI string) *oauth2.Config {
	if redirectURI == "" {
		redirectURI = a.cfg.RedirectURL
	}
	return &oauth2.Config{
		ClientID:     a.cfg.ClientID,
		ClientSecret: a.cfg.ClientSecret,
		RedirectURL:  redirectURI,
		Scopes:       append([]string(nil), a.cfg.Scopes...),
		Endpoint: oauth2.Endpoint{
			AuthURL:  a.cfg.AuthURL,
			TokenURL: a.cfg.TokenURL,
		},
	}
}

// BeginAuthorization starts the OAuth authorization flow.
func (a *Adapter) BeginAuthorization(_ context.Context, redirectURI string, state string) (contactsync.AuthRequest, error) {
	if strings.TrimSpace(state) == "" {
		return contactsync.AuthRequest{}, errors.New("oauth state is required")
	}
	if strings.TrimSpace(a.cfg.ClientID) == "" || strings.TrimSpace(a.cfg.ClientSecret) == "" {
		return contactsync.AuthRequest{}, errors.New("google oauth is not configured")
	}
	url := a.oauthConfig(redirectURI).AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent select_account"),
		oauth2.SetAuthURLParam("include_granted_scopes", "true"),
	)
	return contactsync.AuthRequest{AuthorizationURL: url, State: state}, nil
}

// CompleteAuthorization exchanges the callback code and returns the auth session.
func (a *Adapter) CompleteAuthorization(ctx context.Context, code string) (contactsync.AuthSession, error) {
	if strings.TrimSpace(code) == "" {
		return contactsync.AuthSession{}, errors.New("oauth code is required")
	}
	tok, err := a.oauthConfig("").Exchange(ctx, code)
	if err != nil {
		return contactsync.AuthSession{}, fmt.Errorf("google oauth exchange failed: %w", err)
	}
	providerAccountID, displayName, err := a.resolveIdentity(ctx, tok)
	if err != nil {
		return contactsync.AuthSession{}, err
	}
	scope, _ := tok.Extra("scope").(string)
	if strings.TrimSpace(scope) == "" {
		scope = strings.Join(a.cfg.Scopes, " ")
	}
	return contactsync.AuthSession{
		ProviderAccountID: providerAccountID,
		DisplayName:       displayName,
		AccessToken:       tok.AccessToken,
		RefreshToken:      tok.RefreshToken,
		ExpiresAt:         tok.Expiry.UTC(),
		Scope:             scope,
	}, nil
}

// resolveIdentity verifies the Google-issued ID token and returns a stable,
// per-account identifier (the token subject) plus the account's email for
// display, so multiple Google accounts can be told apart in the UI.
func (a *Adapter) resolveIdentity(ctx context.Context, tok *oauth2.Token) (string, string, error) {
	rawIDToken, _ := tok.Extra("id_token").(string)
	if rawIDToken == "" || a.verifier == nil {
		return "", "", errors.New("google did not return an id token; ensure the openid/email scopes are granted")
	}
	idToken, err := a.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return "", "", fmt.Errorf("verifying google id token: %w", err)
	}
	var claims struct {
		Email string `json:"email"`
	}
	_ = idToken.Claims(&claims)
	if strings.TrimSpace(idToken.Subject) == "" {
		return "", "", errors.New("google id token is missing a subject")
	}
	return idToken.Subject, strings.TrimSpace(claims.Email), nil
}

// RefreshAuthorization refreshes a Google OAuth token using the refresh token.
func (a *Adapter) RefreshAuthorization(ctx context.Context, session contactsync.AuthSession) (contactsync.AuthSession, error) {
	if strings.TrimSpace(session.RefreshToken) == "" {
		return contactsync.AuthSession{}, errors.New("google refresh token is missing")
	}
	source := a.oauthConfig("").TokenSource(ctx, &oauth2.Token{RefreshToken: session.RefreshToken})
	tok, err := source.Token()
	if err != nil {
		return contactsync.AuthSession{}, fmt.Errorf("google oauth refresh failed: %w", err)
	}
	refreshed := session
	if tok.AccessToken != "" {
		refreshed.AccessToken = tok.AccessToken
	}
	if tok.RefreshToken != "" {
		refreshed.RefreshToken = tok.RefreshToken
	}
	if !tok.Expiry.IsZero() {
		refreshed.ExpiresAt = tok.Expiry.UTC()
	}
	if scope, _ := tok.Extra("scope").(string); strings.TrimSpace(scope) != "" {
		refreshed.Scope = scope
	}
	return refreshed, nil
}

// ListChanges lists incremental remote updates since the previous sync token.
//
// Google's people.connections.list requires two DIFFERENT tokens sent via
// two DIFFERENT query parameters: pageToken to continue a listing already
// in progress, and syncToken to resume incremental sync on the NEXT
// separate pull once a listing has fully completed. Our single opaque
// cursor string encodes whichever of the two is currently relevant (see
// contactSyncCursor) - previously every cursor value was sent as syncToken
// regardless of which kind it actually was, so continuing to a second page
// sent that page's nextPageToken as if it were a syncToken. Google treats
// that as an unrecognized/invalid sync token and silently starts the whole
// listing over, which is why a pull with more than one page of contacts
// never reached HasMore=false - it kept restarting forever, reprocessing
// the same (already-linked, so not duplicated) contacts in a different
// order each time and burning through the rate-limit quota on every
// restart.
func (a *Adapter) ListChanges(ctx context.Context, session contactsync.AuthSession, cursor string) (contactsync.ChangePage, error) {
	resolver, err := a.groupResolverFor(ctx, session)
	if err != nil {
		return contactsync.ChangePage{}, err
	}
	state := decodeContactSyncCursor(cursor)
	values := url.Values{}
	values.Set("personFields", googlePersonFields)
	values.Set("requestSyncToken", "true")
	switch {
	case state.PageToken != "":
		values.Set("pageToken", state.PageToken)
	case state.SyncToken != "":
		values.Set("syncToken", state.SyncToken)
	}
	body := googleConnectionsResponse{}
	if err := a.getJSON(ctx, session.AccessToken, "/people/me/connections?"+values.Encode(), &body); err != nil {
		return contactsync.ChangePage{}, err
	}
	records := make([]contactsync.ProviderRecord, 0, len(body.Connections))
	for _, entry := range body.Connections {
		records = append(records, toProviderRecord(entry, resolver))
	}
	next := contactSyncCursor{}
	switch {
	case body.NextPageToken != "":
		next.PageToken = body.NextPageToken
	case body.NextSyncToken != "":
		next.SyncToken = body.NextSyncToken
	default:
		next.SyncToken = state.SyncToken
	}
	return contactsync.ChangePage{
		Records:    records,
		NextCursor: next.encode(),
		HasMore:    body.NextPageToken != "",
	}, nil
}

// contactSyncCursor distinguishes an in-progress page continuation from a
// completed pass's resumable sync token (see ListChanges). Encoded as JSON
// so Account.SyncCursor can stay a plain opaque string column; a raw,
// non-JSON value (any cursor persisted before this fix) decodes as a bare
// sync token, since that's the only kind of cursor ever persisted before.
type contactSyncCursor struct {
	PageToken string `json:"p,omitempty"`
	SyncToken string `json:"s,omitempty"`
}

func decodeContactSyncCursor(raw string) contactSyncCursor {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return contactSyncCursor{}
	}
	var c contactSyncCursor
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return contactSyncCursor{SyncToken: raw}
	}
	return c
}

func (c contactSyncCursor) encode() string {
	if c.PageToken == "" && c.SyncToken == "" {
		return ""
	}
	data, err := json.Marshal(c)
	if err != nil {
		return c.SyncToken
	}
	return string(data)
}

// FetchRecord fetches one remote Google contact.
func (a *Adapter) FetchRecord(ctx context.Context, session contactsync.AuthSession, remoteID string) (contactsync.ProviderRecord, error) {
	personBody, err := a.getContact(ctx, session.AccessToken, remoteID)
	if err != nil {
		return contactsync.ProviderRecord{}, err
	}
	resolver, err := a.groupResolverFor(ctx, session)
	if err != nil {
		return contactsync.ProviderRecord{}, err
	}
	return toProviderRecord(personBody, resolver), nil
}

// UpsertRecord creates or updates one remote contact.
func (a *Adapter) UpsertRecord(ctx context.Context, session contactsync.AuthSession, record contactsync.Record) (contactsync.ProviderRecord, error) {
	if a.cfg.DryRun {
		action := "update"
		if strings.TrimSpace(record.ExternalID) == "" {
			action = "create"
		}
		a.logger.Info("dry run: skipping google write", "action", action, "external_id", record.ExternalID, "field_count", len(record.Fields))
		return contactsync.ProviderRecord{Record: record}, nil
	}
	resolver, err := a.groupResolverFor(ctx, session)
	if err != nil {
		return contactsync.ProviderRecord{}, err
	}
	if strings.TrimSpace(record.ExternalID) == "" {
		payload := toGooglePerson(record, "")
		var created googlePerson
		if err := a.postJSON(ctx, session.AccessToken, "/people:createContact", payload, &created); err != nil {
			return contactsync.ProviderRecord{}, err
		}
		if err := a.reconcileMemberships(ctx, session.AccessToken, resolver, created.ResourceName, created, record); err != nil {
			a.logger.Warn("failed to sync google group memberships for new contact", "external_id", created.ResourceName, "error", err)
		}
		return toProviderRecord(created, resolver), nil
	}
	current, err := a.getContact(ctx, session.AccessToken, record.ExternalID)
	if err != nil {
		return contactsync.ProviderRecord{}, err
	}
	values := url.Values{}
	values.Set("updatePersonFields", googleUpdatePersonFields)
	updated, err := a.updateContact(ctx, session.AccessToken, record, current.ETag, values)
	if isETagConflict(err) {
		// The contact changed on Google's side between our read of its etag
		// above and this update - re-read the now-current etag and retry
		// once instead of failing the whole sync run.
		if refetched, refetchErr := a.getContact(ctx, session.AccessToken, record.ExternalID); refetchErr == nil {
			current = refetched
			updated, err = a.updateContact(ctx, session.AccessToken, record, current.ETag, values)
		}
	}
	if err != nil {
		return contactsync.ProviderRecord{}, err
	}
	if err := a.reconcileMemberships(ctx, session.AccessToken, resolver, record.ExternalID, current, record); err != nil {
		a.logger.Warn("failed to sync google group memberships", "external_id", record.ExternalID, "error", err)
	}
	return toProviderRecord(updated, resolver), nil
}

// updateContact sends one updateContact PATCH using the given etag.
func (a *Adapter) updateContact(ctx context.Context, accessToken string, record contactsync.Record, etag string, values url.Values) (googlePerson, error) {
	payload := toGooglePerson(record, etag)
	var updated googlePerson
	if err := a.patchJSON(ctx, accessToken, "/"+record.ExternalID+":updateContact?"+values.Encode(), payload, &updated); err != nil {
		return googlePerson{}, err
	}
	return updated, nil
}

// DeleteRecord deletes one remote Google contact.
func (a *Adapter) DeleteRecord(ctx context.Context, session contactsync.AuthSession, remoteID string) error {
	if strings.TrimSpace(remoteID) == "" {
		return nil
	}
	if a.cfg.DryRun {
		a.logger.Info("dry run: skipping google delete", "external_id", remoteID)
		return nil
	}
	return a.delete(ctx, session.AccessToken, "/"+remoteID+":deleteContact")
}

// Sync performs two-way synchronization for the current job and account state.
func (a *Adapter) Sync(ctx context.Context, account contactsync.Account, job contactsync.Job) error {
	if a == nil {
		return nil
	}
	if a.accounts == nil || a.people == nil || a.links == nil {
		return errors.New("google sync adapter dependencies are not configured")
	}
	// A large contact list under heavy rate-limiting can take far longer
	// than the scheduler's tick interval, so the periodic due-account scan
	// and the "sync immediately after connecting" background trigger can
	// both try to run this same account concurrently before either one
	// finishes and persists LastSyncedAt/SyncCursor. Two overlapping full
	// pulls each see the other's not-yet-committed local_id tags as absent,
	// so findUnlinkedMatch (which excludes a match already linked to this
	// account) falls through to creating a second Person for almost every
	// contact - reject the second call outright instead.
	if _, alreadyRunning := a.syncing.LoadOrStore(account.ID, struct{}{}); alreadyRunning {
		if job.PersonID != uuid.Nil {
			// A job-scoped call must not report success without ever having
			// pushed the job's own change: returning nil here (like the
			// full-sync case below) would make the caller mark the job
			// "done" even though syncLocalJob never ran for this account,
			// silently dropping that specific edit. Returning an error lets
			// the existing job retry/backoff machinery pick it up again
			// shortly, once the in-flight sync has finished.
			a.logger.Info("google sync already in progress for this account, will retry the pending job", "account_id", account.ID, "person_id", job.PersonID)
			return ErrSyncInProgress
		}
		a.logger.Info("google sync already in progress for this account, skipping", "account_id", account.ID)
		return nil
	}
	defer a.syncing.Delete(account.ID)

	a.logger.Info("google sync starting", "account_id", account.ID, "provider_account_id", account.ProviderAccountID, "has_job", job.PersonID != uuid.Nil)

	session, err := sessionFromAccount(account)
	if err != nil {
		return a.markAccountFailed(ctx, &account, &reauthRequiredError{err})
	}
	session, err = a.ensureSession(ctx, &account, session)
	if err != nil {
		return a.markAccountFailed(ctx, &account, &reauthRequiredError{err})
	}
	// Evict this run's cached group list on exit, so a later run (whether
	// the next scheduled sync or a one-off syncLocalJob push) always sees
	// any group renamed/created/deleted directly in Google Contacts since.
	defer a.groupCaches.Delete(session.ProviderAccountID)

	if job.PersonID != uuid.Nil {
		if err := a.syncLocalJob(ctx, account.ID, session, job); err != nil {
			return a.markAccountFailed(ctx, &account, err)
		}
	}

	initialSync := strings.TrimSpace(account.SyncCursor) == ""
	nextCursor, pulled, err := a.pullRemote(ctx, &account, session, account.SyncCursor)
	// Persist whatever progress pullRemote made even on error, so a failed
	// attempt doesn't force the next one to replay already-merged pages
	// (see pullRemote's comments) - a plain assignment after an early
	// return would silently discard that progress.
	account.SyncCursor = nextCursor
	if err != nil {
		return a.markAccountFailed(ctx, &account, err)
	}

	if initialSync {
		if err := a.exportLocal(ctx, &account, session); err != nil {
			return a.markAccountFailed(ctx, &account, err)
		}
	}

	now := time.Now().UTC()
	account.LastSyncedAt = &now
	account.LastError = nil
	account.Status = "connected"
	if _, err := a.accounts.UpdateSyncState(ctx, &account); err != nil {
		return fmt.Errorf("updating sync account state: %w", err)
	}
	a.logger.Info("google sync finished", "account_id", account.ID, "remote_records_seen", pulled, "initial_sync", initialSync)
	return nil
}

// remoteIDFor returns the remote resource name already linked to a person on
// this specific sync account, or "" if none is known yet.
func (a *Adapter) remoteIDFor(ctx context.Context, accountID, personID uuid.UUID) string {
	if a.links == nil {
		return ""
	}
	link, err := a.links.Get(ctx, accountID, personID)
	if err != nil {
		return ""
	}
	return link.RemoteID
}

// personIDForRemote returns the person already linked to a remote record on
// this specific sync account, if any. This catches a remote record that's
// already linked in our own link table (typically via an earlier exact-name
// match) even when its own contacts_local_id tag is stale or missing -
// without this check, such a record would otherwise look "unlinked" and get
// recreated as a duplicate Person on every sync.
func (a *Adapter) personIDForRemote(ctx context.Context, accountID uuid.UUID, remoteID string) (uuid.UUID, bool) {
	if a.links == nil {
		return uuid.Nil, false
	}
	link, err := a.links.GetByRemoteID(ctx, accountID, remoteID)
	if err != nil {
		return uuid.Nil, false
	}
	return link.PersonID, true
}

// linkRecord records (or updates) which remote resource a person maps to on
// this specific sync account.
func (a *Adapter) linkRecord(ctx context.Context, accountID, personID uuid.UUID, remoteID string) {
	if a.links == nil || personID == uuid.Nil || strings.TrimSpace(remoteID) == "" {
		return
	}
	if err := a.links.Upsert(ctx, &contactsync.RecordLink{
		SyncAccountID: accountID,
		PersonID:      personID,
		RemoteID:      remoteID,
	}); err != nil {
		a.logger.Error("google sync linkRecord: failed to persist remote link", "account_id", accountID, "person_id", personID, "remote_id", remoteID, "error", err)
	}
}

// pendingRelationship defers relationship reconciliation for a person until
// after every remote record in the current pull has been created/updated
// locally, so a relationship can resolve to a contact that was itself only
// just created earlier in the same sync run (e.g. Person A references
// Person B, but B hadn't been synced yet when A was processed).
type pendingRelationship struct {
	PersonID uuid.UUID
	Fields   map[string]contactsync.FieldState
}

// attachRelationsForExport loads personID's current relationships (resolving
// live display names) and attaches them to record's "relations" field, since
// relationships can't be populated by the plain Person.Snapshot()/
// personToRecord() path - they require a database join, not just the
// person's own row.
func (a *Adapter) attachRelationsForExport(ctx context.Context, personID uuid.UUID, updatedAt time.Time, record contactsync.Record) contactsync.Record {
	views, err := a.people.ListRelationships(ctx, personID)
	if err != nil {
		a.logger.Warn("failed to load relationships for export", "person_id", personID, "error", err)
		return record
	}
	relations := make([]contactsync.LabeledValue, 0, len(views))
	for _, v := range views {
		if v.RelatedPersonName == "" {
			continue
		}
		relations = append(relations, contactsync.LabeledValue{Label: string(v.Type), Value: v.RelatedPersonName})
	}
	record.Fields["relations"] = contactsync.FieldState{IsSet: true, Value: relations, UpdatedAt: updatedAt}
	return record
}

// relationshipAlreadyExists reports whether existing (personID's incoming,
// other-person-owned relationships) already contains an entry equivalent to
// the candidate, so it isn't stored a second time. Google can report the
// same relationship symmetrically on both contacts (A says "child: B" and B
// says "parent: A"), but that is exactly one relationship in our schema -
// whichever side is reconciled second must skip re-adding its own copy of
// what the other side already established.
func relationshipAlreadyExists(existing []person.RelationshipView, relType person.RelationType, relatedID *uuid.UUID, name string) bool {
	for _, v := range existing {
		if v.Type != relType {
			continue
		}
		if relatedID != nil {
			if v.RelatedPersonID != nil && *v.RelatedPersonID == *relatedID {
				return true
			}
			continue
		}
		if v.RelatedPersonID == nil && v.RelatedPersonName == name {
			return true
		}
	}
	return false
}

// reconcileRelationships replaces personID's relationships with the ones
// reported by the remote provider, resolving each provider-supplied name to
// an existing local contact when there's exactly one exact display-name
// match, and otherwise keeping it as an unlinked, name-only relationship so
// the information isn't lost. Entries already represented via the other
// person's side (see relationshipAlreadyExists) are skipped to avoid
// duplicate rows.
func (a *Adapter) reconcileRelationships(ctx context.Context, personID uuid.UUID, fields map[string]contactsync.FieldState) {
	raw := fieldLabeledValues(fields, "relations")
	incoming, err := a.people.ListIncomingRelationships(ctx, personID)
	if err != nil {
		a.logger.Warn("failed to load incoming relationships for dedup", "person_id", personID, "error", err)
		incoming = nil
	}
	inputs := make([]person.RelationshipInput, 0, len(raw))
	for _, rel := range raw {
		relType := person.RelationType(rel.Label)
		if !relType.Valid() {
			continue
		}
		name := strings.TrimSpace(rel.Value)
		if name == "" {
			continue
		}
		in := person.RelationshipInput{Type: relType}
		var relatedID *uuid.UUID
		if matches, err := a.people.FindByDisplayName(ctx, name); err == nil && len(matches) == 1 && matches[0].ID != personID {
			id := matches[0].ID
			relatedID = &id
			in.RelatedPersonID = &id
		} else {
			relatedName := name
			in.RelatedPersonName = &relatedName
		}
		if relationshipAlreadyExists(incoming, relType, relatedID, name) {
			continue
		}
		inputs = append(inputs, in)
	}
	if err := a.people.ReplaceRelationships(ctx, personID, inputs); err != nil {
		a.logger.Warn("failed to reconcile relationships from google", "person_id", personID, "error", err)
	}
}

func (a *Adapter) syncLocalJob(ctx context.Context, accountID uuid.UUID, session contactsync.AuthSession, job contactsync.Job) error {
	remoteID := a.remoteIDFor(ctx, accountID, job.PersonID)
	if job.Kind == contactsync.ChangeKindDeleted || job.Kind == contactsync.ChangeKindHardDeleted {
		return a.DeleteRecord(ctx, session, remoteID)
	}
	record := snapshotToRecord(job.Snapshot)
	record.ExternalID = remoteID
	record = a.attachRelationsForExport(ctx, job.Snapshot.ID, job.Snapshot.UpdatedAt, record)
	providerRecord, err := a.UpsertRecord(ctx, session, record)
	if err != nil {
		return err
	}
	a.linkRecord(ctx, accountID, job.PersonID, providerRecord.Record.ExternalID)
	return nil
}

func (a *Adapter) pullRemote(ctx context.Context, account *contactsync.Account, session contactsync.AuthSession, cursor string) (string, int, error) {
	accountID := account.ID
	current := cursor
	seen := 0
	var pending []pendingRelationship
	for {
		// A single Sync() run - especially a large pull slowed by repeated
		// 429 rate-limit backoff - can run far longer than the access
		// token's remaining lifetime. The one refresh at the top of Sync()
		// only covers the token as of the start of the run, so re-check
		// (and refresh if needed) before every page instead of just once,
		// or a long-running pull ends up failing partway through with a
		// 401 even though the account's credentials are actually fine.
		refreshed, err := a.ensureSession(ctx, account, session)
		if err != nil {
			return current, seen, &reauthRequiredError{err}
		}
		session = refreshed
		page, err := a.ListChanges(ctx, session, current)
		if err != nil {
			var apiErr *apiError
			if errors.As(err, &apiErr) && apiErr.Status == http.StatusGone && current != "" {
				a.logger.Info("google sync token expired, restarting full pull", "account_id", accountID)
				current = ""
				continue
			}
			// Preserve progress already made through earlier pages in this
			// same run (current, not the original stale cursor) - returning
			// the stale cursor here would replay already-merged records on
			// the next attempt, which can produce duplicate local contacts
			// (findUnlinkedMatch won't reuse a record already linked to
			// this same account, so a replay looks like a brand new one).
			return current, seen, fmt.Errorf("listing google contact changes: %w", err)
		}
		a.logger.Info("google sync fetched remote page", "account_id", accountID, "records", len(page.Records), "has_more", page.HasMore)
		seen += len(page.Records)
		for _, remote := range page.Records {
			// A single record that fails to merge (a transient error that
			// outlived a.do's own retries, a malformed payload, etc.) must
			// not abort the rest of the page/pull - besides losing progress
			// on every other record, restarting from an earlier cursor
			// replays already-merged records and risks duplicating them
			// (see the comment above).
			if err := a.mergeRemoteRecord(ctx, accountID, session, remote, &pending); err != nil {
				a.logger.Error("failed to merge remote record, skipping", "account_id", accountID, "external_id", remote.Record.ExternalID, "error", err)
			}
		}
		current = page.NextCursor
		if !page.HasMore {
			break
		}
	}
	// Second pass: every contact from this pull has now been created or
	// updated locally, so relationships that referenced a not-yet-synced
	// contact earlier in this same run can now resolve correctly.
	for _, p := range pending {
		a.reconcileRelationships(ctx, p.PersonID, p.Fields)
	}
	return current, seen, nil
}

func (a *Adapter) exportLocal(ctx context.Context, account *contactsync.Account, session contactsync.AuthSession) error {
	accountID := account.ID
	page := 1
	exported := 0
	for {
		// See the matching comment in pullRemote: a large export can run
		// long enough for the access token to expire mid-run, so refresh
		// (if needed) before every page rather than relying solely on the
		// one refresh at the top of Sync().
		refreshed, err := a.ensureSession(ctx, account, session)
		if err != nil {
			return &reauthRequiredError{err}
		}
		session = refreshed
		rows, total, err := a.people.List(ctx, person.ListParams{Page: page, PageSize: 100, SortField: "updated_at", SortDesc: false})
		if err != nil {
			return fmt.Errorf("listing local persons for export: %w", err)
		}
		for i := range rows {
			remoteID := a.remoteIDFor(ctx, accountID, rows[i].ID)
			record := a.attachRelationsForExport(ctx, rows[i].ID, rows[i].UpdatedAt, personToRecord(rows[i], remoteID))
			providerRecord, upsertErr := a.UpsertRecord(ctx, session, record)
			if upsertErr != nil {
				// One contact failing to push (a transient error outliving
				// a.do's own retries, etc.) must not abort exporting every
				// other contact in this account - see the matching comment
				// in pullRemote.
				a.logger.Error("failed to export local contact, skipping", "account_id", accountID, "person_id", rows[i].ID, "error", upsertErr)
				continue
			}
			a.linkRecord(ctx, accountID, rows[i].ID, providerRecord.Record.ExternalID)
			exported++
		}
		if page*100 >= total || len(rows) == 0 {
			break
		}
		page++
	}
	a.logger.Info("google sync exported local contacts", "account_id", accountID, "count", exported)
	return nil
}

// findUnlinkedMatch looks for exactly one existing local Person with the
// same first+last name as a Google contact that has no contacts_local_id
// tag, so a contact that already exists on both sides before this account
// was ever connected gets linked instead of duplicated. Only persons not
// already linked to this account are considered, since a linked person's
// Google copy would already carry its local_id tag. An ambiguous (more than
// one) or absent match returns false, leaving the caller to create a new
// Person as before.
func (a *Adapter) findUnlinkedMatch(ctx context.Context, accountID uuid.UUID, remoteModel person.CreateInput) (*person.Person, bool) {
	matches, err := a.people.FindByExactName(ctx, remoteModel.FirstName, remoteModel.LastName)
	if err != nil || len(matches) == 0 {
		a.logger.Info("google sync findUnlinkedMatch: no name matches", "account_id", accountID, "match_count", len(matches), "err", err)
		return nil, false
	}
	var candidate *person.Person
	excludedAlreadyLinked := 0
	for i := range matches {
		if a.remoteIDFor(ctx, accountID, matches[i].ID) != "" {
			excludedAlreadyLinked++
			continue
		}
		if candidate != nil {
			a.logger.Info("google sync findUnlinkedMatch: ambiguous, multiple unlinked candidates", "account_id", accountID, "match_count", len(matches), "excluded_already_linked", excludedAlreadyLinked)
			return nil, false
		}
		candidate = &matches[i]
	}
	if candidate == nil {
		a.logger.Info("google sync findUnlinkedMatch: all name matches already linked to this account", "account_id", accountID, "match_count", len(matches), "excluded_already_linked", excludedAlreadyLinked)
		return nil, false
	}
	a.logger.Info("google sync findUnlinkedMatch: matched", "account_id", accountID, "person_id", candidate.ID, "match_count", len(matches), "excluded_already_linked", excludedAlreadyLinked)
	return candidate, true
}

func (a *Adapter) mergeRemoteRecord(ctx context.Context, accountID uuid.UUID, session contactsync.AuthSession, remote contactsync.ProviderRecord, pending *[]pendingRelationship) error {
	remoteModel, localID := remoteToLocal(remote)
	a.logger.Info("google sync mergeRemoteRecord", "account_id", accountID, "external_id", remote.Record.ExternalID, "has_local_id_tag", localID != nil, "tombstone", remote.Record.Tombstone.Deleted)

	// Our own link table is authoritative over the remote record's own
	// contacts_local_id tag: that tag can go stale (e.g. pointing at a
	// Person that was later deleted, or never corrected after this record
	// was linked by name instead of by tag) even though this exact remote
	// record is already linked here. Trusting the tag in that case
	// recreates a brand new Person - and does so again on every later sync,
	// since the doomed recreate's own link write always loses to the
	// existing (account_id, remote_id) row on a unique-constraint conflict.
	if existingID, ok := a.personIDForRemote(ctx, accountID, remote.Record.ExternalID); ok {
		if existing, err := a.people.Get(ctx, existingID); err == nil {
			a.logger.Info("google sync mergeRemoteRecord: matched existing local person by remote link", "account_id", accountID, "external_id", remote.Record.ExternalID, "person_id", existing.ID, "tagged_local_id", localID)
			return a.reconcileExisting(ctx, accountID, session, existing, remoteModel, remote, pending)
		}
	}

	if localID == nil {
		if remote.Record.Tombstone.Deleted {
			// A tombstone for a contact we never linked (e.g. deleted before
			// we ever saw it, or deleted by another client). There's nothing
			// local to create or delete - creating one here would then try
			// to push it back to Google using the already-deleted
			// resourceName, which 404s ("Requested entity was not found")
			// and aborts the whole sync run.
			return nil
		}
		if match, ok := a.findUnlinkedMatch(ctx, accountID, remoteModel); ok {
			a.logger.Info("google sync mergeRemoteRecord: reconciling against unlinked name match", "account_id", accountID, "external_id", remote.Record.ExternalID, "person_id", match.ID)
			return a.reconcileExisting(ctx, accountID, session, match, remoteModel, remote, pending)
		}
		created, verrs, err := a.people.Create(contactsync.WithSyncOrigin(ctx, accountID), remoteModel)
		if err != nil {
			return fmt.Errorf("creating local person from google record %s: %w", remote.Record.ExternalID, err)
		}
		if verrs.HasErrors() {
			return fmt.Errorf("creating local person from google record %s: %s", remote.Record.ExternalID, verrs.Error())
		}
		a.logger.Info("google sync mergeRemoteRecord: created new local person (no tag, no unlinked name match)", "account_id", accountID, "external_id", remote.Record.ExternalID, "person_id", created.ID)
		*pending = append(*pending, pendingRelationship{PersonID: created.ID, Fields: remote.Record.Fields})
		out, upsertErr := a.UpsertRecord(ctx, session, a.attachRelationsForExport(ctx, created.ID, created.UpdatedAt, personToRecord(*created, remote.Record.ExternalID)))
		if upsertErr != nil {
			a.logger.Error("google sync mergeRemoteRecord: failed to push newly-created person back to google, it will remain unlinked", "account_id", accountID, "external_id", remote.Record.ExternalID, "person_id", created.ID, "error", upsertErr)
			return upsertErr
		}
		a.linkRecord(ctx, accountID, created.ID, out.Record.ExternalID)
		return nil
	}

	local, err := a.people.Get(ctx, *localID)
	if err != nil {
		if !errors.Is(err, person.ErrNotFound) {
			return fmt.Errorf("loading local person %s: %w", localID.String(), err)
		}
		if remote.Record.Tombstone.Deleted {
			// The local person is already gone (soft/hard deleted) and the
			// remote record is a tombstone too - nothing to reconcile.
			return nil
		}
		created, verrs, createErr := a.people.Create(contactsync.WithSyncOrigin(ctx, accountID), remoteModel)
		if createErr != nil {
			return fmt.Errorf("creating local person for missing mapping: %w", createErr)
		}
		if verrs.HasErrors() {
			return fmt.Errorf("creating local person for missing mapping: %s", verrs.Error())
		}
		a.logger.Info("google sync mergeRemoteRecord: tagged local_id not found locally, recreated", "account_id", accountID, "external_id", remote.Record.ExternalID, "missing_local_id", localID.String(), "person_id", created.ID)
		*pending = append(*pending, pendingRelationship{PersonID: created.ID, Fields: remote.Record.Fields})
		out, upsertErr := a.UpsertRecord(ctx, session, a.attachRelationsForExport(ctx, created.ID, created.UpdatedAt, personToRecord(*created, remote.Record.ExternalID)))
		if upsertErr != nil {
			return upsertErr
		}
		a.linkRecord(ctx, accountID, created.ID, out.Record.ExternalID)
		return nil
	}

	a.logger.Info("google sync mergeRemoteRecord: matched existing local person by contacts_local_id tag", "account_id", accountID, "external_id", remote.Record.ExternalID, "person_id", local.ID)
	return a.reconcileExisting(ctx, accountID, session, local, remoteModel, remote, pending)
}

// reconcileExisting reconciles a remote record against a Person already
// known to exist locally, whether found via its contacts_local_id tag or
// (for a contact never linked before) an exact first+last name match.
func (a *Adapter) reconcileExisting(ctx context.Context, accountID uuid.UUID, session contactsync.AuthSession, local *person.Person, remoteModel person.CreateInput, remote contactsync.ProviderRecord, pending *[]pendingRelationship) error {
	// The remote record's own resource name is now confirmed for this
	// (account, person) pair regardless of which branch below runs, so record
	// it opportunistically. This also self-heals the link table for contacts
	// that were synced before per-account link tracking existed (or just
	// matched by name for the first time).
	a.linkRecord(ctx, accountID, local.ID, remote.Record.ExternalID)

	remoteUpdatedAt := extractUpdatedAt(remote)
	if remote.Record.Tombstone.Deleted {
		if remoteUpdatedAt.After(local.UpdatedAt) {
			if err := a.people.Delete(contactsync.WithSyncOrigin(ctx, accountID), local.ID); err != nil && !errors.Is(err, person.ErrNotFound) {
				return fmt.Errorf("deleting local person %s: %w", local.ID.String(), err)
			}
		} else {
			record := a.attachRelationsForExport(ctx, local.ID, local.UpdatedAt, personToRecord(*local, remote.Record.ExternalID))
			out, upsertErr := a.UpsertRecord(ctx, session, record)
			var apiErr *apiError
			if upsertErr != nil && errors.As(upsertErr, &apiErr) && apiErr.Status == http.StatusNotFound {
				// The local edit is newer, so it should survive - but the
				// resourceName it's linked to is already gone on Google's
				// side (deleted, and by now possibly purged from Google's
				// trash), so updating it 404s. Recreate it as a brand new
				// contact instead of aborting the whole account's sync, the
				// same way an unmapped tombstone is handled above (see
				// TestMergeRemoteRecordSkipsUnknownTombstone).
				record.ExternalID = ""
				out, upsertErr = a.UpsertRecord(ctx, session, record)
			}
			if upsertErr != nil {
				return upsertErr
			}
			a.linkRecord(ctx, accountID, local.ID, out.Record.ExternalID)
		}
		return nil
	}

	if remoteUpdatedAt.After(local.UpdatedAt) {
		update := fieldAwareUpdate(remote.Record.Fields, remoteModel)
		_, verrs, err := a.people.Update(contactsync.WithSyncOrigin(ctx, accountID), local.ID, update)
		if err != nil {
			return fmt.Errorf("updating local person %s: %w", local.ID.String(), err)
		}
		if verrs.HasErrors() {
			return fmt.Errorf("updating local person %s: %s", local.ID.String(), verrs.Error())
		}
		*pending = append(*pending, pendingRelationship{PersonID: local.ID, Fields: remote.Record.Fields})
		return nil
	}

	if local.UpdatedAt.After(remoteUpdatedAt) {
		_, err := a.UpsertRecord(ctx, session, a.attachRelationsForExport(ctx, local.ID, local.UpdatedAt, personToRecord(*local, remote.Record.ExternalID)))
		return err
	}
	return nil
}

// fieldAwareUpdate builds the local UpdateInput to apply when the remote
// side wins, only marking a field *Set when Google's own payload actually
// reported it (fields[key].IsSet). Without this, a Google contact that
// simply never had e.g. an organization or note would unconditionally wipe
// out a local value for that field on every sync where Google's copy
// happened to be newer overall - clobbering an edit make on the local side
// that Google's edit never touched, even though the two changes didn't
// actually conflict.
func fieldAwareUpdate(fields map[string]contactsync.FieldState, remoteModel person.CreateInput) person.UpdateInput {
	var update person.UpdateInput
	if fieldIsSet(fields, "first_name") {
		update.FirstName = stringPtr(remoteModel.FirstName)
		update.FirstNameSet = true
	}
	if fieldIsSet(fields, "middle_names") {
		update.MiddleNames = &remoteModel.MiddleNames
		update.MiddleNamesSet = true
	}
	if fieldIsSet(fields, "last_name") {
		update.LastName = stringPtr(remoteModel.LastName)
		update.LastNameSet = true
	}
	if fieldIsSet(fields, "nickname") {
		update.Nickname = remoteModel.Nickname
		update.NicknameSet = true
	}
	if fieldIsSet(fields, "pronouns") {
		update.Pronouns = remoteModel.Pronouns
		update.PronounsSet = true
	}
	if fieldIsSet(fields, "birthdate") {
		update.Birthdate = remoteModel.Birthdate
		update.BirthdateSet = true
	}
	if fieldIsSet(fields, "phone_numbers") {
		update.PhoneNumbers = &remoteModel.PhoneNumbers
		update.PhoneNumbersSet = true
	}
	if fieldIsSet(fields, "emails") {
		update.Emails = &remoteModel.Emails
		update.EmailsSet = true
	}
	if fieldIsSet(fields, "addresses") {
		update.Addresses = &remoteModel.Addresses
		update.AddressesSet = true
	}
	if fieldIsSet(fields, "organization") {
		update.Organization = remoteModel.Organization
		update.OrganizationSet = true
	}
	if fieldIsSet(fields, "notes") {
		update.Notes = remoteModel.Notes
		update.NotesSet = true
	}
	if fieldIsSet(fields, "custom_fields") {
		update.CustomFields = remoteModel.CustomFields
		update.CustomFieldsSet = true
	}
	if fieldIsSet(fields, "labels") {
		update.Labels = &remoteModel.Labels
		update.LabelsSet = true
	}
	if fieldIsSet(fields, "is_favorite") {
		v := remoteModel.IsFavorite
		update.IsFavorite = &v
		update.IsFavoriteSet = true
	}
	return update
}

func fieldIsSet(fields map[string]contactsync.FieldState, key string) bool {
	field, ok := fields[key]
	return ok && field.IsSet
}

func (a *Adapter) ensureSession(ctx context.Context, account *contactsync.Account, session contactsync.AuthSession) (contactsync.AuthSession, error) {
	if session.AccessToken == "" {
		return contactsync.AuthSession{}, errors.New("google access token is missing")
	}
	if session.ExpiresAt.IsZero() || session.ExpiresAt.After(time.Now().Add(2*time.Minute)) {
		return session, nil
	}
	refreshed, err := a.RefreshAuthorization(ctx, session)
	if err != nil {
		return contactsync.AuthSession{}, err
	}
	account.AccessToken = stringPtr(refreshed.AccessToken)
	account.RefreshToken = stringPtr(refreshed.RefreshToken)
	if !refreshed.ExpiresAt.IsZero() {
		expiresAt := refreshed.ExpiresAt.UTC()
		account.ExpiresAt = &expiresAt
	}
	account.Scope = refreshed.Scope
	if _, err := a.accounts.UpdateSyncState(ctx, account); err != nil {
		return contactsync.AuthSession{}, fmt.Errorf("persisting refreshed google token: %w", err)
	}
	return refreshed, nil
}

func (a *Adapter) markAccountFailed(ctx context.Context, account *contactsync.Account, syncErr error) error {
	message := syncErr.Error()
	account.LastError = &message
	// Default to a generic failure. Only escalate to reconnect_required when
	// the error is genuinely about missing/invalid/revoked OAuth credentials -
	// transient or unrelated failures (a single bad record, rate limits,
	// Google 5xx, network blips, etc.) must not force the user to re-auth.
	account.Status = "error"
	var reauthErr *reauthRequiredError
	var apiErr *apiError
	switch {
	case errors.As(syncErr, &reauthErr):
		account.Status = "reconnect_required"
	case errors.As(syncErr, &apiErr) && apiErr.Status == http.StatusUnauthorized:
		account.Status = "reconnect_required"
	}
	a.logger.Error("google sync failed", "account_id", account.ID, "provider_account_id", account.ProviderAccountID, "status", account.Status, "error", syncErr)
	_, _ = a.accounts.UpdateSyncState(ctx, account)
	return syncErr
}

// reauthRequiredError marks a sync failure that can only be resolved by the
// user reconnecting the account (missing, invalid, or revoked OAuth
// credentials), as opposed to a transient or record-specific failure.
type reauthRequiredError struct {
	err error
}

func (e *reauthRequiredError) Error() string { return e.err.Error() }
func (e *reauthRequiredError) Unwrap() error { return e.err }

// ErrSyncInProgress is returned by a job-scoped Sync call that lost the race
// against another sync already running for the same account. It's a
// transient condition, not a real failure - the caller should let the
// existing job retry/backoff mechanism try again shortly.
var ErrSyncInProgress = errors.New("google sync already in progress for this account")

func sessionFromAccount(account contactsync.Account) (contactsync.AuthSession, error) {
	if account.AccessToken == nil || strings.TrimSpace(*account.AccessToken) == "" {
		return contactsync.AuthSession{}, errors.New("sync account is missing google access token")
	}
	session := contactsync.AuthSession{
		ProviderAccountID: account.ProviderAccountID,
		AccessToken:       strings.TrimSpace(*account.AccessToken),
		Scope:             account.Scope,
	}
	if account.RefreshToken != nil {
		session.RefreshToken = strings.TrimSpace(*account.RefreshToken)
	}
	if account.ExpiresAt != nil {
		session.ExpiresAt = account.ExpiresAt.UTC()
	}
	return session, nil
}

func snapshotToRecord(snapshot contactsync.PersonSnapshot) contactsync.Record {
	record := contactsync.Record{
		Tombstone: contactsync.Tombstone{Deleted: snapshot.DeletedAt != nil, UpdatedAt: snapshot.UpdatedAt},
		Fields:    map[string]contactsync.FieldState{},
	}
	record.Fields["first_name"] = contactsync.FieldState{IsSet: true, Value: snapshot.FirstName, UpdatedAt: snapshot.UpdatedAt}
	record.Fields["middle_names"] = contactsync.FieldState{IsSet: true, Value: append([]string(nil), snapshot.MiddleNames...), UpdatedAt: snapshot.UpdatedAt}
	record.Fields["last_name"] = contactsync.FieldState{IsSet: true, Value: snapshot.LastName, UpdatedAt: snapshot.UpdatedAt}
	record.Fields["phone_numbers"] = contactsync.FieldState{IsSet: true, Value: append([]contactsync.LabeledValue(nil), snapshot.PhoneNumbers...), UpdatedAt: snapshot.UpdatedAt}
	record.Fields["emails"] = contactsync.FieldState{IsSet: true, Value: append([]contactsync.LabeledValue(nil), snapshot.Emails...), UpdatedAt: snapshot.UpdatedAt}
	record.Fields["addresses"] = contactsync.FieldState{IsSet: true, Value: append([]contactsync.Address(nil), snapshot.Addresses...), UpdatedAt: snapshot.UpdatedAt}
	if snapshot.Organization != nil {
		record.Fields["organization"] = contactsync.FieldState{IsSet: true, Value: snapshot.Organization, UpdatedAt: snapshot.UpdatedAt}
	}
	if snapshot.Notes != nil {
		record.Fields["notes"] = contactsync.FieldState{IsSet: true, Value: *snapshot.Notes, UpdatedAt: snapshot.UpdatedAt}
	}
	if snapshot.Nickname != nil {
		record.Fields["nickname"] = contactsync.FieldState{IsSet: true, Value: *snapshot.Nickname, UpdatedAt: snapshot.UpdatedAt}
	}
	if snapshot.Pronouns != nil {
		record.Fields["pronouns"] = contactsync.FieldState{IsSet: true, Value: *snapshot.Pronouns, UpdatedAt: snapshot.UpdatedAt}
	}
	if snapshot.Birthdate != nil {
		record.Fields["birthdate"] = contactsync.FieldState{IsSet: true, Value: *snapshot.Birthdate, UpdatedAt: snapshot.UpdatedAt}
	}
	if snapshot.ID != uuid.Nil {
		record.Fields["local_id"] = contactsync.FieldState{IsSet: true, Value: snapshot.ID.String(), UpdatedAt: snapshot.UpdatedAt}
	}
	record.Fields["custom_fields"] = contactsync.FieldState{IsSet: true, Value: cloneCustomFields(snapshot.CustomFields), UpdatedAt: snapshot.UpdatedAt}
	record.Fields["labels"] = contactsync.FieldState{IsSet: true, Value: append([]string(nil), snapshot.Labels...), UpdatedAt: snapshot.UpdatedAt}
	record.Fields["is_favorite"] = contactsync.FieldState{IsSet: true, Value: snapshot.IsFavorite, UpdatedAt: snapshot.UpdatedAt}
	return record
}

func personToRecord(p person.Person, externalID string) contactsync.Record {
	record := contactsync.Record{
		ExternalID: externalID,
		Tombstone: contactsync.Tombstone{
			Deleted:   p.DeletedAt != nil,
			UpdatedAt: p.UpdatedAt,
		},
		Fields: map[string]contactsync.FieldState{
			"first_name":    {IsSet: true, Value: p.FirstName, UpdatedAt: p.UpdatedAt},
			"middle_names":  {IsSet: true, Value: append([]string(nil), p.MiddleNames...), UpdatedAt: p.UpdatedAt},
			"last_name":     {IsSet: true, Value: p.LastName, UpdatedAt: p.UpdatedAt},
			"phone_numbers": {IsSet: true, Value: append([]contactsync.LabeledValue(nil), p.PhoneNumbers...), UpdatedAt: p.UpdatedAt},
			"emails":        {IsSet: true, Value: append([]contactsync.LabeledValue(nil), p.Emails...), UpdatedAt: p.UpdatedAt},
			"addresses":     {IsSet: true, Value: append([]contactsync.Address(nil), p.Addresses...), UpdatedAt: p.UpdatedAt},
			"local_id":      {IsSet: true, Value: p.ID.String(), UpdatedAt: p.UpdatedAt},
		},
	}
	if p.Organization != nil {
		record.Fields["organization"] = contactsync.FieldState{IsSet: true, Value: p.Organization, UpdatedAt: p.UpdatedAt}
	}
	if p.Notes != nil {
		record.Fields["notes"] = contactsync.FieldState{IsSet: true, Value: *p.Notes, UpdatedAt: p.UpdatedAt}
	}
	if p.Nickname != nil {
		record.Fields["nickname"] = contactsync.FieldState{IsSet: true, Value: *p.Nickname, UpdatedAt: p.UpdatedAt}
	}
	if p.Pronouns != nil {
		record.Fields["pronouns"] = contactsync.FieldState{IsSet: true, Value: *p.Pronouns, UpdatedAt: p.UpdatedAt}
	}
	if p.Birthdate != nil {
		record.Fields["birthdate"] = contactsync.FieldState{IsSet: true, Value: *p.Birthdate, UpdatedAt: p.UpdatedAt}
	}
	record.Fields["custom_fields"] = contactsync.FieldState{IsSet: true, Value: cloneCustomFields(p.CustomFields), UpdatedAt: p.UpdatedAt}
	record.Fields["labels"] = contactsync.FieldState{IsSet: true, Value: append([]string(nil), p.Labels...), UpdatedAt: p.UpdatedAt}
	record.Fields["is_favorite"] = contactsync.FieldState{IsSet: true, Value: p.IsFavorite, UpdatedAt: p.UpdatedAt}
	return record
}

func remoteToLocal(record contactsync.ProviderRecord) (person.CreateInput, *uuid.UUID) {
	firstName := fieldString(record.Record.Fields, "first_name")
	middleNames := fieldStrings(record.Record.Fields, "middle_names")
	lastName := fieldString(record.Record.Fields, "last_name")
	phoneNumbers := fieldLabeledValues(record.Record.Fields, "phone_numbers")
	emails := fieldLabeledValues(record.Record.Fields, "emails")
	addresses := fieldAddresses(record.Record.Fields, "addresses")
	if firstName == "" {
		firstName = "Unknown"
	}
	if lastName == "" {
		lastName = "Unknown"
	}
	create := person.CreateInput{
		FirstName:    firstName,
		MiddleNames:  middleNames,
		LastName:     lastName,
		PhoneNumbers: normalizeLabeledValues(phoneNumbers, 10, 50),
		Emails:       normalizeLabeledValues(emails, 10, 254),
		Addresses:    addresses,
		Organization: fieldOrganization(record.Record.Fields, "organization"),
		CustomFields: person.SanitizeCustomFieldsForSync(fieldCustomFields(record.Record.Fields, "custom_fields")),
		Labels:       person.SanitizeLabelsForSync(fieldStrings(record.Record.Fields, "labels")),
		IsFavorite:   fieldBool(record.Record.Fields, "is_favorite"),
	}
	if notes := fieldString(record.Record.Fields, "notes"); notes != "" {
		create.Notes = &notes
	}
	if nickname := fieldString(record.Record.Fields, "nickname"); nickname != "" {
		create.Nickname = &nickname
	}
	if pronouns := fieldString(record.Record.Fields, "pronouns"); pronouns != "" {
		create.Pronouns = &pronouns
	}
	if birthdate := fieldString(record.Record.Fields, "birthdate"); birthdate != "" {
		create.Birthdate = &birthdate
	}
	localIDRaw := fieldString(record.Record.Fields, "local_id")
	if localIDRaw == "" {
		return create, nil
	}
	parsed, err := uuid.Parse(localIDRaw)
	if err != nil {
		return create, nil
	}
	return create, &parsed
}

func extractUpdatedAt(record contactsync.ProviderRecord) time.Time {
	updated := fieldString(record.Record.Fields, remoteUpdatedFieldKey)
	if updated == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func stringPtr(v string) *string {
	s := v
	return &s
}

// googlePersonFields and googleUpdatePersonFields list the People API field
// masks we read and write, respectively. Keep these in sync with the mapping
// logic in toProviderRecord/toGooglePerson below.
const (
	googlePersonFields = "names,nicknames,emailAddresses,phoneNumbers,addresses,organizations,biographies,birthdays,relations,metadata,userDefined,memberships"
	// memberships is deliberately excluded here: Google rejects an
	// updateContact call that includes memberships in the update mask when
	// it would leave the contact with zero memberships (e.g. clearing every
	// label), so group membership is always managed separately via
	// contactGroups.members.modify instead (see reconcileMemberships).
	googleUpdatePersonFields = "names,nicknames,emailAddresses,phoneNumbers,addresses,organizations,biographies,birthdays,relations,userDefined"
)

func fieldString(fields map[string]contactsync.FieldState, key string) string {
	field, ok := fields[key]
	if !ok || !field.IsSet || field.Value == nil {
		return ""
	}
	value, _ := field.Value.(string)
	return strings.TrimSpace(value)
}

func fieldStrings(fields map[string]contactsync.FieldState, key string) []string {
	field, ok := fields[key]
	if !ok || !field.IsSet || field.Value == nil {
		return []string{}
	}
	switch values := field.Value.(type) {
	case []string:
		// append([]string(nil), values...) would stay nil when values is
		// empty, and that nil later flows into a NOT NULL DB column via
		// person.UpdateInput - always return a non-nil slice instead.
		out := make([]string, len(values))
		copy(out, values)
		return out
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if s, ok := value.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return []string{}
	}
}

// fieldLabeledValues returns a defensive copy of a []contactsync.LabeledValue
// field (e.g. emails or phone_numbers), or an empty (never nil) slice.
func fieldLabeledValues(fields map[string]contactsync.FieldState, key string) []contactsync.LabeledValue {
	field, ok := fields[key]
	if !ok || !field.IsSet || field.Value == nil {
		return []contactsync.LabeledValue{}
	}
	values, ok := field.Value.([]contactsync.LabeledValue)
	if !ok {
		return []contactsync.LabeledValue{}
	}
	out := make([]contactsync.LabeledValue, len(values))
	copy(out, values)
	return out
}

// fieldAddresses returns a defensive copy of a []contactsync.Address field, or
// an empty (never nil) slice.
func fieldAddresses(fields map[string]contactsync.FieldState, key string) []contactsync.Address {
	field, ok := fields[key]
	if !ok || !field.IsSet || field.Value == nil {
		return []contactsync.Address{}
	}
	values, ok := field.Value.([]contactsync.Address)
	if !ok {
		return []contactsync.Address{}
	}
	out := make([]contactsync.Address, len(values))
	copy(out, values)
	return out
}

// fieldOrganization returns the *contactsync.Organization stored in a field,
// or nil if unset.
func fieldOrganization(fields map[string]contactsync.FieldState, key string) *contactsync.Organization {
	field, ok := fields[key]
	if !ok || !field.IsSet || field.Value == nil {
		return nil
	}
	org, ok := field.Value.(*contactsync.Organization)
	if !ok {
		return nil
	}
	return org
}

// fieldCustomFields returns the map[string]any stored in a field, or an
// empty (never nil) map.
func fieldCustomFields(fields map[string]contactsync.FieldState, key string) map[string]any {
	field, ok := fields[key]
	if !ok || !field.IsSet || field.Value == nil {
		return map[string]any{}
	}
	values, ok := field.Value.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return values
}

// cloneCustomFields returns a shallow defensive copy, never nil.
func cloneCustomFields(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// stringifyCustomFieldValue renders a custom field's value as plain text for
// Google's userDefined field, which has no concept of types. ok is false for
// a value type we don't support (shouldn't occur given our own validation,
// but this is data crossing a trust boundary).
func stringifyCustomFieldValue(v any) (string, bool) {
	switch value := v.(type) {
	case string:
		return value, true
	case bool:
		if value {
			return "true", true
		}
		return "false", true
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64), true
	case json.Number:
		return value.String(), true
	default:
		return "", false
	}
}

// sniffCustomFieldValue infers a typed value from a plain-text Google
// userDefined value (Google itself has no concept of types): an exact
// "true"/"false" becomes a bool, a cleanly-parseable finite number becomes a
// float64, otherwise it stays a string. This intentionally mirrors how a
// spreadsheet/CSV import would guess types from raw text. A value with
// leading zeros (e.g. a zip code stored as "02134") will be misread as a
// number and lose them on export - an accepted tradeoff of type-sniffing
// plain text rather than encoding the type explicitly.
func sniffCustomFieldValue(raw string) any {
	switch raw {
	case "true":
		return true
	case "false":
		return false
	}
	if n, err := strconv.ParseFloat(raw, 64); err == nil && !math.IsInf(n, 0) && !math.IsNaN(n) {
		return n
	}
	return raw
}

// normalizeLabeledValues trims/truncates and drops empty-valued entries from
// a slice of labeled values (emails, phone numbers), enforcing count and
// length caps. It never returns nil.
func normalizeLabeledValues(values []contactsync.LabeledValue, maxCount, maxValueLen int) []contactsync.LabeledValue {
	if len(values) > maxCount {
		values = values[:maxCount]
	}
	out := make([]contactsync.LabeledValue, 0, len(values))
	for _, v := range values {
		value := strings.TrimSpace(v.Value)
		if value == "" {
			continue
		}
		if len(value) > maxValueLen {
			value = value[:maxValueLen]
		}
		label := strings.TrimSpace(v.Label)
		if len(label) > 50 {
			label = label[:50]
		}
		out = append(out, contactsync.LabeledValue{Label: label, Value: value})
	}
	return out
}

type googleConnectionsResponse struct {
	Connections   []googlePerson `json:"connections"`
	NextPageToken string         `json:"nextPageToken"`
	NextSyncToken string         `json:"nextSyncToken"`
}

type googlePerson struct {
	ResourceName   string               `json:"resourceName"`
	ETag           string               `json:"etag"`
	Metadata       googleMetadata       `json:"metadata"`
	Names          []googleName         `json:"names"`
	Nicknames      []googleNickname     `json:"nicknames"`
	EmailAddresses []googleEmailAddress `json:"emailAddresses"`
	PhoneNumbers   []googlePhoneNumber  `json:"phoneNumbers"`
	Addresses      []googleAddress      `json:"addresses"`
	Organizations  []googleOrganization `json:"organizations"`
	Biographies    []googleBiography    `json:"biographies"`
	Birthdays      []googleBirthday     `json:"birthdays"`
	Relations      []googleRelation     `json:"relations"`
	UserDefined    []googleUserDefined  `json:"userDefined"`
	Memberships    []googleMembership   `json:"memberships,omitempty"`
}

type googleMetadata struct {
	Deleted bool           `json:"deleted"`
	Sources []googleSource `json:"sources"`
}

type googleSource struct {
	UpdateTime string `json:"updateTime"`
}

type googleName struct {
	DisplayName string `json:"displayName"`
	GivenName   string `json:"givenName"`
	MiddleName  string `json:"middleName"`
	FamilyName  string `json:"familyName"`
	Metadata    struct {
		Primary bool `json:"primary"`
	} `json:"metadata"`
}

type googlePhoneNumber struct {
	Value string `json:"value"`
	Type  string `json:"type,omitempty"`
}

type googleNickname struct {
	Value string `json:"value,omitempty"`
}

type googleEmailAddress struct {
	Value string `json:"value,omitempty"`
	Type  string `json:"type,omitempty"`
}

type googleAddress struct {
	StreetAddress string `json:"streetAddress,omitempty"`
	City          string `json:"city,omitempty"`
	Region        string `json:"region,omitempty"`
	PostalCode    string `json:"postalCode,omitempty"`
	Country       string `json:"country,omitempty"`
	Type          string `json:"type,omitempty"`
}

type googleOrganization struct {
	Name       string `json:"name,omitempty"`
	Title      string `json:"title,omitempty"`
	Department string `json:"department,omitempty"`
}

type googleBiography struct {
	Value       string `json:"value,omitempty"`
	ContentType string `json:"contentType,omitempty"`
}

type googleBirthday struct {
	Date *googleDate `json:"date,omitempty"`
}

type googleDate struct {
	Year  int `json:"year,omitempty"`
	Month int `json:"month,omitempty"`
	Day   int `json:"day,omitempty"`
}

type googleRelation struct {
	Person string `json:"person,omitempty"`
	Type   string `json:"type,omitempty"`
}

type googleUserDefined struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// googleMembership and googleContactGroupMembership mirror the People API's
// Person.memberships[].contactGroupMembership shape - the read-side view of
// which contactGroups (Google's "Labels") a contact belongs to. Membership
// changes are never written back through this struct (see
// googleUpdatePersonFields); they go through contactGroups.members.modify
// instead (see reconcileMemberships).
type googleMembership struct {
	ContactGroupMembership *googleContactGroupMembership `json:"contactGroupMembership,omitempty"`
}

type googleContactGroupMembership struct {
	ContactGroupResourceName string `json:"contactGroupResourceName,omitempty"`
}

// contactGroup is a minimal local view of a Google contactGroups resource.
type contactGroup struct {
	ResourceName string
	Name         string
	System       bool
}

// groupResolver caches one Google account's contact groups for the duration
// of a single sync run (see Adapter.groupCaches), so label <->
// contactGroups.resourceName lookups don't re-list groups - or risk
// creating duplicate groups - on every contact processed in that run.
// byName only ever holds user-created groups; system groups (myContacts,
// starred, and the deprecated family/friends/work, etc.) are never exposed
// as labels - starred is tracked separately via starredID and mapped to
// Person.IsFavorite instead.
type groupResolver struct {
	byResourceName map[string]contactGroup
	byName         map[string]contactGroup
	starredID      string
}

// googleContactGroupsResponse and googleContactGroup mirror the People
// API's contactGroups.list response shape.
type googleContactGroupsResponse struct {
	ContactGroups []googleContactGroup `json:"contactGroups"`
	NextPageToken string               `json:"nextPageToken"`
}

type googleContactGroup struct {
	ResourceName string `json:"resourceName"`
	Name         string `json:"name"`
	GroupType    string `json:"groupType"`
}

// googleRelationTypeMap maps Google's relation type strings to our fixed
// enum. Relation types we don't support (friend, relative, manager,
// assistant, referredBy, colleague, etc.) aren't listed here and are skipped
// on import, since custom relationship types aren't supported.
var googleRelationTypeMap = map[string]person.RelationType{
	"parent":          person.RelationParent,
	"mother":          person.RelationParent,
	"father":          person.RelationParent,
	"child":           person.RelationChild,
	"son":             person.RelationChild,
	"daughter":        person.RelationChild,
	"spouse":          person.RelationSpouse,
	"sibling":         person.RelationSibling,
	"brother":         person.RelationSibling,
	"sister":          person.RelationSibling,
	"partner":         person.RelationPartner,
	"domesticpartner": person.RelationPartner,
}

// mapGoogleRelationType maps a Google relation type string to our fixed
// enum, reporting false for unsupported types.
func mapGoogleRelationType(googleType string) (person.RelationType, bool) {
	t, ok := googleRelationTypeMap[strings.ToLower(strings.TrimSpace(googleType))]
	return t, ok
}

// dedupeLabeledValues drops exact (label, value) duplicates while preserving
// order. Google contacts can carry genuine duplicate entries within a single
// source (observed live: a contact with the same mobile number listed
// twice), which we don't want to keep re-importing verbatim.
func dedupeLabeledValues(values []contactsync.LabeledValue) []contactsync.LabeledValue {
	seen := make(map[contactsync.LabeledValue]struct{}, len(values))
	out := make([]contactsync.LabeledValue, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// dedupeAddresses drops exact duplicate addresses while preserving order.
func dedupeAddresses(values []contactsync.Address) []contactsync.Address {
	seen := make(map[contactsync.Address]struct{}, len(values))
	out := make([]contactsync.Address, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// resolveMemberships translates a Google contact's raw group memberships
// into our provider-neutral shape: user-created group names become labels,
// and membership in the "starred" system group becomes is_favorite. Every
// other system group (myContacts, and the deprecated family/friends/work,
// etc.) is ignored. A nil resolver (e.g. a pure-function unit test that
// doesn't have a live Google account to list groups from) yields no labels
// and is_favorite=false rather than panicking.
func resolveMemberships(memberships []googleMembership, resolver *groupResolver) ([]string, bool) {
	if resolver == nil {
		return []string{}, false
	}
	labels := make([]string, 0, len(memberships))
	isFavorite := false
	for _, m := range memberships {
		if m.ContactGroupMembership == nil {
			continue
		}
		cg, ok := resolver.byResourceName[m.ContactGroupMembership.ContactGroupResourceName]
		if !ok {
			continue
		}
		if cg.System {
			if m.ContactGroupMembership.ContactGroupResourceName == resolver.starredID {
				isFavorite = true
			}
			continue
		}
		labels = append(labels, cg.Name)
	}
	sort.Strings(labels)
	return labels, isFavorite
}

func toProviderRecord(in googlePerson, resolver *groupResolver) contactsync.ProviderRecord {
	fields := map[string]contactsync.FieldState{}
	updatedAt := parseRemoteUpdatedAt(in.Metadata)
	name := selectName(in.Names)
	fields["first_name"] = contactsync.FieldState{IsSet: name.GivenName != "", Value: name.GivenName, UpdatedAt: updatedAt}
	middle := []string{}
	if trimmed := strings.TrimSpace(name.MiddleName); trimmed != "" {
		// Google's People API has only one middleName string, unlike our
		// ordered middle_names array - split on whitespace so multiple local
		// middle names (joined the same way on export, see toGooglePerson)
		// survive a round trip instead of collapsing to just the first.
		middle = strings.Fields(trimmed)
	}
	fields["middle_names"] = contactsync.FieldState{IsSet: true, Value: middle, UpdatedAt: updatedAt}
	fields["last_name"] = contactsync.FieldState{IsSet: name.FamilyName != "", Value: name.FamilyName, UpdatedAt: updatedAt}
	phones := make([]contactsync.LabeledValue, 0, len(in.PhoneNumbers))
	for _, number := range in.PhoneNumbers {
		if strings.TrimSpace(number.Value) != "" {
			phones = append(phones, contactsync.LabeledValue{Label: number.Type, Value: number.Value})
		}
	}
	fields["phone_numbers"] = contactsync.FieldState{IsSet: true, Value: dedupeLabeledValues(normalizeLabeledValues(phones, 10, 50)), UpdatedAt: updatedAt}
	emails := make([]contactsync.LabeledValue, 0, len(in.EmailAddresses))
	for _, email := range in.EmailAddresses {
		if strings.TrimSpace(email.Value) != "" {
			emails = append(emails, contactsync.LabeledValue{Label: email.Type, Value: email.Value})
		}
	}
	fields["emails"] = contactsync.FieldState{IsSet: true, Value: dedupeLabeledValues(normalizeLabeledValues(emails, 10, 254)), UpdatedAt: updatedAt}
	addresses := make([]contactsync.Address, 0, len(in.Addresses))
	for _, addr := range in.Addresses {
		if strings.TrimSpace(addr.StreetAddress) == "" && strings.TrimSpace(addr.City) == "" &&
			strings.TrimSpace(addr.Region) == "" && strings.TrimSpace(addr.PostalCode) == "" && strings.TrimSpace(addr.Country) == "" {
			continue
		}
		addresses = append(addresses, contactsync.Address{
			Label:      addr.Type,
			Street:     addr.StreetAddress,
			City:       addr.City,
			Region:     addr.Region,
			PostalCode: addr.PostalCode,
			Country:    addr.Country,
		})
	}
	fields["addresses"] = contactsync.FieldState{IsSet: true, Value: dedupeAddresses(addresses), UpdatedAt: updatedAt}
	if len(in.Organizations) > 0 {
		org := in.Organizations[0]
		fields["organization"] = contactsync.FieldState{IsSet: true, Value: &contactsync.Organization{
			Name:       org.Name,
			Title:      org.Title,
			Department: org.Department,
		}, UpdatedAt: updatedAt}
	}
	if len(in.Biographies) > 0 {
		notes := in.Biographies[0].Value
		fields["notes"] = contactsync.FieldState{IsSet: true, Value: notes, UpdatedAt: updatedAt}
	}
	if len(in.Nicknames) > 0 && strings.TrimSpace(in.Nicknames[0].Value) != "" {
		fields["nickname"] = contactsync.FieldState{IsSet: true, Value: in.Nicknames[0].Value, UpdatedAt: updatedAt}
	}
	if birthdate := formatGoogleBirthday(in.Birthdays); birthdate != nil {
		fields["birthdate"] = contactsync.FieldState{IsSet: true, Value: *birthdate, UpdatedAt: updatedAt}
	}
	relations := make([]contactsync.LabeledValue, 0, len(in.Relations))
	for _, rel := range in.Relations {
		name := strings.TrimSpace(rel.Person)
		if name == "" {
			continue
		}
		if t, ok := mapGoogleRelationType(rel.Type); ok {
			relations = append(relations, contactsync.LabeledValue{Label: string(t), Value: name})
		}
	}
	fields["relations"] = contactsync.FieldState{IsSet: true, Value: relations, UpdatedAt: updatedAt}
	fields[remoteUpdatedFieldKey] = contactsync.FieldState{IsSet: true, Value: updatedAt.Format(time.RFC3339Nano), UpdatedAt: updatedAt}
	customFields := map[string]any{}
	localIDSeen := false
	localIDAmbiguous := false
	for _, item := range in.UserDefined {
		if item.Key == localIDUserDefinedKey {
			// Google enforces no uniqueness on userDefined labels - a second
			// custom field also labeled "contacts_local_id" (e.g. hand-added
			// via the Google Contacts UI) is possible. Picking either one
			// arbitrarily risks merging into an unrelated existing Person,
			// so an ambiguous tag is treated the same as no tag at all,
			// falling back to the link table / exact-name match instead.
			if localIDSeen {
				localIDAmbiguous = true
			}
			localIDSeen = true
			if !localIDAmbiguous {
				fields["local_id"] = contactsync.FieldState{IsSet: true, Value: item.Value, UpdatedAt: updatedAt}
			}
			continue
		}
		if item.Key == pronounsUserDefinedKey {
			if value := strings.TrimSpace(item.Value); value != "" {
				fields["pronouns"] = contactsync.FieldState{IsSet: true, Value: value, UpdatedAt: updatedAt}
			}
			continue
		}
		if key := strings.TrimSpace(item.Key); key != "" {
			customFields[key] = sniffCustomFieldValue(item.Value)
		}
	}
	if localIDAmbiguous {
		delete(fields, "local_id")
	}
	fields["custom_fields"] = contactsync.FieldState{IsSet: true, Value: person.SanitizeCustomFieldsForSync(customFields), UpdatedAt: updatedAt}
	labels, isFavorite := resolveMemberships(in.Memberships, resolver)
	fields["labels"] = contactsync.FieldState{IsSet: true, Value: labels, UpdatedAt: updatedAt}
	fields["is_favorite"] = contactsync.FieldState{IsSet: true, Value: isFavorite, UpdatedAt: updatedAt}
	return contactsync.ProviderRecord{
		Record: contactsync.Record{
			ExternalID: in.ResourceName,
			Tombstone: contactsync.Tombstone{
				Deleted:   in.Metadata.Deleted,
				UpdatedAt: updatedAt,
			},
			Fields: fields,
		},
		ETag: in.ETag,
	}
}

func toGooglePerson(record contactsync.Record, etag string) googlePerson {
	firstName := fieldString(record.Fields, "first_name")
	middleNames := fieldStrings(record.Fields, "middle_names")
	lastName := fieldString(record.Fields, "last_name")
	if firstName == "" {
		firstName = "Unknown"
	}
	if lastName == "" {
		lastName = "Unknown"
	}
	middleName := ""
	if len(middleNames) > 0 {
		// Google's People API has only one middleName string - join every
		// local middle name into it (split back apart on import, see
		// toProviderRecord) instead of exporting just the first and silently
		// dropping the rest.
		middleName = strings.Join(middleNames, " ")
	}
	phones := normalizeLabeledValues(fieldLabeledValues(record.Fields, "phone_numbers"), 10, 50)
	phoneValues := make([]googlePhoneNumber, 0, len(phones))
	for _, number := range phones {
		phoneValues = append(phoneValues, googlePhoneNumber{Value: number.Value, Type: number.Label})
	}
	emails := normalizeLabeledValues(fieldLabeledValues(record.Fields, "emails"), 10, 254)
	emailValues := make([]googleEmailAddress, 0, len(emails))
	for _, email := range emails {
		emailValues = append(emailValues, googleEmailAddress{Value: email.Value, Type: email.Label})
	}
	addresses := fieldAddresses(record.Fields, "addresses")
	addressValues := make([]googleAddress, 0, len(addresses))
	for _, addr := range addresses {
		addressValues = append(addressValues, googleAddress{
			StreetAddress: addr.Street,
			City:          addr.City,
			Region:        addr.Region,
			PostalCode:    addr.PostalCode,
			Country:       addr.Country,
			Type:          addr.Label,
		})
	}
	var organizations []googleOrganization
	if org := fieldOrganization(record.Fields, "organization"); org != nil {
		organizations = []googleOrganization{{Name: org.Name, Title: org.Title, Department: org.Department}}
	}
	var biographies []googleBiography
	if notes := fieldString(record.Fields, "notes"); notes != "" {
		biographies = []googleBiography{{Value: notes, ContentType: "TEXT_PLAIN"}}
	}
	var nicknames []googleNickname
	if nickname := fieldString(record.Fields, "nickname"); nickname != "" {
		nicknames = []googleNickname{{Value: nickname}}
	}
	var birthdate *string
	if value := fieldString(record.Fields, "birthdate"); value != "" {
		birthdate = &value
	}
	userDefined := []googleUserDefined{}
	for key, value := range fieldCustomFields(record.Fields, "custom_fields") {
		key = strings.TrimSpace(key)
		if key == "" || key == localIDUserDefinedKey || key == pronounsUserDefinedKey {
			continue
		}
		if str, ok := stringifyCustomFieldValue(value); ok {
			userDefined = append(userDefined, googleUserDefined{Key: key, Value: str})
		}
	}
	sort.Slice(userDefined, func(i, j int) bool { return userDefined[i].Key < userDefined[j].Key })
	localID := fieldString(record.Fields, "local_id")
	if localID != "" {
		userDefined = append(userDefined, googleUserDefined{Key: localIDUserDefinedKey, Value: localID})
	}
	if pronouns := fieldString(record.Fields, "pronouns"); pronouns != "" {
		userDefined = append(userDefined, googleUserDefined{Key: pronounsUserDefinedKey, Value: pronouns})
	}
	relations := fieldLabeledValues(record.Fields, "relations")
	relationValues := make([]googleRelation, 0, len(relations))
	for _, rel := range relations {
		relationValues = append(relationValues, googleRelation{Person: rel.Value, Type: rel.Label})
	}
	return googlePerson{
		ResourceName: record.ExternalID,
		ETag:         etag,
		Names: []googleName{{
			GivenName:  firstName,
			MiddleName: middleName,
			FamilyName: lastName,
		}},
		Nicknames:      nicknames,
		EmailAddresses: emailValues,
		PhoneNumbers:   phoneValues,
		Addresses:      addressValues,
		Organizations:  organizations,
		Biographies:    biographies,
		Birthdays:      toGoogleBirthday(birthdate),
		Relations:      relationValues,
		UserDefined:    userDefined,
	}
}

// formatGoogleBirthday returns the first fully-specified (year/month/day)
// birthday as an ISO-8601 date string, or nil if none is present.
func formatGoogleBirthday(birthdays []googleBirthday) *string {
	for _, b := range birthdays {
		if b.Date == nil || b.Date.Year == 0 || b.Date.Month == 0 || b.Date.Day == 0 {
			continue
		}
		s := fmt.Sprintf("%04d-%02d-%02d", b.Date.Year, b.Date.Month, b.Date.Day)
		return &s
	}
	return nil
}

// toGoogleBirthday converts an ISO-8601 date string into the Google People
// API's structured birthday representation.
func toGoogleBirthday(birthdate *string) []googleBirthday {
	if birthdate == nil || strings.TrimSpace(*birthdate) == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", *birthdate)
	if err != nil {
		return nil
	}
	return []googleBirthday{{Date: &googleDate{Year: t.Year(), Month: int(t.Month()), Day: t.Day()}}}
}

func selectName(names []googleName) googleName {
	for _, name := range names {
		if name.Metadata.Primary {
			return name
		}
	}
	if len(names) == 0 {
		return googleName{}
	}
	return names[0]
}

func parseRemoteUpdatedAt(metadata googleMetadata) time.Time {
	timestamps := make([]time.Time, 0, len(metadata.Sources))
	for _, source := range metadata.Sources {
		parsed, err := time.Parse(time.RFC3339Nano, source.UpdateTime)
		if err == nil {
			timestamps = append(timestamps, parsed.UTC())
		}
	}
	if len(timestamps) == 0 {
		return time.Now().UTC()
	}
	slices.SortFunc(timestamps, func(a, b time.Time) int {
		switch {
		case a.Before(b):
			return -1
		case a.After(b):
			return 1
		default:
			return 0
		}
	})
	return timestamps[len(timestamps)-1]
}

func (a *Adapter) getContact(ctx context.Context, token string, resource string) (googlePerson, error) {
	resource = strings.TrimPrefix(resource, "/")
	values := url.Values{}
	values.Set("personFields", googlePersonFields)
	var out googlePerson
	if err := a.getJSON(ctx, token, "/"+resource+"?"+values.Encode(), &out); err != nil {
		return googlePerson{}, err
	}
	return out, nil
}

type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	if e.Body == "" {
		return "google api error: status " + strconv.Itoa(e.Status)
	}
	return "google api error: status " + strconv.Itoa(e.Status) + ": " + e.Body
}

func (a *Adapter) getJSON(ctx context.Context, accessToken string, path string, out any) error {
	req, err := a.newRequest(ctx, http.MethodGet, path, accessToken, nil)
	if err != nil {
		return err
	}
	return a.do(req, out)
}

func (a *Adapter) postJSON(ctx context.Context, accessToken string, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshalling google request body: %w", err)
	}
	req, err := a.newRequest(ctx, http.MethodPost, path, accessToken, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return a.do(req, out)
}

func (a *Adapter) patchJSON(ctx context.Context, accessToken string, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshalling google request body: %w", err)
	}
	req, err := a.newRequest(ctx, http.MethodPatch, path, accessToken, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return a.do(req, out)
}

func (a *Adapter) delete(ctx context.Context, accessToken string, path string) error {
	req, err := a.newRequest(ctx, http.MethodDelete, path, accessToken, nil)
	if err != nil {
		return err
	}
	return a.do(req, nil)
}

func (a *Adapter) newRequest(ctx context.Context, method string, path string, accessToken string, body io.Reader) (*http.Request, error) {
	u := strings.TrimRight(a.cfg.PeopleBaseURL, "/") + "/" + strings.TrimPrefix(path, "/")
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, fmt.Errorf("creating google api request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (a *Adapter) do(req *http.Request, out any) error {
	// 6 attempts with backoff capped at 30s (1,2,4,8,16,30 ~ 61s of waiting)
	// so a "Critical read requests" per-minute quota - which resets a full
	// 60s after its own window started, not after our first attempt - has
	// a real chance to clear before we give up, instead of exhausting a
	// much shorter retry budget and failing the whole sync run.
	const maxAttempts = 6
	const maxBackoff = 30 * time.Second
	backoff := time.Second
	for attempt := 1; ; attempt++ {
		if attempt > 1 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return fmt.Errorf("rewinding google api request body for retry: %w", err)
			}
			req.Body = body
		}
		resp, err := a.http.Do(req)
		if err != nil {
			return fmt.Errorf("google api request failed: %w", err)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("reading google api response: %w", readErr)
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxAttempts {
			wait := retryDelay(resp.Header.Get("Retry-After"), body, backoff)
			a.logger.Warn("google api rate limited, retrying", "attempt", attempt, "wait", wait.String())
			select {
			case <-req.Context().Done():
				return req.Context().Err()
			case <-time.After(wait):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return &apiError{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
		}
		if out == nil || len(body) == 0 {
			return nil
		}
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("decoding google api response: %w", err)
		}
		return nil
	}
}

// retryDelay picks how long to wait before retrying a 429: the
// server-specified Retry-After header wins if present, then a per-minute
// quota window reset time parsed from the error body (see
// quotaWindowResetWait), falling back to our own exponential backoff.
func retryDelay(retryAfter string, body []byte, backoff time.Duration) time.Duration {
	if secs, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if wait, ok := quotaWindowResetWait(body); ok {
		return wait
	}
	return backoff
}

// quotaWindowResetWait parses a Google RESOURCE_EXHAUSTED error body's
// quota_location/window_start_time metadata - present on per-minute quota
// errors like "Critical read requests ... per minute per user" - and
// returns how long until that 60-second window resets. Our own exponential
// backoff alone tops out well under a minute and can retry right back into
// the same still-exhausted window; Google doesn't send a Retry-After header
// for this error, but does tell us exactly when the window it's counting
// against started.
func quotaWindowResetWait(body []byte) (time.Duration, bool) {
	var parsed struct {
		Error struct {
			Details []struct {
				Metadata struct {
					WindowStartTime string `json:"window_start_time"`
				} `json:"metadata"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return 0, false
	}
	for _, d := range parsed.Error.Details {
		secs, err := strconv.ParseInt(strings.TrimSpace(d.Metadata.WindowStartTime), 10, 64)
		if err != nil {
			continue
		}
		// A couple seconds of slack for clock skew between us and Google,
		// capped so a bogus/far-future timestamp can't stall a sync run.
		wait := time.Until(time.Unix(secs, 0).Add(time.Minute)) + 2*time.Second
		if wait <= 0 {
			continue
		}
		if wait > 90*time.Second {
			wait = 90 * time.Second
		}
		return wait, true
	}
	return 0, false
}

// isETagConflict reports whether err is Google's "person.etag is different
// than the current person.etag" FAILED_PRECONDITION response, which happens
// when the contact changed remotely between our read of its etag and our
// update using it - retryable by re-reading the etag and trying once more.
func isETagConflict(err error) bool {
	var apiErr *apiError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusBadRequest && strings.Contains(apiErr.Body, "FAILED_PRECONDITION")
}

// groupResolverFor returns the cached groupResolver for session's Google
// account, loading it (a full contactGroups.list) on first use in this sync
// run. Cached by AuthSession.ProviderAccountID rather than our internal
// account UUID, since every adapter method already carries a session but
// not an account ID.
func (a *Adapter) groupResolverFor(ctx context.Context, session contactsync.AuthSession) (*groupResolver, error) {
	key := session.ProviderAccountID
	if key != "" {
		if cached, ok := a.groupCaches.Load(key); ok {
			return cached.(*groupResolver), nil
		}
	}
	resolver, err := a.loadGroupResolver(ctx, session.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("listing google contact groups: %w", err)
	}
	if key != "" {
		a.groupCaches.Store(key, resolver)
	}
	return resolver, nil
}

// loadGroupResolver lists every contact group on the account (paginated) and
// indexes it for lookup by name (user-created groups only) and by
// resourceName (all groups, so an unrecognized/unmanaged membership can
// still be identified and left alone).
func (a *Adapter) loadGroupResolver(ctx context.Context, accessToken string) (*groupResolver, error) {
	resolver := &groupResolver{byResourceName: map[string]contactGroup{}, byName: map[string]contactGroup{}}
	pageToken := ""
	for {
		values := url.Values{}
		values.Set("pageSize", "1000")
		if pageToken != "" {
			values.Set("pageToken", pageToken)
		}
		var page googleContactGroupsResponse
		if err := a.getJSON(ctx, accessToken, "/contactGroups?"+values.Encode(), &page); err != nil {
			return nil, err
		}
		for _, g := range page.ContactGroups {
			cg := contactGroup{ResourceName: g.ResourceName, Name: g.Name, System: g.GroupType == "SYSTEM_CONTACT_GROUP"}
			resolver.byResourceName[cg.ResourceName] = cg
			if cg.System {
				if cg.Name == "starred" {
					resolver.starredID = cg.ResourceName
				}
				continue // system groups other than starred are never exposed as labels
			}
			resolver.byName[cg.Name] = cg
		}
		if strings.TrimSpace(page.NextPageToken) == "" {
			break
		}
		pageToken = page.NextPageToken
	}
	return resolver, nil
}

// refreshGroupResolver re-lists every group and replaces resolver's cached
// state in place, for the rare case a create races with a group that
// appeared since the last list (see ensureLabelGroups).
func (a *Adapter) refreshGroupResolver(ctx context.Context, accessToken string, resolver *groupResolver) error {
	fresh, err := a.loadGroupResolver(ctx, accessToken)
	if err != nil {
		return err
	}
	resolver.byResourceName = fresh.byResourceName
	resolver.byName = fresh.byName
	resolver.starredID = fresh.starredID
	return nil
}

// ensureLabelGroups resolves each label name to its Google contactGroups
// resourceName, creating a new user-created group for any label the
// resolver hasn't seen before.
func (a *Adapter) ensureLabelGroups(ctx context.Context, accessToken string, resolver *groupResolver, labels []string) ([]string, error) {
	resourceNames := make([]string, 0, len(labels))
	for _, name := range labels {
		if cg, ok := resolver.byName[name]; ok {
			resourceNames = append(resourceNames, cg.ResourceName)
			continue
		}
		created, err := a.createContactGroup(ctx, accessToken, name)
		if err != nil {
			var apiErr *apiError
			if errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict {
				// A group with this name already exists (created elsewhere,
				// e.g. directly in Google Contacts, since our last list) -
				// refresh once and use it instead of failing the record.
				if refreshErr := a.refreshGroupResolver(ctx, accessToken, resolver); refreshErr == nil {
					if cg, ok := resolver.byName[name]; ok {
						resourceNames = append(resourceNames, cg.ResourceName)
						continue
					}
				}
			}
			return nil, fmt.Errorf("creating google contact group %q: %w", name, err)
		}
		resolver.byName[name] = created
		resolver.byResourceName[created.ResourceName] = created
		resourceNames = append(resourceNames, created.ResourceName)
	}
	return resourceNames, nil
}

func (a *Adapter) createContactGroup(ctx context.Context, accessToken, name string) (contactGroup, error) {
	body := map[string]any{"contactGroup": map[string]string{"name": name}}
	var created googleContactGroup
	if err := a.postJSON(ctx, accessToken, "/contactGroups", body, &created); err != nil {
		return contactGroup{}, err
	}
	return contactGroup{ResourceName: created.ResourceName, Name: created.Name}, nil
}

// modifyGroupMembers adds and/or removes one contact from one contact group.
func (a *Adapter) modifyGroupMembers(ctx context.Context, accessToken, groupResourceName string, toAdd, toRemove []string) error {
	if len(toAdd) == 0 && len(toRemove) == 0 {
		return nil
	}
	body := map[string]any{}
	if len(toAdd) > 0 {
		body["resourceNamesToAdd"] = toAdd
	}
	if len(toRemove) > 0 {
		body["resourceNamesToRemove"] = toRemove
	}
	var out any
	return a.postJSON(ctx, accessToken, "/"+groupResourceName+"/members:modify", body, &out)
}

// reconcileMemberships makes personResourceName's actual Google group
// memberships match record's labels/is_favorite, by adding/removing
// membership only in the groups that changed (see current, the contact's
// state as last read from Google). System groups other than starred (e.g.
// myContacts) are never touched, and a label removed locally only has its
// membership removed - the underlying Google group itself is never deleted.
func (a *Adapter) reconcileMemberships(ctx context.Context, accessToken string, resolver *groupResolver, personResourceName string, current googlePerson, record contactsync.Record) error {
	labels := person.SanitizeLabelsForSync(fieldStrings(record.Fields, "labels"))
	resourceNames, err := a.ensureLabelGroups(ctx, accessToken, resolver, labels)
	if err != nil {
		return err
	}
	desired := make(map[string]bool, len(resourceNames)+1)
	for _, rn := range resourceNames {
		desired[rn] = true
	}
	if fieldBool(record.Fields, "is_favorite") && resolver.starredID != "" {
		desired[resolver.starredID] = true
	}

	existing := map[string]bool{}
	for _, m := range current.Memberships {
		if m.ContactGroupMembership == nil {
			continue
		}
		rn := m.ContactGroupMembership.ContactGroupResourceName
		cg, ok := resolver.byResourceName[rn]
		if !ok || (cg.System && rn != resolver.starredID) {
			continue
		}
		existing[rn] = true
	}

	for rn := range desired {
		if !existing[rn] {
			if err := a.modifyGroupMembers(ctx, accessToken, rn, []string{personResourceName}, nil); err != nil {
				return fmt.Errorf("adding contact to google group: %w", err)
			}
		}
	}
	for rn := range existing {
		if !desired[rn] {
			if err := a.modifyGroupMembers(ctx, accessToken, rn, nil, []string{personResourceName}); err != nil {
				return fmt.Errorf("removing contact from google group: %w", err)
			}
		}
	}
	return nil
}

// fieldBool returns the bool stored in a field, or false if unset/wrong type.
func fieldBool(fields map[string]contactsync.FieldState, key string) bool {
	field, ok := fields[key]
	if !ok || !field.IsSet || field.Value == nil {
		return false
	}
	v, _ := field.Value.(bool)
	return v
}
