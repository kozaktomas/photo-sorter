package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"

	"github.com/kozaktomas/photo-sorter/internal/auth"
	"github.com/kozaktomas/photo-sorter/internal/database"
)

const (
	// userUIDPrefix is the single-character type prefix for user UIDs.
	userUIDPrefix = "u"

	// userUIDRandLen is the number of random base32 characters appended
	// after the "u" prefix in a generated user UID.
	userUIDRandLen = 16

	// usersUsernameUniqueConstraint is the name of the UNIQUE index on
	// users.username from migration 032. PostgreSQL surfaces the constraint
	// name on conflict; matching against the explicit name avoids confusing
	// unrelated future uniques with username collisions.
	usersUsernameUniqueConstraint = "users_username_key"
)

// userColumns is the canonical column list for SELECT statements against the
// users table. The order matches scanUser below.
const userColumns = `uid, username, display_name, email, role,
	disabled, created_at, updated_at, last_login_at`

// userColumnsWithSecret extends userColumns with password_hash for the login
// flow. Scan order matches scanUserWithSecret.
// #nosec G101 -- SQL column list, not a literal credential.
const userColumnsWithSecret = `uid, username, display_name, email, role,
	disabled, created_at, updated_at, last_login_at, password_hash`

// UserRepository provides PostgreSQL-backed storage for the native users
// table. It implements database.UserReader and database.UserWriter.
type UserRepository struct {
	pool *Pool
}

// NewUserRepository returns a UserRepository bound to the given pool.
func NewUserRepository(pool *Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

// NewUserUID returns a freshly generated user UID. The format is
// `"u" + 16 lowercase base32 characters`, e.g. "ua3bz5x9k8mq7n2v".
// The random suffix is drawn from crypto/rand; the function panics if the
// system random source fails, since callers cannot meaningfully recover.
func NewUserUID() string {
	randBytes := (userUIDRandLen*5 + 7) / 8
	buf := make([]byte, randBytes)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("user uid: read random: %v", err))
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	return userUIDPrefix + strings.ToLower(enc[:userUIDRandLen])
}

// --- Reads ---

