package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/scottfridlund/contacts/backend/internal/contactsync"
)

type syncAccountHandler struct {
	repo syncAccountStore
}

type syncAccountListResponse struct {
	Data []contactsync.Account `json:"data"`
}

type syncAccountCreateInput struct {
	Provider             string     `json:"provider"`
	ProviderAccountID    string     `json:"provider_account_id"`
	AccessToken          *string    `json:"access_token"`
	RefreshToken         *string    `json:"refresh_token"`
	ExpiresAt            *time.Time `json:"expires_at"`
	Scope                string     `json:"scope"`
	SyncCursor           string     `json:"sync_cursor"`
	SyncFrequencyMinutes *int       `json:"sync_frequency_minutes"`
	Status               string     `json:"status"`
}

type syncAccountUpdateInput struct {
	Provider                *string    `json:"provider"`
	ProviderAccountID       *string    `json:"provider_account_id"`
	AccessToken             *string    `json:"access_token"`
	RefreshToken            *string    `json:"refresh_token"`
	ExpiresAt               *time.Time `json:"expires_at"`
	Scope                   *string    `json:"scope"`
	SyncCursor              *string    `json:"sync_cursor"`
	SyncFrequencyMinutes    *int       `json:"sync_frequency_minutes"`
	Status                  *string    `json:"status"`
	ProviderSet             bool
	ProviderAccountIDSet    bool
	AccessTokenSet          bool
	RefreshTokenSet         bool
	ExpiresAtSet            bool
	ScopeSet                bool
	SyncCursorSet           bool
	SyncFrequencyMinutesSet bool
	StatusSet               bool
}

func (h *syncAccountHandler) list(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.repo.List(r.Context(), 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list sync accounts", nil)
		return
	}
	writeJSON(w, http.StatusOK, syncAccountListResponse{Data: accounts})
}

func (h *syncAccountHandler) create(w http.ResponseWriter, r *http.Request) {
	var in syncAccountCreateInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if strings.TrimSpace(in.Provider) == "" || strings.TrimSpace(in.ProviderAccountID) == "" {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "request validation failed", map[string]string{
			"provider":            "provider is required",
			"provider_account_id": "provider_account_id is required",
		})
		return
	}
	account := &contactsync.Account{
		Provider:          strings.TrimSpace(in.Provider),
		ProviderAccountID: strings.TrimSpace(in.ProviderAccountID),
		AccessToken:       in.AccessToken,
		RefreshToken:      in.RefreshToken,
		ExpiresAt:         in.ExpiresAt,
		Scope:             in.Scope,
		SyncCursor:        in.SyncCursor,
		Status:            in.Status,
	}
	if in.SyncFrequencyMinutes != nil {
		account.SyncFrequencyMinutes = *in.SyncFrequencyMinutes
	}
	created, err := h.repo.Create(r.Context(), account)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create sync account", nil)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *syncAccountHandler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSyncAccountID(w, r)
	if !ok {
		return
	}
	account, err := h.repo.GetByID(r.Context(), id)
	if errors.Is(err, contactsync.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "sync account not found", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to fetch sync account", nil)
		return
	}
	writeJSON(w, http.StatusOK, account)
}

