package google

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/scottfridlund/contacts/backend/internal/contactsync"
	"github.com/scottfridlund/contacts/backend/internal/person"
)

const (
	localIDUserDefinedKey = "contacts_local_id"
	remoteUpdatedFieldKey = "_google_updated_at"
	googleIssuer          = "https://accounts.google.com"
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
}

type accountStateStore interface {
	Update(context.Context, *contactsync.Account) (*contactsync.Account, error)
}

// recordLinkStore maps a local Person to its remote record on one specific
// sync account, so the same Person can be linked to several accounts (even
// several accounts of the same provider) without colliding on a shared field.
type recordLinkStore interface {
	Get(ctx context.Context, syncAccountID, personID uuid.UUID) (*contactsync.RecordLink, error)
	Upsert(ctx context.Context, link *contactsync.RecordLink) error
}

type personService interface {
	Get(context.Context, uuid.UUID) (*person.Person, error)
	Create(context.Context, person.CreateInput) (*person.Person, person.ValidationErrors, error)
	Update(context.Context, uuid.UUID, person.UpdateInput) (*person.Person, person.ValidationErrors, error)
	Delete(context.Context, uuid.UUID) error
	List(context.Context, person.ListParams) ([]person.Person, int, error)
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
func (a *Adapter) ListChanges(ctx context.Context, session contactsync.AuthSession, cursor string) (contactsync.ChangePage, error) {
	values := url.Values{}
	values.Set("personFields", googlePersonFields)
	values.Set("requestSyncToken", "true")
	if cursor != "" {
		values.Set("syncToken", cursor)
	}
	body := googleConnectionsResponse{}
	if err := a.getJSON(ctx, session.AccessToken, "/people/me/connections?"+values.Encode(), &body); err != nil {
		return contactsync.ChangePage{}, err
	}
	records := make([]contactsync.ProviderRecord, 0, len(body.Connections))
	for _, entry := range body.Connections {
		records = append(records, toProviderRecord(entry))
	}
	nextCursor := strings.TrimSpace(body.NextSyncToken)
	if nextCursor == "" {
		nextCursor = strings.TrimSpace(cursor)
	}
	if body.NextPageToken != "" {
		nextCursor = body.NextPageToken
	}
	return contactsync.ChangePage{
		Records:    records,
		NextCursor: nextCursor,
		HasMore:    body.NextPageToken != "",
	}, nil
}

// FetchRecord fetches one remote Google contact.
func (a *Adapter) FetchRecord(ctx context.Context, session contactsync.AuthSession, remoteID string) (contactsync.ProviderRecord, error) {
	personBody, err := a.getContact(ctx, session.AccessToken, remoteID)
	if err != nil {
		return contactsync.ProviderRecord{}, err
	}
	return toProviderRecord(personBody), nil
}

// UpsertRecord creates or updates one remote contact.
func (a *Adapter) UpsertRecord(ctx context.Context, session contactsync.AuthSession, record contactsync.Record) (contactsync.ProviderRecord, error) {
	if strings.TrimSpace(record.ExternalID) == "" {
		payload := toGooglePerson(record, "")
		var created googlePerson
		if err := a.postJSON(ctx, session.AccessToken, "/people:createContact", payload, &created); err != nil {
			return contactsync.ProviderRecord{}, err
		}
		return toProviderRecord(created), nil
	}
	current, err := a.getContact(ctx, session.AccessToken, record.ExternalID)
	if err != nil {
		return contactsync.ProviderRecord{}, err
	}
	payload := toGooglePerson(record, current.ETag)
	values := url.Values{}
	values.Set("updatePersonFields", googleUpdatePersonFields)
	var updated googlePerson
	if err := a.patchJSON(ctx, session.AccessToken, "/"+record.ExternalID+":updateContact?"+values.Encode(), payload, &updated); err != nil {
		return contactsync.ProviderRecord{}, err
	}
	return toProviderRecord(updated), nil
}

// DeleteRecord deletes one remote Google contact.
func (a *Adapter) DeleteRecord(ctx context.Context, session contactsync.AuthSession, remoteID string) error {
	if strings.TrimSpace(remoteID) == "" {
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
	a.logger.Info("google sync starting", "account_id", account.ID, "provider_account_id", account.ProviderAccountID, "has_job", job.PersonID != uuid.Nil)

	session, err := sessionFromAccount(account)
	if err != nil {
		return a.markAccountFailed(ctx, &account, err)
	}
	session, err = a.ensureSession(ctx, &account, session)
	if err != nil {
		return a.markAccountFailed(ctx, &account, err)
	}

	if job.PersonID != uuid.Nil {
		if err := a.syncLocalJob(ctx, account.ID, session, job); err != nil {
			return a.markAccountFailed(ctx, &account, err)
		}
	}

	initialSync := strings.TrimSpace(account.SyncCursor) == ""
	nextCursor, pulled, err := a.pullRemote(ctx, account.ID, session, account.SyncCursor)
	if err != nil {
		return a.markAccountFailed(ctx, &account, err)
	}
	account.SyncCursor = nextCursor

	if initialSync {
		if err := a.exportLocal(ctx, account.ID, session); err != nil {
			return a.markAccountFailed(ctx, &account, err)
		}
	}

	now := time.Now().UTC()
	account.LastSyncedAt = &now
	account.LastError = nil
	account.Status = "connected"
	if _, err := a.accounts.Update(ctx, &account); err != nil {
		return fmt.Errorf("updating sync account state: %w", err)
	}
	a.logger.Info("google sync finished", "account_id", account.ID, "remote_records_seen", pulled, "initial_sync", initialSync)
	return nil
}

// remoteIDFor returns the remote resource name already linked to a person on
// this specific sync account, or "" if none is known yet.
func (a *Adapter) remoteIDFor(ctx context.Context, accountID, personID uuid.UUID) string {
	link, err := a.links.Get(ctx, accountID, personID)
	if err != nil {
		return ""
	}
	return link.RemoteID
}

// linkRecord records (or updates) which remote resource a person maps to on
// this specific sync account.
func (a *Adapter) linkRecord(ctx context.Context, accountID, personID uuid.UUID, remoteID string) {
	if personID == uuid.Nil || strings.TrimSpace(remoteID) == "" {
		return
	}
	_ = a.links.Upsert(ctx, &contactsync.RecordLink{
		SyncAccountID: accountID,
		PersonID:      personID,
		RemoteID:      remoteID,
	})
}

func (a *Adapter) syncLocalJob(ctx context.Context, accountID uuid.UUID, session contactsync.AuthSession, job contactsync.Job) error {
	remoteID := a.remoteIDFor(ctx, accountID, job.PersonID)
	if job.Kind == contactsync.ChangeKindDeleted || job.Kind == contactsync.ChangeKindHardDeleted {
		return a.DeleteRecord(ctx, session, remoteID)
	}
	record := snapshotToRecord(job.Snapshot)
	record.ExternalID = remoteID
	providerRecord, err := a.UpsertRecord(ctx, session, record)
	if err != nil {
		return err
	}
	a.linkRecord(ctx, accountID, job.PersonID, providerRecord.Record.ExternalID)
	return nil
}

func (a *Adapter) pullRemote(ctx context.Context, accountID uuid.UUID, session contactsync.AuthSession, cursor string) (string, int, error) {
	current := cursor
	seen := 0
	for {
		page, err := a.ListChanges(ctx, session, current)
		if err != nil {
			var apiErr *apiError
			if errors.As(err, &apiErr) && apiErr.Status == http.StatusGone && current != "" {
				a.logger.Info("google sync token expired, restarting full pull", "account_id", accountID)
				current = ""
				continue
			}
			return cursor, seen, fmt.Errorf("listing google contact changes: %w", err)
		}
		a.logger.Info("google sync fetched remote page", "account_id", accountID, "records", len(page.Records), "has_more", page.HasMore)
		seen += len(page.Records)
		for _, remote := range page.Records {
			if err := a.mergeRemoteRecord(ctx, accountID, session, remote); err != nil {
				return cursor, seen, err
			}
		}
		current = page.NextCursor
		if !page.HasMore {
			break
		}
	}
	return current, seen, nil
}

func (a *Adapter) exportLocal(ctx context.Context, accountID uuid.UUID, session contactsync.AuthSession) error {
	page := 1
	exported := 0
	for {
		rows, total, err := a.people.List(ctx, person.ListParams{Page: page, PageSize: 100, SortField: "updated_at", SortDesc: false})
		if err != nil {
			return fmt.Errorf("listing local persons for export: %w", err)
		}
		for i := range rows {
			remoteID := a.remoteIDFor(ctx, accountID, rows[i].ID)
			record := personToRecord(rows[i], remoteID)
			providerRecord, upsertErr := a.UpsertRecord(ctx, session, record)
			if upsertErr != nil {
				return upsertErr
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

func (a *Adapter) mergeRemoteRecord(ctx context.Context, accountID uuid.UUID, session contactsync.AuthSession, remote contactsync.ProviderRecord) error {
	remoteModel, localID := remoteToLocal(remote)
	if localID == nil {
		created, _, err := a.people.Create(contactsync.WithSyncOrigin(ctx), remoteModel)
		if err != nil {
			return fmt.Errorf("creating local person from google record %s: %w", remote.Record.ExternalID, err)
		}
		out, upsertErr := a.UpsertRecord(ctx, session, personToRecord(*created, remote.Record.ExternalID))
		if upsertErr != nil {
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
		created, _, createErr := a.people.Create(contactsync.WithSyncOrigin(ctx), remoteModel)
		if createErr != nil {
			return fmt.Errorf("creating local person for missing mapping: %w", createErr)
		}
		out, upsertErr := a.UpsertRecord(ctx, session, personToRecord(*created, remote.Record.ExternalID))
		if upsertErr != nil {
			return upsertErr
		}
		a.linkRecord(ctx, accountID, created.ID, out.Record.ExternalID)
		return nil
	}

	// The remote record's own resource name is now confirmed for this
	// (account, person) pair regardless of which branch below runs, so record
	// it opportunistically. This also self-heals the link table for contacts
	// that were synced before per-account link tracking existed.
	a.linkRecord(ctx, accountID, local.ID, remote.Record.ExternalID)

	remoteUpdatedAt := extractUpdatedAt(remote)
	if remote.Record.Tombstone.Deleted {
		if remoteUpdatedAt.After(local.UpdatedAt) {
			if err := a.people.Delete(contactsync.WithSyncOrigin(ctx), local.ID); err != nil && !errors.Is(err, person.ErrNotFound) {
				return fmt.Errorf("deleting local person %s: %w", local.ID.String(), err)
			}
		} else {
			_, upsertErr := a.UpsertRecord(ctx, session, personToRecord(*local, remote.Record.ExternalID))
			if upsertErr != nil {
				return upsertErr
			}
		}
		return nil
	}

	if remoteUpdatedAt.After(local.UpdatedAt) {
		update := person.UpdateInput{
			FirstName:       stringPtr(remoteModel.FirstName),
			FirstNameSet:    true,
			MiddleNames:     &remoteModel.MiddleNames,
			MiddleNamesSet:  true,
			LastName:        stringPtr(remoteModel.LastName),
			LastNameSet:     true,
			PhoneNumbers:    &remoteModel.PhoneNumbers,
			PhoneNumbersSet: true,
			Emails:          &remoteModel.Emails,
			EmailsSet:       true,
			Addresses:       &remoteModel.Addresses,
			AddressesSet:    true,
			Organization:    remoteModel.Organization,
			OrganizationSet: true,
			Notes:           remoteModel.Notes,
			NotesSet:        true,
		}
		if _, _, err := a.people.Update(contactsync.WithSyncOrigin(ctx), local.ID, update); err != nil {
			return fmt.Errorf("updating local person %s: %w", local.ID.String(), err)
		}
		return nil
	}

	if local.UpdatedAt.After(remoteUpdatedAt) {
		_, err := a.UpsertRecord(ctx, session, personToRecord(*local, remote.Record.ExternalID))
		return err
	}
	return nil
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
	if _, err := a.accounts.Update(ctx, account); err != nil {
		return contactsync.AuthSession{}, fmt.Errorf("persisting refreshed google token: %w", err)
	}
	return refreshed, nil
}

func (a *Adapter) markAccountFailed(ctx context.Context, account *contactsync.Account, syncErr error) error {
	message := syncErr.Error()
	account.LastError = &message
	account.Status = "reconnect_required"
	var apiErr *apiError
	if errors.As(syncErr, &apiErr) && apiErr.Status >= 500 {
		account.Status = "error"
	}
	a.logger.Error("google sync failed", "account_id", account.ID, "provider_account_id", account.ProviderAccountID, "status", account.Status, "error", syncErr)
	_, _ = a.accounts.Update(ctx, account)
	return syncErr
}

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
	if snapshot.Birthdate != nil {
		record.Fields["birthdate"] = contactsync.FieldState{IsSet: true, Value: *snapshot.Birthdate, UpdatedAt: snapshot.UpdatedAt}
	}
	if snapshot.ID != uuid.Nil {
		record.Fields["local_id"] = contactsync.FieldState{IsSet: true, Value: snapshot.ID.String(), UpdatedAt: snapshot.UpdatedAt}
	}
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
	if p.Birthdate != nil {
		record.Fields["birthdate"] = contactsync.FieldState{IsSet: true, Value: *p.Birthdate, UpdatedAt: p.UpdatedAt}
	}
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
	}
	if notes := fieldString(record.Record.Fields, "notes"); notes != "" {
		create.Notes = &notes
	}
	if nickname := fieldString(record.Record.Fields, "nickname"); nickname != "" {
		create.Nickname = &nickname
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
	googlePersonFields       = "names,nicknames,emailAddresses,phoneNumbers,addresses,organizations,biographies,birthdays,metadata,userDefined"
	googleUpdatePersonFields = "names,nicknames,emailAddresses,phoneNumbers,addresses,organizations,biographies,birthdays,userDefined"
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
	UserDefined    []googleUserDefined  `json:"userDefined"`
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

type googleUserDefined struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func toProviderRecord(in googlePerson) contactsync.ProviderRecord {
	fields := map[string]contactsync.FieldState{}
	updatedAt := parseRemoteUpdatedAt(in.Metadata)
	name := selectName(in.Names)
	fields["first_name"] = contactsync.FieldState{IsSet: name.GivenName != "", Value: name.GivenName, UpdatedAt: updatedAt}
	middle := []string{}
	if strings.TrimSpace(name.MiddleName) != "" {
		middle = []string{name.MiddleName}
	}
	fields["middle_names"] = contactsync.FieldState{IsSet: true, Value: middle, UpdatedAt: updatedAt}
	fields["last_name"] = contactsync.FieldState{IsSet: name.FamilyName != "", Value: name.FamilyName, UpdatedAt: updatedAt}
	phones := make([]contactsync.LabeledValue, 0, len(in.PhoneNumbers))
	for _, number := range in.PhoneNumbers {
		if strings.TrimSpace(number.Value) != "" {
			phones = append(phones, contactsync.LabeledValue{Label: number.Type, Value: number.Value})
		}
	}
	fields["phone_numbers"] = contactsync.FieldState{IsSet: true, Value: normalizeLabeledValues(phones, 10, 50), UpdatedAt: updatedAt}
	emails := make([]contactsync.LabeledValue, 0, len(in.EmailAddresses))
	for _, email := range in.EmailAddresses {
		if strings.TrimSpace(email.Value) != "" {
			emails = append(emails, contactsync.LabeledValue{Label: email.Type, Value: email.Value})
		}
	}
	fields["emails"] = contactsync.FieldState{IsSet: true, Value: normalizeLabeledValues(emails, 10, 254), UpdatedAt: updatedAt}
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
	fields["addresses"] = contactsync.FieldState{IsSet: true, Value: addresses, UpdatedAt: updatedAt}
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
	fields[remoteUpdatedFieldKey] = contactsync.FieldState{IsSet: true, Value: updatedAt.Format(time.RFC3339Nano), UpdatedAt: updatedAt}
	for _, item := range in.UserDefined {
		if item.Key == localIDUserDefinedKey {
			fields["local_id"] = contactsync.FieldState{IsSet: true, Value: item.Value, UpdatedAt: updatedAt}
			break
		}
	}
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
		middleName = middleNames[0]
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
	localID := fieldString(record.Fields, "local_id")
	if localID != "" {
		userDefined = append(userDefined, googleUserDefined{Key: localIDUserDefinedKey, Value: localID})
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
	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("google api request failed: %w", err)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return fmt.Errorf("reading google api response: %w", readErr)
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
