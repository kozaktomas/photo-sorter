package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/audit"
	"github.com/kozaktomas/photo-sorter/internal/auth"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var usersCmd = &cobra.Command{
	Use:   "users",
	Short: "Manage local users (admin recovery from the CLI)",
	Long: `Manage the local user accounts that back the web UI login.

Use these commands to bootstrap a fresh install, reset a forgotten admin
password, or remove a stale account when the web UI is unreachable.

Mutating subcommands append rows to the audit_log table with actor=cli so
the operation appears in the admin audit viewer.`,
}

var usersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all local users",
	RunE:  runUsersList,
}

var usersCreateCmd = &cobra.Command{
	Use:   "create <username>",
	Short: "Create a new user (prompts for the password)",
	Long: `Create a new user. The password is read from the terminal (hidden,
confirmed twice). --role is required. Username must match [a-z0-9_.-]{3,64}.

Example:
  photo-sorter users create alice --role=editor --display-name="Alice"`,
	Args: cobra.ExactArgs(1),
	RunE: runUsersCreate,
}

var usersSetPasswordCmd = &cobra.Command{
	Use:   "set-password <username>",
	Short: "Reset a user's password (prompts for the new password)",
	Args:  cobra.ExactArgs(1),
	RunE:  runUsersSetPassword,
}

var usersDeleteCmd = &cobra.Command{
	Use:   "delete <username>",
	Short: "Delete a user (prompts for confirmation unless --yes)",
	Long: `Delete a user account. Refuses to delete the only remaining admin
to keep the system reachable.

Example:
  photo-sorter users delete alice
  photo-sorter users delete --yes alice`,
	Args: cobra.ExactArgs(1),
	RunE: runUsersDelete,
}

func init() {
	rootCmd.AddCommand(usersCmd)
	usersCmd.AddCommand(usersListCmd)
	usersCmd.AddCommand(usersCreateCmd)
	usersCmd.AddCommand(usersSetPasswordCmd)
	usersCmd.AddCommand(usersDeleteCmd)

	usersListCmd.Flags().Bool("json", false, "Output as JSON instead of a table")

	usersCreateCmd.Flags().String("role", "", "Role: admin, editor, or viewer (required)")
	usersCreateCmd.Flags().String("display-name", "", "Display name (defaults to the username)")
	usersCreateCmd.Flags().String("email", "", "Email address (optional)")
	_ = usersCreateCmd.MarkFlagRequired("role")

	usersDeleteCmd.Flags().Bool("yes", false, "Skip the confirmation prompt")
}

// usersDeps bundles the repositories the user-management subcommands need.
// We intentionally open both the user repo and the audit-log repo so every
// CLI mutation lands in the audit trail just like its REST counterpart.
type usersDeps struct {
	users database.UserWriter
	audit *audit.Logger
}

func initUsersDeps(cfg *config.Config) (*usersDeps, error) {
	if cfg.Database.URL == "" {
		return nil, errors.New("DATABASE_URL environment variable is required")
	}
	if err := postgres.Initialize(&cfg.Database); err != nil {
		return nil, fmt.Errorf("initialise PostgreSQL: %w", err)
	}
	pool := postgres.GetGlobalPool()
	return &usersDeps{
		users: postgres.NewUserRepository(pool),
		audit: audit.NewLogger(postgres.NewAuditLogRepository(pool)),
	}, nil
}

// logCLI records an audit event for a CLI-initiated mutation. user_uid is
// left empty (no logged-in actor on the CLI) and metadata.actor="cli" is
// stamped on the row so the audit viewer can distinguish CLI activity
// from web traffic.
func (d *usersDeps) logCLI(
	ctx context.Context, action, entityUID string, metadata map[string]any,
) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["actor"] = "cli"
	d.audit.LogAnonymous(ctx, action, audit.EntityUser, entityUID, "cli", metadata)
}

func runUsersList(cmd *cobra.Command, _ []string) error {
	jsonOut := mustGetBool(cmd, "json")
	cfg := config.Load()
	deps, err := initUsersDeps(cfg)
	if err != nil {
		return err
	}
	users, err := deps.users.ListUsers(cmd.Context())
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}
	if jsonOut {
		return renderUsersJSON(users)
	}
	return renderUsersTable(users)
}

func renderUsersJSON(users []database.User) error {
	rows := make([]userJSONRow, 0, len(users))
	for _, u := range users {
		rows = append(rows, toUserJSONRow(u))
	}
	if err := json.NewEncoder(os.Stdout).Encode(rows); err != nil {
		return fmt.Errorf("encode users: %w", err)
	}
	return nil
}

