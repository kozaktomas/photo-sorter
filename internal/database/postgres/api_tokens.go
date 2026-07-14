package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/auth"
	"github.com/kozaktomas/photo-sorter/internal/database"
)

// apiTokenColumns is the canonical column list for SELECTs against
// api_tokens. The order matches scanAPIToken.
//
//nolint:gosec // G101: this is a SQL column list, not a credential.
const apiTokenColumns = `uid, name, token_hash, scope, created_by_user_uid,
	created_at, expires_at, last_used_at, revoked_at`

// apiTokenUIDRandLen / apiTokenUIDPrefix mirror the photo UID scheme: a
// single-character type prefix plus 16 lowercase base32 chars.
const (
	apiTokenUIDRandLen = 16
	apiTokenUIDPrefix  = "t"

	// apiTokenTouchInterval throttles last_used_at writes. Without it every
	// authenticated GET would also issue an UPDATE — a 20k-photo export would
	// turn a read-only walk into thousands of writes for a field nobody reads
	// at that resolution.
	apiTokenTouchInterval = time.Minute
)

// APITokenRepository provides PostgreSQL-backed storage for long-lived,
// read-only machine tokens. It implements database.APITokenReader,
// database.APITokenWriter, and middleware.APITokenStore.
type APITokenRepository struct {
	pool *Pool
}

// NewAPITokenRepository returns an APITokenRepository bound to the given pool.
func NewAPITokenRepository(pool *Pool) *APITokenRepository {
	return &APITokenRepository{pool: pool}
}

// NewAPITokenUID returns a freshly generated API token UID ("t" + 16 base32
// chars). It panics if the system random source fails, since callers cannot
// meaningfully recover from that.
func NewAPITokenUID() string {
	randBytes := (apiTokenUIDRandLen*5 + 7) / 8
	buf := make([]byte, randBytes)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("api token uid: read random: %v", err))
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	return apiTokenUIDPrefix + strings.ToLower(enc[:apiTokenUIDRandLen])
}

// CreateAPIToken inserts a token row. The caller is responsible for having
// generated the raw token and its hash (see auth.GenerateAPIToken); this
// method never sees the raw value.
func (r *APITokenRepository) CreateAPIToken(ctx context.Context, t *database.APIToken) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO api_tokens (uid, name, token_hash, scope, created_by_user_uid, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		t.UID, t.Name, t.TokenHash, t.Scope,
		nullableString(t.CreatedByUserUID), t.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("create api token: %w", err)
	}
	return nil
}

// ListAPITokens returns every token row, newest first. Revoked and expired
// tokens are included so the operator can see the full history; callers
// render the state from RevokedAt / ExpiresAt.
func (r *APITokenRepository) ListAPITokens(ctx context.Context) ([]database.APIToken, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+apiTokenColumns+` FROM api_tokens ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	defer rows.Close()

	var tokens []database.APIToken
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api tokens: %w", err)
	}
	return tokens, nil
}

// GetAPITokenByHash fetches a token by its SHA-256 hash regardless of whether
// it is still live. Returns database.ErrNotFound when no row matches.
//
// The auth path uses ResolveAPIToken instead — this method is for management
// surfaces that need to inspect revoked/expired rows too.
func (r *APITokenRepository) GetAPITokenByHash(
	ctx context.Context, hash string,
) (*database.APIToken, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+apiTokenColumns+` FROM api_tokens WHERE token_hash = $1`, hash)
	t, err := scanAPIToken(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// RevokeAPIToken soft-revokes a token by stamping revoked_at. It is
// idempotent: re-revoking an already-revoked token leaves the original
// timestamp in place and still reports success. Returns database.ErrNotFound
// when the UID does not exist.
func (r *APITokenRepository) RevokeAPIToken(ctx context.Context, uid string) error {
	res, err := r.pool.Exec(ctx,
		`UPDATE api_tokens SET revoked_at = NOW()
		 WHERE uid = $1 AND revoked_at IS NULL`, uid)
	if err != nil {
		return fmt.Errorf("revoke api token: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke api token: rows affected: %w", err)
	}
	if n > 0 {
		return nil
	}
	// Either the token does not exist, or it was already revoked. Only the
	// former is an error.
	var exists bool
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM api_tokens WHERE uid = $1)`, uid).Scan(&exists); err != nil {
		return fmt.Errorf("revoke api token: check exists: %w", err)
	}
	if !exists {
		return database.ErrNotFound
	}
	return nil
}

// ResolveAPIToken is the authentication hot path: it maps a raw bearer token
// onto a live token row, or returns (nil, nil) when the token is unknown,
// revoked, or expired.
//
// Liveness is enforced in SQL rather than in Go so there is exactly one place
// a revoked token can be let through, and NOW() is the database's clock — the
// same clock that stamped revoked_at.
//
// A (nil, nil) result is deliberately indistinguishable across "no such
// token", "revoked", and "expired": the caller turns all three into the same
// 401, so an attacker learns nothing about which tokens exist.
func (r *APITokenRepository) ResolveAPIToken(
	ctx context.Context, rawToken string,
) (*database.APIToken, error) {
	hash := auth.HashAPIToken(rawToken)
	row := r.pool.QueryRow(ctx,
		`SELECT `+apiTokenColumns+` FROM api_tokens
		 WHERE token_hash = $1
		   AND revoked_at IS NULL
		   AND (expires_at IS NULL OR expires_at > NOW())`, hash)
	t, err := scanAPIToken(row)
	if errors.Is(err, sql.ErrNoRows) {
		// "No live token" is not an error — see the doc comment above.
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// TouchAPIToken records that a token was just used, throttled to at most one
// write per apiTokenTouchInterval. The throttle lives in the UPDATE's WHERE
// clause so it costs no extra round-trip and races between concurrent
// requests resolve harmlessly (one wins, the rest no-op).
func (r *APITokenRepository) TouchAPIToken(ctx context.Context, uid string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE api_tokens SET last_used_at = NOW()
		 WHERE uid = $1
		   AND (last_used_at IS NULL OR last_used_at < NOW() - $2::interval)`,
		uid, apiTokenTouchInterval.String(),
	)
	if err != nil {
		return fmt.Errorf("touch api token: %w", err)
	}
	return nil
}

// scanAPIToken reads one api_tokens row into a database.APIToken, mapping the
// nullable columns onto their pointer/empty-string representations.
func scanAPIToken(s rowScanner) (*database.APIToken, error) {
	var (
		t         database.APIToken
		createdBy sql.NullString
	)
	if err := s.Scan(
		&t.UID, &t.Name, &t.TokenHash, &t.Scope, &createdBy,
		&t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt, &t.RevokedAt,
	); err != nil {
		// Wrapped with %w either way, so callers can still match
		// sql.ErrNoRows with errors.Is and map it to "no such token".
		return nil, fmt.Errorf("scan api token row: %w", err)
	}
	t.CreatedByUserUID = createdBy.String
	return &t, nil
}

// Verify interface compliance.
var (
	_ database.APITokenReader = (*APITokenRepository)(nil)
	_ database.APITokenWriter = (*APITokenRepository)(nil)
)
