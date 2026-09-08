package user

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type User struct {
	ID          uuid.UUID
	Subject     string
	Email       string
	DisplayName string
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Upsert(ctx context.Context, subject, email, displayName string) (*User, error) {
	const q = `
		INSERT INTO users (subject, email, display_name)
		VALUES ($1, $2, $3)
		ON CONFLICT (subject) DO UPDATE SET email = EXCLUDED.email,
			display_name = EXCLUDED.display_name, updated_at = now()
		RETURNING id, subject, email, display_name`
	var u User
	if err := r.pool.QueryRow(ctx, q, subject, email, displayName).
		Scan(&u.ID, &u.Subject, &u.Email, &u.DisplayName); err != nil {
		return nil, fmt.Errorf("upserting user: %w", err)
	}
	return &u, nil
}

func (r *Repository) PurgeSessions(ctx context.Context, before time.Time) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM user_sessions WHERE expires_at < $1`, before)
	return err
}
