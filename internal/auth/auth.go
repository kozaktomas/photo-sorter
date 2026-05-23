// Package auth provides password hashing, role constants, and helpers for
// the native user model. It deliberately does NOT touch HTTP sessions or
// login handlers — those live in internal/web/middleware.
package auth

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// Role constants for the native user model. The values are persisted in the
// users.role column and enforced by a CHECK constraint at the database level
// (see migration 032_native_photo_management.sql).
const (
	RoleAdmin  = "admin"
	RoleEditor = "editor"
	RoleViewer = "viewer"
)

// bcryptCost is the work factor passed to bcrypt.GenerateFromPassword. A cost
// of 12 keeps hashing under ~300ms on a modern Pi while staying well above
// bcrypt.DefaultCost (10).
const bcryptCost = 12

// HashPassword returns a bcrypt hash of the given plaintext password using
// cost 12. The returned string is safe to persist directly as the
// password_hash column value.
func HashPassword(plain string) (string, error) {
	if plain == "" {
		return "", errors.New("hash password: empty password")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword reports whether the supplied plaintext attempt matches the
// stored bcrypt hash. The comparison is performed by bcrypt itself, which is
// constant-time across equal-length hashes. Any error (including malformed
// hashes) results in a false return.
func CheckPassword(plainAttempt, hash string) bool {
	if plainAttempt == "" || hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plainAttempt)) == nil
}

// IsValidRole reports whether the given string is one of the three known
// role values.
func IsValidRole(role string) bool {
	switch role {
	case RoleAdmin, RoleEditor, RoleViewer:
		return true
	default:
		return false
	}
}

// HasWriteAccess reports whether the given role is permitted to modify data.
// Admin and editor roles can write; viewer is read-only.
func HasWriteAccess(role string) bool {
	return role == RoleAdmin || role == RoleEditor
}

// IsAdmin reports whether the given role is the admin role.
func IsAdmin(role string) bool {
	return role == RoleAdmin
}

// ErrLastAdmin is returned by EnsureNotLastAdmin when the requested
// operation would leave the system without an enabled admin user. It is
// sentinel-style so callers can pattern-match without parsing strings.
var ErrLastAdmin = errors.New("cannot delete the last admin")
