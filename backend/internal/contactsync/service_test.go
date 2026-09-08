package contactsync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeJobStore struct {
	jobs []*Job
	err  error
}

func (f *fakeJobStore) Create(_ context.Context, job *Job) (*Job, error) {
	if f.err != nil {
		return nil, f.err
	}
	cp := *job
	cp.ID = uuid.New()
	cp.CreatedAt = time.Now()
	cp.UpdatedAt = cp.CreatedAt
	if cp.Status == "" {
		cp.Status = JobStatusPending
	}
	f.jobs = append(f.jobs, &cp)
	return &cp, nil
}

func TestServiceRecordChangedPersistsJob(t *testing.T) {
	store := &fakeJobStore{}
	svc := NewService(store)
	ownerID := uuid.New()
	personID := uuid.New()
	if err := svc.RecordChanged(context.Background(), PersonChange{
		Kind: ChangeKindUpdated,
		Snapshot: PersonSnapshot{
			ID:      personID,
			OwnerID: &ownerID,
		},
	}); err != nil {
		t.Fatalf("RecordChanged: %v", err)
	}
	if len(store.jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(store.jobs))
	}
	if store.jobs[0].Kind != ChangeKindUpdated || store.jobs[0].PersonID != personID {
		t.Fatalf("unexpected job: %#v", store.jobs[0])
	}
}

func TestServiceRecordChangedPropagatesCreateError(t *testing.T) {
	svc := NewService(&fakeJobStore{err: errors.New("db down")})
	err := svc.RecordChanged(context.Background(), PersonChange{Snapshot: PersonSnapshot{ID: uuid.New()}})
	if err == nil {
		t.Fatal("expected error")
	}
}
