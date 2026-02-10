package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// SessionRepository provides PostgreSQL-backed session storage
type SessionRepository struct {
	pool *Pool
}

// NewSessionRepository creates a new PostgreSQL session repository
func NewSessionRepository(pool *Pool) *SessionRepository {
	return &SessionRepository{pool: pool}
}

// Save stores a session in the database
func (r *SessionRepository) Save(ctx context.Context, id, token, downloadToken string, createdAt, expiresAt time.Time) error {
	query := `
		INSERT INTO sessions (id, token, download_token, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET
			token = EXCLUDED.token,
			download_token = EXCLUDED.download_token,
			created_at = EXCLUDED.created_at,
			expires_at = EXCLUDED.expires_at
	`

	_, err := r.pool.Exec(ctx, query, id, token, downloadToken, createdAt, expiresAt)
	if err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

// Get retrieves a session by ID, returns nil if not found or expired
func (r *SessionRepository) Get(ctx context.Context, sessionID string) (*middleware.StoredSession, error) {
	query := `
		SELECT id, token, download_token, created_at, expires_at
		FROM sessions
		WHERE id = $1 AND expires_at > NOW()
	`

	var s middleware.StoredSession
	err := r.pool.QueryRow(ctx, query, sessionID).Scan(
		&s.ID,
		&s.Token,
		&s.DownloadToken,
		&s.CreatedAt,
		&s.ExpiresAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}

	return &s, nil
}

// Delete removes a session from the database
func (r *SessionRepository) Delete(ctx context.Context, sessionID string) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM sessions WHERE id = $1", sessionID)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteExpired removes all expired sessions and returns the count deleted
func (r *SessionRepository) DeleteExpired(ctx context.Context) (int64, error) {
	result, err := r.pool.Exec(ctx, "DELETE FROM sessions WHERE expires_at <= NOW()")
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("getting rows affected: %w", err)
	}
	return count, nil
}