// GetUser fetches a single user by UID. Returns database.ErrNotFound when
// the row does not exist.
func (r *UserRepository) GetUser(ctx context.Context, uid string) (*database.User, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE uid = $1`, uid)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

// GetUserByUsername fetches a user (with the bcrypt hash) by username.
// Returns database.ErrNotFound when the row does not exist.
func (r *UserRepository) GetUserByUsername(
	ctx context.Context, username string,
) (*database.UserWithSecret, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+userColumnsWithSecret+` FROM users WHERE username = $1`, username)
	u, err := scanUserWithSecret(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by username: %w", err)
	}
	return u, nil
}

// ListUsers returns every user, ordered by username for deterministic
// iteration in admin UIs.
func (r *UserRepository) ListUsers(ctx context.Context) ([]database.User, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+userColumns+` FROM users ORDER BY username ASC`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	var users []database.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, *u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return users, nil
}

// CountUsers returns the total number of rows in the users table.
func (r *UserRepository) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

// --- Writes ---

// CreateUser inserts a new user. The UID is generated when u.UID is empty.
// Username collisions are surfaced as database.ErrUsernameTaken; the role
// value must be one of auth.RoleAdmin/Editor/Viewer (it is also enforced by
// the table-level CHECK constraint). The created/updated timestamps are set
// to NOW() by the database and copied back into u on success.
func (r *UserRepository) CreateUser(
	ctx context.Context, u *database.UserWithSecret,
) error {
	if u == nil {
		return errors.New("create user: nil user")
	}
	if u.UID == "" {
		u.UID = NewUserUID()
	}
	if !auth.IsValidRole(u.Role) {
		return fmt.Errorf("create user: invalid role %q", u.Role)
	}
	if u.Username == "" {
		return errors.New("create user: empty username")
	}
	if u.PasswordHash == "" {
		return errors.New("create user: empty password hash")
	}

	row := r.pool.QueryRow(ctx,
		`INSERT INTO users (
			uid, username, display_name, email,
			password_hash, role, disabled
		 ) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7
		 )
		 RETURNING created_at, updated_at`,
		u.UID, u.Username, u.DisplayName, u.Email,
		u.PasswordHash, u.Role, u.Disabled,
	)
	if err := row.Scan(&u.CreatedAt, &u.UpdatedAt); err != nil {
		if isUniqueViolation(err, usersUsernameUniqueConstraint) {
			return database.ErrUsernameTaken
		}
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

// UpdateUser writes the supplied user back to the database. The password
// hash and last_login_at columns are NOT modified by this method — use
// SetPassword and TouchLastLogin for those. Returns database.ErrNotFound
// when the row does not exist.
func (r *UserRepository) UpdateUser(ctx context.Context, u *database.User) error {
	if u == nil {
		return errors.New("update user: nil user")
	}
	if !auth.IsValidRole(u.Role) {
		return fmt.Errorf("update user: invalid role %q", u.Role)
	}
	row := r.pool.QueryRow(ctx,
		`UPDATE users SET
			username = $1,
			display_name = $2,
			email = $3,
			role = $4,
			disabled = $5,
			updated_at = NOW()
		 WHERE uid = $6
		 RETURNING updated_at`,
		u.Username, u.DisplayName, u.Email, u.Role, u.Disabled, u.UID,
	)
	if err := row.Scan(&u.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return database.ErrNotFound
		}
		if isUniqueViolation(err, usersUsernameUniqueConstraint) {
			return database.ErrUsernameTaken
		}
		return fmt.Errorf("update user: %w", err)
	}
	return nil
}

// SetPassword overwrites the bcrypt hash for the given user. Returns
// database.ErrNotFound when no row is affected.
func (r *UserRepository) SetPassword(ctx context.Context, uid, newHash string) error {
	if newHash == "" {
		return errors.New("set password: empty hash")
	}
	res, err := r.pool.Exec(ctx,
		`UPDATE users SET password_hash = $1, updated_at = NOW() WHERE uid = $2`,
		newHash, uid,
	)
	if err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set password rows affected: %w", err)
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}

// SetDisabled toggles the disabled flag for the given user. Returns
// database.ErrNotFound when no row is affected.
func (r *UserRepository) SetDisabled(ctx context.Context, uid string, disabled bool) error {
	res, err := r.pool.Exec(ctx,
		`UPDATE users SET disabled = $1, updated_at = NOW() WHERE uid = $2`,
		disabled, uid,
	)
	if err != nil {
		return fmt.Errorf("set disabled: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set disabled rows affected: %w", err)
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}

// TouchLastLogin sets last_login_at to NOW() for the given user. Returns
// database.ErrNotFound when no row is affected.
func (r *UserRepository) TouchLastLogin(ctx context.Context, uid string) error {
	res, err := r.pool.Exec(ctx,
		`UPDATE users SET last_login_at = NOW() WHERE uid = $1`, uid,
	)
	if err != nil {
		return fmt.Errorf("touch last login: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("touch last login rows affected: %w", err)
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}

// DeleteUser hard-deletes the user row. FK references from other tables
// (photos.uploaded_by, albums.created_by) use ON DELETE SET NULL so the
// pointed-to rows are kept. Returns database.ErrNotFound when no row is
// affected.
func (r *UserRepository) DeleteUser(ctx context.Context, uid string) error {
	res, err := r.pool.Exec(ctx, `DELETE FROM users WHERE uid = $1`, uid)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete user rows affected: %w", err)
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}

// --- Helpers ---

// scanUser reads one users row (without the password hash) using the column
// order in userColumns.
func scanUser(s rowScanner) (*database.User, error) {
	var u database.User
	if err := s.Scan(
		&u.UID, &u.Username, &u.DisplayName, &u.Email, &u.Role,
		&u.Disabled, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err //nolint:wrapcheck // sentinel propagated to caller
		}
		return nil, fmt.Errorf("scan user row: %w", err)
	}
	return &u, nil
}

// scanUserWithSecret reads one users row including the password hash, using
// the column order in userColumnsWithSecret.
func scanUserWithSecret(s rowScanner) (*database.UserWithSecret, error) {
	var u database.UserWithSecret
	if err := s.Scan(
		&u.UID, &u.Username, &u.DisplayName, &u.Email, &u.Role,
		&u.Disabled, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt,
		&u.PasswordHash,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err //nolint:wrapcheck // sentinel propagated to caller
		}
		return nil, fmt.Errorf("scan user row: %w", err)
	}
	return &u, nil
}

// Verify interface compliance.
var (
	_ database.UserReader = (*UserRepository)(nil)
	_ database.UserWriter = (*UserRepository)(nil)
)
