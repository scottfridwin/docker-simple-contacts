package contactsync

import (
	"time"

	"github.com/google/uuid"
)

// JobStatus values track the lifecycle of a sync job.
const (
	JobStatusPending = "pending"
	JobStatusFailed  = "failed"
	JobStatusDone    = "done"
)

// Job represents one queued sync operation for a local record.
type Job struct {
	ID          uuid.UUID
	OwnerID     *uuid.UUID
	PersonID    uuid.UUID
	Kind        ChangeKind
	Snapshot    PersonSnapshot
	Status      string
	Attempts    int
	LastError   *string
	ProcessedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
