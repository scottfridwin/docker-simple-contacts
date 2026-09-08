package contactsync

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type jobStore interface {
	Create(context.Context, *Job) (*Job, error)
}

// Service consumes local person mutations and persists sync jobs.
type Service struct {
	jobs jobStore
}

// NewService constructs a sync service.
func NewService(jobs jobStore) *Service {
	return &Service{jobs: jobs}
}

// RecordChanged stores a job for the changed record so a later sync pass can
// push or reconcile it with connected providers.
func (s *Service) RecordChanged(ctx context.Context, change PersonChange) error {
	if s.jobs == nil {
		return nil
	}
	if change.Snapshot.ID == uuid.Nil {
		return fmt.Errorf("sync change missing person id")
	}
	_, err := s.jobs.Create(ctx, &Job{
		OwnerID:  change.Snapshot.OwnerID,
		PersonID: change.Snapshot.ID,
		Kind:     change.Kind,
		Snapshot: change.Snapshot,
	})
	if err != nil {
		return fmt.Errorf("creating sync job: %w", err)
	}
	return nil
}
