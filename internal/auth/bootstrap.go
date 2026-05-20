package auth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
)

// Bootstrap environment variable names. They are read by BootstrapAdmin
// itself rather than being plumbed through internal/config to keep the
// bootstrap path self-contained — the variables are only consulted on a
// fresh install, never again.
const (
	envBootstrapUsername = "BOOTSTRAP_ADMIN_USERNAME"
	envBootstrapPassword = "BOOTSTRAP_ADMIN_PASSWORD"
)

// BootstrapAdmin auto-creates an admin user on a fresh install. It is a
// no-op if any users already exist (the second-start case), so it is safe
// to call unconditionally on every server startup.
//
// When the users table is empty:
//   - If BOOTSTRAP_ADMIN_USERNAME and BOOTSTRAP_ADMIN_PASSWORD are both set,
//     an admin user with those credentials is created.
//   - If either variable is missing, a WARN line is logged and the function
//     returns nil so the server can still start; an operator is expected to
//     populate the first user manually (e.g. via a CLI command) once that
//     surface is implemented.
//
// Returns a non-nil error only when the database calls themselves fail, or
// when CreateUser fails for a reason other than "user already exists" (a
// race between two concurrent starts).
func BootstrapAdmin(
	ctx context.Context,
	reader database.UserReader,
	writer database.UserWriter,
	cfg config.Config,
) error {
	_ = cfg // reserved for future bootstrap settings; bootstrap creds are read from env
	if reader == nil || writer == nil {
		return errors.New("bootstrap admin: nil user repository")
	}

	count, err := reader.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("bootstrap admin: count users: %w", err)
	}
	if count > 0 {
		return nil
	}

	username := os.Getenv(envBootstrapUsername)
	password := os.Getenv(envBootstrapPassword)
	if username == "" || password == "" {
		log.Printf(
			"WARN: no users exist and %s / %s are not set; "+
				"the first admin user must be created manually before login is possible",
			envBootstrapUsername, envBootstrapPassword,
		)
		return nil
	}

	hash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("bootstrap admin: hash password: %w", err)
	}

	u := &database.UserWithSecret{
		User: database.User{
			Username:    username,
			DisplayName: username,
			Role:        RoleAdmin,
		},
		PasswordHash: hash,
	}
	if err := writer.CreateUser(ctx, u); err != nil {
		// A race against another instance creating the same user is benign:
		// once the row exists the next call to BootstrapAdmin sees
		// CountUsers > 0 and is a no-op.
		if errors.Is(err, database.ErrUsernameTaken) {
			log.Printf(
				"WARN: bootstrap admin %q already exists (concurrent startup?); skipping",
				username,
			)
			return nil
		}
		return fmt.Errorf("bootstrap admin: create user: %w", err)
	}
	log.Printf("Bootstrap admin user %q created", username)
	return nil
}
