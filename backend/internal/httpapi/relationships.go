package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/scottfridlund/contacts/backend/internal/person"
)

type relationshipHandler struct {
	svc *person.Service
}

// relationshipResponse is the wire format for a relationship as seen from
// one specific person's side.
type relationshipResponse struct {
	ID                   uuid.UUID `json:"id"`
	Type                 string    `json:"type"`
	RelatedPersonID      *string   `json:"related_person_id,omitempty"`
	RelatedPersonName    string    `json:"related_person_name"`
	RelatedPersonDeleted bool      `json:"related_person_deleted"`
}

func toRelationshipResponse(v person.RelationshipView) relationshipResponse {
	resp := relationshipResponse{
		ID:                   v.ID,
		Type:                 string(v.Type),
		RelatedPersonName:    v.RelatedPersonName,
		RelatedPersonDeleted: v.RelatedPersonDeleted,
	}
	if v.RelatedPersonID != nil {
		id := v.RelatedPersonID.String()
		resp.RelatedPersonID = &id
	}
	return resp
}

func (h *relationshipHandler) list(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	views, err := h.svc.ListRelationships(r.Context(), id)
	if errors.Is(err, person.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "person not found", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list relationships", nil)
		return
	}
	out := make([]relationshipResponse, 0, len(views))
	for _, v := range views {
		out = append(out, toRelationshipResponse(v))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

type createRelationshipRequest struct {
	Type              string  `json:"type"`
	RelatedPersonID   *string `json:"related_person_id"`
	RelatedPersonName *string `json:"related_person_name"`
}

func (h *relationshipHandler) create(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	var req createRelationshipRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must be a JSON object", nil)
		return
	}

	in := person.RelationshipInput{Type: person.RelationType(req.Type), RelatedPersonName: req.RelatedPersonName}
	if req.RelatedPersonID != nil {
		relatedID, err := uuid.Parse(*req.RelatedPersonID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "related_person_id must be a valid UUID", nil)
			return
		}
		in.RelatedPersonID = &relatedID
	}

	view, validationErrs, err := h.svc.CreateRelationship(r.Context(), id, in)
	if errors.Is(err, person.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "person not found", nil)
		return
	}
	if validationErrs.HasErrors() {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "request validation failed", validationErrs)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create relationship", nil)
		return
	}
	writeJSON(w, http.StatusCreated, toRelationshipResponse(*view))
}

func (h *relationshipHandler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	relationshipID, err := uuid.Parse(chi.URLParam(r, "relationshipId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "relationshipId must be a valid UUID", nil)
		return
	}
	if err := h.svc.DeleteRelationship(r.Context(), id, relationshipID); err != nil {
		if errors.Is(err, person.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "relationship not found", nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to delete relationship", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
