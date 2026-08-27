package httpapi

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/scottfridlund/contacts/backend/internal/person"
)

type relationshipHandler struct {
	svc *person.RelationshipService
}

func (h *relationshipHandler) create(w http.ResponseWriter, r *http.Request) {
	personID, ok := parseID(w, r)
	if !ok {
		return
	}

	var in struct {
		PersonID2        string `json:"person_id_2"`
		RelationshipType string `json:"relationship_type"`
		Label            *string `json:"label,omitempty"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	personID2, err := uuid.Parse(in.PersonID2)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "person_id_2 must be a valid UUID", nil)
		return
	}

	created, validationErrs, err := h.svc.CreateRelationship(r.Context(), personID, personID2, in.RelationshipType, in.Label)
	if validationErrs.HasErrors() {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "request validation failed", validationErrs)
		return
	}
	if errors.Is(err, person.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "one or both persons not found", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create relationship", nil)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *relationshipHandler) get(w http.ResponseWriter, r *http.Request) {
	relID, ok := parseID(w, r)
	if !ok {
		return
	}
	rel, err := h.svc.GetRelationship(r.Context(), relID)
	if errors.Is(err, person.ErrRelationshipNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "relationship not found", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to fetch relationship", nil)
		return
	}
	writeJSON(w, http.StatusOK, rel)
}

func (h *relationshipHandler) listForPerson(w http.ResponseWriter, r *http.Request) {
	personID, ok := parseID(w, r)
	if !ok {
		return
	}
	rels, err := h.svc.ListRelationshipsForPerson(r.Context(), personID)
	if errors.Is(err, person.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "person not found", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list relationships", nil)
		return
	}
	if rels == nil {
		rels = []person.Relationship{}
	}
	writeJSON(w, http.StatusOK, rels)
}

func (h *relationshipHandler) update(w http.ResponseWriter, r *http.Request) {
	relID, ok := parseID(w, r)
	if !ok {
		return
	}

	var in struct {
		RelationshipType string  `json:"relationship_type"`
		Label            *string `json:"label,omitempty"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	updated, validationErrs, err := h.svc.UpdateRelationship(r.Context(), relID, in.RelationshipType, in.Label)
	if validationErrs.HasErrors() {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "request validation failed", validationErrs)
		return
	}
	if errors.Is(err, person.ErrRelationshipNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "relationship not found", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update relationship", nil)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *relationshipHandler) delete(w http.ResponseWriter, r *http.Request) {
	relID, ok := parseID(w, r)
	if !ok {
		return
	}
	err := h.svc.DeleteRelationship(r.Context(), relID)
	if errors.Is(err, person.ErrRelationshipNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "relationship not found", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to delete relationship", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
