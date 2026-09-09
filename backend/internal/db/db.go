// Package db manages the PostgreSQL connection pool and schema migrations.
package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // pgx5 database driver
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/scottfridlund/contacts/backend/migrations"
)

const (
	retryInitialBackoff = time.Second
	retryMaxBackoff     = 15 * time.Second
	retryMaxWait        = 2 * time.Minute
)

// permanentError marks a retryUntilReady failure that will never succeed on
// retry (e.g. a dirty migration state that needs manual repair), so it's
// worth failing fast instead of burning the whole retry budget on it.
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

func markPermanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err}
}

// retryConfig controls retryUntilReady's timing. Production call sites use
// the package defaults (retryInitialBackoff/retryMaxBackoff/retryMaxWait);
// tests pass small values so they don't have to wait on the real budget.
type retryConfig struct {
	initialBackoff time.Duration
	maxBackoff     time.Duration
	maxWait        time.Duration
}

func defaultRetryConfig() retryConfig {
	return retryConfig{initialBackoff: retryInitialBackoff, maxBackoff: retryMaxBackoff, maxWait: retryMaxWait}
}

// retryUntilReady calls fn repeatedly with truncated exponential backoff
// until it succeeds, fn returns a permanentError, the context is canceled, or
// the overall retry budget elapses. This lets the app recover on its own from
// the database not being ready yet at startup (a common race with container
// orchestration, DNS, or a slow first boot) instead of crash-looping and
// relying solely on the container's restart policy.
func retryUntilReady(ctx context.Context, logger *slog.Logger, what string, cfg retryConfig, fn func() error) error {
	deadline := time.Now().Add(cfg.maxWait)
	backoff := cfg.initialBackoff
	var lastErr error
	for attempt := 1; ; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		var perm *permanentError
		if errors.As(err, &perm) {
			return fmt.Errorf("%s: %w", what, perm.err)
		}
		lastErr = err
		if time.Now().After(deadline) {
			return fmt.Errorf("%s: giving up after %d attempts: %w", what, attempt, lastErr)
		}
		logger.Warn("database not ready yet, retrying", "operation", what, "attempt", attempt, "retry_in", backoff, "error", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > cfg.maxBackoff {
			backoff = cfg.maxBackoff
		}
	}
}

// Connect opens a pgx connection pool and verifies connectivity, retrying
// with backoff if the database isn't reachable yet.
func Connect(ctx context.Context, databaseURL string, logger *slog.Logger) (*pgxpool.Pool, error) {
	if logger == nil {
		logger = slog.Default()
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("creating connection pool: %w", err)
	}

	err = retryUntilReady(ctx, logger, "connecting to database", defaultRetryConfig(), func() error {
		pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return pool.Ping(pingCtx)
	})
	if err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// Migrate applies all forward migrations, retrying with backoff if the
// database isn't reachable yet. It fails fast (no retry) when the migration
// state is dirty, since that requires manual repair and won't resolve on its
// own.
func Migrate(ctx context.Context, databaseURL string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	// golang-migrate expects the pgx5 scheme for the pgx v5 driver.
	migrateURL := strings.Replace(databaseURL, "postgres://", "pgx5://", 1)

	return retryUntilReady(ctx, logger, "applying migrations", defaultRetryConfig(), func() error {
		source, err := iofs.New(migrations.FS, ".")
		if err != nil {
			return markPermanent(fmt.Errorf("loading embedded migrations: %w", err))
		}
		m, err := migrate.NewWithSourceInstance("iofs", source, migrateURL)
		if err != nil {
			return fmt.Errorf("initializing migrator: %w", err)
		}
		defer m.Close()

		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			var dirty migrate.ErrDirty
			if errors.As(err, &dirty) {
				return markPermanent(fmt.Errorf("applying migrations: %w (requires manual repair, e.g. golang-migrate 'force')", err))
			}
			return fmt.Errorf("applying migrations: %w", err)
		}
		return nil
	})
}
