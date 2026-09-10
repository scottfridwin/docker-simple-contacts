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

// MaxJobAttempts caps automatic retries for a failed sync job. Once a job
// has failed this many times it is left in "failed" status permanently (a
// dead letter, visible via last_error) instead of retried forever.
const MaxJobAttempts = 5

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
