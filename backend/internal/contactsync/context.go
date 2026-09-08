package contactsync

import "context"

type syncOriginContextKey struct{}

// WithSyncOrigin marks a context as being caused by sync reconciliation.
func WithSyncOrigin(ctx context.Context) context.Context {
	return context.WithValue(ctx, syncOriginContextKey{}, true)
}

// IsSyncOrigin reports whether this context originated from sync reconciliation.
func IsSyncOrigin(ctx context.Context) bool {
	v, _ := ctx.Value(syncOriginContextKey{}).(bool)
	return v
}
