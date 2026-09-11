package contactsync

import (
	"context"

	"github.com/google/uuid"
)

type syncOriginContextKey struct{}

// WithSyncOrigin marks a context as being caused by sync reconciliation for
// the given sync account (the account a remote change was just pulled
// from). Person mutations made with this context still notify sync like any
// other change - so it can propagate onward to *other* linked/shared
// accounts - but the resulting job must exclude accountID from its own
// fan-out, or pushing the change straight back to the account it just came
// from would create a self-sustaining ping-pong loop (each write looks like
// a newer remote change on the next pull).
func WithSyncOrigin(ctx context.Context, accountID uuid.UUID) context.Context {
	return context.WithValue(ctx, syncOriginContextKey{}, accountID)
}

// IsSyncOrigin reports whether this context originated from sync reconciliation.
func IsSyncOrigin(ctx context.Context) bool {
	_, ok := ctx.Value(syncOriginContextKey{}).(uuid.UUID)
	return ok
}

// SyncOriginAccountID returns the sync account a change originated from, if
// the context was created with WithSyncOrigin.
func SyncOriginAccountID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(syncOriginContextKey{}).(uuid.UUID)
	return id, ok
}
