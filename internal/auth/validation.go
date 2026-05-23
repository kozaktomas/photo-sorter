package auth

import (
	"context"
	"fmt"
	"regexp"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// MinPasswordLength is the minimum number of characters accepted for a
// new password. Shared between the web handler and the CLI so the rules
// cannot drift apart.
const MinPasswordLength = 8

// usernameRegexp matches a valid username: lowercase alphanumerics plus _,
// ., and -, length 3-64. The same shape is enforced by the bootstrap
// admin flow and the table-level constraints.
var usernameRegexp = regexp.MustCompile(`^[a-z0-9_.-]{3,64}$`)

// ValidUsername reports whether s passes the username shape check.
func ValidUsername(s string) bool {
	return usernameRegexp.MatchString(s)
}

// EnsureNotLastAdmin returns nil when the user identified by targetUID can
// safely be removed (deleted, demoted, or disabled) without leaving the
// users table with no enabled admin remaining. When target is not an
// admin the check is a no-op. ErrLastAdmin is returned when the action
// would strand the system without an admin; repo failures are returned
// verbatim so the caller can wrap them in its own error envelope.
//
// This is the shared source of truth for the "you can't kick the last
// admin out" rule — both the REST handler and the CLI call it so the
// guarantee holds regardless of how the request arrived.
func EnsureNotLastAdmin(
	ctx context.Context,
	repo database.UserReader,
	target *database.User,
	targetUID string,
) error {
	if target == nil || target.Role != RoleAdmin {
		return nil
	}
	users, err := repo.ListUsers(ctx)
	if err != nil {
		return fmt.Errorf("ensure not last admin: list users: %w", err)
	}
	for _, u := range users {
		if u.UID == targetUID {
			continue
		}
		if u.Role == RoleAdmin && !u.Disabled {
			return nil
		}
	}
	return ErrLastAdmin
}
