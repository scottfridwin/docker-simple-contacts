package authn

import (
	"context"

	"github.com/google/uuid"
)

type contextKey struct{}

// WithUserID associates the authenticated account with a request.
func WithUserID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// UserID returns the authenticated account, when one is present.
func UserID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(contextKey{}).(uuid.UUID)
	return id, ok && id != uuid.Nil
}