func renderUsersTable(users []database.User) error {
	if len(users) == 0 {
		fmt.Println("No users.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "USERNAME\tROLE\tDISPLAY NAME\tDISABLED\tLAST LOGIN\tCREATED")
	fmt.Fprintln(w, "--------\t----\t------------\t--------\t----------\t-------")
	for _, u := range users {
		last := "-"
		if u.LastLoginAt != nil && !u.LastLoginAt.IsZero() {
			last = u.LastLoginAt.UTC().Format(time.RFC3339)
		}
		disabled := ""
		if u.Disabled {
			disabled = yesAnswerLiteral
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			u.Username, u.Role, u.DisplayName, disabled, last,
			u.CreatedAt.UTC().Format(time.RFC3339),
		)
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush table: %w", err)
	}
	return nil
}

// userJSONRow is the wire shape emitted by `users list --json`. The
// database.User struct deliberately has no json tags (the REST handler
// projects through its own UserResponse), so the CLI gets its own
// snake_case mapping to keep machine-readable output stable.
type userJSONRow struct {
	UID         string  `json:"uid"`
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	Email       string  `json:"email"`
	Role        string  `json:"role"`
	Disabled    bool    `json:"disabled"`
	CreatedAt   string  `json:"created_at"`
	LastLoginAt *string `json:"last_login_at"`
}

func toUserJSONRow(u database.User) userJSONRow {
	row := userJSONRow{
		UID:         u.UID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Email:       u.Email,
		Role:        u.Role,
		Disabled:    u.Disabled,
		CreatedAt:   u.CreatedAt.UTC().Format(time.RFC3339),
	}
	if u.LastLoginAt != nil && !u.LastLoginAt.IsZero() {
		s := u.LastLoginAt.UTC().Format(time.RFC3339)
		row.LastLoginAt = &s
	}
	return row
}

func runUsersCreate(cmd *cobra.Command, args []string) error {
	username := args[0]
	role := mustGetString(cmd, "role")
	displayName := mustGetString(cmd, "display-name")
	email := mustGetString(cmd, "email")

	if !auth.ValidUsername(username) {
		return fmt.Errorf("invalid username %q: must match [a-z0-9_.-]{3,64}", username)
	}
	if !auth.IsValidRole(role) {
		return fmt.Errorf("invalid role %q: must be admin, editor, or viewer", role)
	}
	if displayName == "" {
		displayName = username
	}
	password, err := promptNewPassword()
	if err != nil {
		return err
	}

	cfg := config.Load()
	deps, err := initUsersDeps(cfg)
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	u := &database.UserWithSecret{
		User: database.User{
			Username:    username,
			DisplayName: displayName,
			Email:       email,
			Role:        role,
		},
		PasswordHash: hash,
	}
	ctx := cmd.Context()
	if err := deps.users.CreateUser(ctx, u); err != nil {
		if errors.Is(err, database.ErrUsernameTaken) {
			return fmt.Errorf("user %q already exists", username)
		}
		return fmt.Errorf("create user: %w", err)
	}
	deps.logCLI(ctx, audit.ActionUserCreate, u.UID, map[string]any{
		"username": u.Username, "role": u.Role,
	})
	fmt.Printf("Created user %q (role=%s, uid=%s)\n", u.Username, u.Role, u.UID)
	return nil
}

func runUsersSetPassword(cmd *cobra.Command, args []string) error {
	username := args[0]
	password, err := promptNewPassword()
	if err != nil {
		return err
	}
	cfg := config.Load()
	deps, err := initUsersDeps(cfg)
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	user, err := lookupUserByUsername(ctx, deps.users, username)
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := deps.users.SetPassword(ctx, user.UID, hash); err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	deps.logCLI(ctx, audit.ActionUserPasswordReset, user.UID, map[string]any{
		"username": user.Username,
	})
	fmt.Printf("Password reset for %q\n", user.Username)
	return nil
}

func runUsersDelete(cmd *cobra.Command, args []string) error {
	username := args[0]
	skipConfirm := mustGetBool(cmd, "yes")

	cfg := config.Load()
	deps, err := initUsersDeps(cfg)
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	user, err := lookupUserByUsername(ctx, deps.users, username)
	if err != nil {
		return err
	}
	if err := auth.EnsureNotLastAdmin(ctx, deps.users, user, user.UID); err != nil {
		if errors.Is(err, auth.ErrLastAdmin) {
			// Return the sentinel verbatim so the CLI prints a clean
			// "cannot delete the last admin" with no wrapping prefix.
			return err //nolint:wrapcheck // sentinel passthrough for user-facing message
		}
		return fmt.Errorf("guard last admin: %w", err)
	}
	if !skipConfirm && !confirmDelete(user.Username, user.Role) {
		fmt.Println("Aborted.")
		return nil
	}
	if err := deps.users.DeleteUser(ctx, user.UID); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	deps.logCLI(ctx, audit.ActionUserDelete, user.UID, map[string]any{
		"username": user.Username,
	})
	fmt.Printf("Deleted user %q\n", user.Username)
	return nil
}

// lookupUserByUsername fetches a user by username, returning a friendly
// error when no row exists.
func lookupUserByUsername(
	ctx context.Context, repo database.UserWriter, username string,
) (*database.User, error) {
	withSecret, err := repo.GetUserByUsername(ctx, username)
	if errors.Is(err, database.ErrNotFound) {
		return nil, fmt.Errorf("user %q not found", username)
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	u := withSecret.User
	return &u, nil
}

// promptNewPassword reads a new password from the TTY, with confirmation,
// and validates the minimum length. Refuses to run when stdin is not a
// TTY — scripted callers should provide a wrapper that talks to the
// REST API instead of trying to pipe a password through here.
func promptNewPassword() (string, error) {
	// #nosec G115 -- os.Stdin.Fd() returns a small file descriptor that
	// always fits in an int; the uintptr -> int conversion is safe.
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("password input requires a TTY (run from an interactive shell)")
	}
	fmt.Fprint(os.Stderr, "New password: ")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	fmt.Fprint(os.Stderr, "Confirm new password: ")
	second, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if !bytes.Equal(first, second) {
		return "", errors.New("passwords do not match")
	}
	if len(first) < auth.MinPasswordLength {
		return "", fmt.Errorf("password too short (minimum %d characters)", auth.MinPasswordLength)
	}
	return string(first), nil
}

func confirmDelete(username, role string) bool {
	fmt.Fprintf(os.Stderr, "Delete user %q (role=%s)? [y/N] ", username, role)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", yesAnswerLiteral:
		return true
	}
	return false
}
