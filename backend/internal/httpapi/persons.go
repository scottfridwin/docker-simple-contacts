package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/scottfridlund/contacts/backend/internal/person"
)

// List behavior defaults (from implementation decisions).
const (
	defaultPageSize = 25
	maxPageSize     = 100
	maxRequestBytes = 1 << 20 // 1 MiB
)

type personHandler struct {
	svc *person.Service
}

// listResponse is the paginated envelope for list results.
type listResponse struct {
	Data       []person.Person `json:"data"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	Total      int             `json:"total"`
	TotalPages int             `json:"total_pages"`
}

func (h *personHandler) create(w http.ResponseWriter, r *http.Request) {
	var in person.CreateInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	created, validationErrs, err := h.svc.Create(r.Context(), in)
	if validationErrs.HasErrors() {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "request validation failed", validationErrs)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create person", nil)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *personHandler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	p, err := h.svc.Get(r.Context(), id)
	if errors.Is(err, person.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "person not found", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to fetch person", nil)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *personHandler) list(w http.ResponseWriter, r *http.Request) {
	params := parseListParams(r)
	persons, total, err := h.svc.List(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list persons", nil)
		return
	}
	totalPages := (total + params.PageSize - 1) / params.PageSize
	writeJSON(w, http.StatusOK, listResponse{
		Data:       persons,
		Page:       params.Page,
		PageSize:   params.PageSize,
		Total:      total,
		TotalPages: totalPages,
	})
}

func (h *personHandler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}

	in, err := decodeUpdate(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	updated, validationErrs, err := h.svc.Update(r.Context(), id, in)
	if validationErrs.HasErrors() {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "request validation failed", validationErrs)
		return
	}
	if errors.Is(err, person.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "person not found", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update person", nil)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *personHandler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	err := h.svc.Delete(r.Context(), id)
	if errors.Is(err, person.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "person not found", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to delete person", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be a valid UUID", nil)
		return uuid.Nil, false
	}
	return id, true
}

func parseListParams(r *http.Request) person.ListParams {
	q := r.URL.Query()

	page := 1
	if v, err := strconv.Atoi(q.Get("page")); err == nil && v > 0 {
		page = v
	}

	pageSize := defaultPageSize
	if v, err := strconv.Atoi(q.Get("page_size")); err == nil && v > 0 {
		pageSize = v
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}

	// Default sort: display_name desc.
	sortField := "display_name"
	if s := q.Get("sort"); s != "" {
		sortField = s
	}
	sortDesc := true
	switch q.Get("order") {
	case "asc":
		sortDesc = false
	case "desc":
		sortDesc = true
	}

	return person.ListParams{
		Page:      page,
		PageSize:  pageSize,
		SortField: sortField,
		SortDesc:  sortDesc,
		FirstName: q.Get("first_name"),
		LastName:  q.Get("last_name"),
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

// decodeUpdate parses a PATCH body while tracking which fields were supplied so
// that omitted fields are left unchanged.
func decodeUpdate(w http.ResponseWriter, r *http.Request) (person.UpdateInput, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return person.UpdateInput{}, err
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return person.UpdateInput{}, errors.New("request body must be a JSON object")
	}

	allowed := map[string]struct{}{
		"first_name": {}, "middle_names": {}, "last_name": {},
		"nickname": {}, "pronouns": {}, "birthdate": {},
		"phone_numbers": {}, "custom_fields": {},
	}
	for key := range fields {
		if _, ok := allowed[key]; !ok {
			return person.UpdateInput{}, errors.New("unknown field: " + key)
		}
	}

	var in person.UpdateInput
	if raw, ok := fields["first_name"]; ok {
		if err := json.Unmarshal(raw, &in.FirstName); err != nil {
			return person.UpdateInput{}, errors.New("first_name must be a string")
		}
		in.FirstNameSet = true
	}
	if raw, ok := fields["last_name"]; ok {
		if err := json.Unmarshal(raw, &in.LastName); err != nil {
			return person.UpdateInput{}, errors.New("last_name must be a string")
		}
		in.LastNameSet = true
	}
	if raw, ok := fields["middle_names"]; ok {
		var names []string
		if err := json.Unmarshal(raw, &names); err != nil {
			return person.UpdateInput{}, errors.New("middle_names must be an array of strings")
		}
		in.MiddleNames = &names
		in.MiddleNamesSet = true
	}
	if raw, ok := fields["nickname"]; ok {
		if err := json.Unmarshal(raw, &in.Nickname); err != nil {
			return person.UpdateInput{}, errors.New("nickname must be a string")
		}
		in.NicknameSet = true
	}
	if raw, ok := fields["pronouns"]; ok {
		if err := json.Unmarshal(raw, &in.Pronouns); err != nil {
			return person.UpdateInput{}, errors.New("pronouns must be a string")
		}
		in.PronounsSet = true
	}
	if raw, ok := fields["birthdate"]; ok {
		if err := json.Unmarshal(raw, &in.Birthdate); err != nil {
			return person.UpdateInput{}, errors.New("birthdate must be a string")
		}
		in.BirthdateSet = true
	}
	if raw, ok := fields["phone_numbers"]; ok {
		var nums []string
		if err := json.Unmarshal(raw, &nums); err != nil {
			return person.UpdateInput{}, errors.New("phone_numbers must be an array of strings")
		}
		in.PhoneNumbers = &nums
		in.PhoneNumbersSet = true
	}
	if raw, ok := fields["custom_fields"]; ok {
		var cf map[string]any
		if err := json.Unmarshal(raw, &cf); err != nil {
			return person.UpdateInput{}, errors.New("custom_fields must be an object")
		}
		in.CustomFields = cf
		in.CustomFieldsSet = true
	}
	return in, nil
}