func (h *syncAccountHandler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSyncAccountID(w, r)
	if !ok {
		return
	}
	in, err := decodeSyncAccountUpdate(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	current, err := h.repo.GetByID(r.Context(), id)
	if errors.Is(err, contactsync.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "sync account not found", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to fetch sync account", nil)
		return
	}
	applySyncAccountPatch(current, in)
	updated, err := h.repo.Update(r.Context(), current)
	if errors.Is(err, contactsync.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "sync account not found", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update sync account", nil)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *syncAccountHandler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSyncAccountID(w, r)
	if !ok {
		return
	}
	if err := h.repo.Delete(r.Context(), id); err != nil {
		if errors.Is(err, contactsync.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "sync account not found", nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to delete sync account", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseSyncAccountID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be a valid UUID", nil)
		return uuid.Nil, false
	}
	return id, true
}

func decodeSyncAccountUpdate(w http.ResponseWriter, r *http.Request) (syncAccountUpdateInput, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return syncAccountUpdateInput{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return syncAccountUpdateInput{}, errors.New("request body must be a JSON object")
	}
	allowed := map[string]struct{}{
		"provider": {}, "provider_account_id": {}, "access_token": {}, "refresh_token": {},
		"expires_at": {}, "scope": {}, "sync_cursor": {}, "sync_frequency_minutes": {}, "status": {},
	}
	for name := range fields {
		if _, ok := allowed[name]; !ok {
			return syncAccountUpdateInput{}, errors.New("unknown field: " + name)
		}
	}
	var out syncAccountUpdateInput
	if v, ok := fields["provider"]; ok {
		out.ProviderSet = true
		if string(v) != "null" {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return out, err
			}
			out.Provider = &s
		}
	}
	if v, ok := fields["provider_account_id"]; ok {
		out.ProviderAccountIDSet = true
		if string(v) != "null" {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return out, err
			}
			out.ProviderAccountID = &s
		}
	}
	if v, ok := fields["access_token"]; ok {
		out.AccessTokenSet = true
		if string(v) != "null" {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return out, err
			}
			out.AccessToken = &s
		}
	}
	if v, ok := fields["refresh_token"]; ok {
		out.RefreshTokenSet = true
		if string(v) != "null" {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return out, err
			}
			out.RefreshToken = &s
		}
	}
	if v, ok := fields["expires_at"]; ok {
		out.ExpiresAtSet = true
		if string(v) != "null" {
			var t time.Time
			if err := json.Unmarshal(v, &t); err != nil {
				return out, err
			}
			out.ExpiresAt = &t
		}
	}
	if v, ok := fields["scope"]; ok {
		out.ScopeSet = true
		if string(v) != "null" {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return out, err
			}
			out.Scope = &s
		}
	}
	if v, ok := fields["sync_cursor"]; ok {
		out.SyncCursorSet = true
		if string(v) != "null" {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return out, err
			}
			out.SyncCursor = &s
		}
	}
	if v, ok := fields["sync_frequency_minutes"]; ok {
		out.SyncFrequencyMinutesSet = true
		if string(v) != "null" {
			var i int
			if err := json.Unmarshal(v, &i); err != nil {
				return out, err
			}
			out.SyncFrequencyMinutes = &i
		}
	}
	if v, ok := fields["status"]; ok {
		out.StatusSet = true
		if string(v) != "null" {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return out, err
			}
			out.Status = &s
		}
	}
	return out, nil
}

func applySyncAccountPatch(current *contactsync.Account, in syncAccountUpdateInput) {
	if in.ProviderSet && in.Provider != nil {
		current.Provider = strings.TrimSpace(*in.Provider)
	}
	if in.ProviderAccountIDSet && in.ProviderAccountID != nil {
		current.ProviderAccountID = strings.TrimSpace(*in.ProviderAccountID)
	}
	if in.AccessTokenSet {
		current.AccessToken = in.AccessToken
	}
	if in.RefreshTokenSet {
		current.RefreshToken = in.RefreshToken
	}
	if in.ExpiresAtSet {
		current.ExpiresAt = in.ExpiresAt
	}
	if in.ScopeSet && in.Scope != nil {
		current.Scope = *in.Scope
	}
	if in.SyncCursorSet && in.SyncCursor != nil {
		current.SyncCursor = *in.SyncCursor
	}
	if in.SyncFrequencyMinutesSet && in.SyncFrequencyMinutes != nil {
		current.SyncFrequencyMinutes = *in.SyncFrequencyMinutes
	}
	if in.StatusSet && in.Status != nil {
		current.Status = *in.Status
	}
}
