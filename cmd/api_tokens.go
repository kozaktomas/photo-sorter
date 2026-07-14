package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/audit"
	"github.com/kozaktomas/photo-sorter/internal/auth"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/spf13/cobra"
)

var apiTokensCmd = &cobra.Command{
	Use:   "api-tokens",
	Short: "Manage long-lived read-only API tokens for machine clients",
	Long: `Manage the long-lived, read-only bearer tokens used by machine clients
(most notably the Kukátko migration exporter).

A session cookie is the wrong credential for a bulk export: it expires after
30 days and is tied to a session row that the cleanup loop eventually deletes,
so a long-running job can die halfway through. An API token has no such
lifetime, and is read-only — it authenticates as the viewer role AND is
refused outright on any non-GET/HEAD/OPTIONS request, so it cannot write.

Clients send it as:  Authorization: Bearer psat_...

The raw token is displayed exactly once, at creation. Only its SHA-256 is
stored, so a lost token cannot be recovered — revoke it and mint a new one.

Mutating subcommands append rows to audit_log with actor=cli.`,
}

var apiTokensListCmd = &cobra.Command{
	Use:   "list",
	Short: "List API tokens (revoked and expired ones included)",
	RunE:  runAPITokensList,
}

var apiTokensCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Mint a new read-only API token and print it once",
	Long: `Mint a read-only API token. The raw value is printed once and never
stored — copy it immediately.

By default the token never expires, which is what a migration run wants; pass
--expires-in to bound it.

Examples:
  photo-sorter api-tokens create kukatko-migration
  photo-sorter api-tokens create ci-export --expires-in=720h`,
	Args: cobra.ExactArgs(1),
	RunE: runAPITokensCreate,
}

var apiTokensRevokeCmd = &cobra.Command{
	Use:   "revoke <uid>",
	Short: "Revoke an API token by UID",
	Long: `Revoke an API token. The row is kept (soft revoke) so the audit trail
still shows the token ever existed. Revocation takes effect immediately: the
auth path re-checks revoked_at on every request.

Example:
  photo-sorter api-tokens revoke t3k9x2mq7n4vb8zc`,
	Args: cobra.ExactArgs(1),
	RunE: runAPITokensRevoke,
}

func init() {
	rootCmd.AddCommand(apiTokensCmd)
	apiTokensCmd.AddCommand(apiTokensListCmd)
	apiTokensCmd.AddCommand(apiTokensCreateCmd)
	apiTokensCmd.AddCommand(apiTokensRevokeCmd)

	apiTokensListCmd.Flags().Bool("json", false, "Output as JSON instead of a table")
	apiTokensCreateCmd.Flags().Duration("expires-in", 0,
		"Token lifetime (e.g. 720h). Zero (default) means the token never expires.")
	apiTokensCreateCmd.Flags().Bool("json", false, "Output as JSON instead of human-readable text")
}

// apiTokensDeps bundles the repositories the api-tokens subcommands need. As
// with usersDeps, the audit-log repo is opened alongside the token repo so
// every CLI mutation lands in the audit trail just like a REST one would.
type apiTokensDeps struct {
	tokens database.APITokenWriter
	audit  *audit.Logger
}

// initAPITokensDeps opens the Postgres pool and returns the repositories the
// api-tokens subcommands operate on. It fails when DATABASE_URL is unset.
func initAPITokensDeps(cfg *config.Config) (*apiTokensDeps, error) {
	if cfg.Database.URL == "" {
		return nil, errors.New("DATABASE_URL environment variable is required")
	}
	if err := postgres.Initialize(&cfg.Database); err != nil {
		return nil, fmt.Errorf("initialise PostgreSQL: %w", err)
	}
	pool := postgres.GetGlobalPool()
	return &apiTokensDeps{
		tokens: postgres.NewAPITokenRepository(pool),
		audit:  audit.NewLogger(postgres.NewAuditLogRepository(pool)),
	}, nil
}

// logCLI records an audit event for a CLI-initiated token mutation, stamping
// metadata.actor="cli" so the audit viewer can tell it from web traffic.
func (d *apiTokensDeps) logCLI(
	ctx context.Context, action, entityUID string, metadata map[string]any,
) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["actor"] = "cli"
	d.audit.LogAnonymous(ctx, action, audit.EntityAPIToken, entityUID, "cli", metadata)
}

// runAPITokensCreate mints a token, persists its hash, and prints the raw
// value exactly once.
func runAPITokensCreate(cmd *cobra.Command, args []string) error {
	name := args[0]
	if name == "" {
		return errors.New("token name must not be empty")
	}
	expiresIn := mustGetDuration(cmd, "expires-in")
	jsonOut := mustGetBool(cmd, "json")

	cfg := config.Load()
	deps, err := initAPITokensDeps(cfg)
	if err != nil {
		return err
	}

	raw, hash, err := auth.GenerateAPIToken()
	if err != nil {
		return fmt.Errorf("mint api token: %w", err)
	}

	var expiresAt *time.Time
	if expiresIn > 0 {
		t := time.Now().Add(expiresIn)
		expiresAt = &t
	}

	token := &database.APIToken{
		UID:       postgres.NewAPITokenUID(),
		Name:      name,
		TokenHash: hash,
		Scope:     auth.APITokenScopeRead,
		ExpiresAt: expiresAt,
	}
	if err := deps.tokens.CreateAPIToken(cmd.Context(), token); err != nil {
		return fmt.Errorf("create api token: %w", err)
	}
	deps.logCLI(cmd.Context(), audit.ActionAPITokenCreate, token.UID, map[string]any{
		"name":  name,
		"scope": token.Scope,
	})

	return renderCreatedAPIToken(token, raw, jsonOut)
}

// renderCreatedAPIToken prints a freshly minted token. The raw value appears
// here and nowhere else, ever.
func renderCreatedAPIToken(token *database.APIToken, raw string, jsonOut bool) error {
	if jsonOut {
		out := map[string]any{
			"uid":   token.UID,
			"name":  token.Name,
			"scope": token.Scope,
			"token": raw,
		}
		if token.ExpiresAt != nil {
			out["expires_at"] = token.ExpiresAt.UTC().Format(time.RFC3339)
		}
		if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
			return fmt.Errorf("encode api token: %w", err)
		}
		return nil
	}

	expiry := "never"
	if token.ExpiresAt != nil {
		expiry = token.ExpiresAt.UTC().Format(time.RFC3339)
	}
	fmt.Printf("API token created.\n\n")
	fmt.Printf("  UID:     %s\n", token.UID)
	fmt.Printf("  Name:    %s\n", token.Name)
	fmt.Printf("  Scope:   %s (read-only)\n", token.Scope)
	fmt.Printf("  Expires: %s\n\n", expiry)
	fmt.Printf("  Token:   %s\n\n", raw)
	fmt.Printf("This is the only time the token is shown — copy it now.\n")
	fmt.Printf("Use it as:  curl -H 'Authorization: Bearer %s' ...\n", raw)
	return nil
}

// runAPITokensList prints every token row, newest first.
func runAPITokensList(cmd *cobra.Command, _ []string) error {
	jsonOut := mustGetBool(cmd, "json")
	cfg := config.Load()
	deps, err := initAPITokensDeps(cfg)
	if err != nil {
		return err
	}
	tokens, err := deps.tokens.ListAPITokens(cmd.Context())
	if err != nil {
		return fmt.Errorf("list api tokens: %w", err)
	}
	if jsonOut {
		return renderAPITokensJSON(tokens)
	}
	return renderAPITokensTable(tokens)
}

// apiTokenState summarises a token's liveness for display: revoked and
// expired are distinct states an operator needs to tell apart, even though
// the auth path rejects both identically.
func apiTokenState(t database.APIToken) string {
	switch {
	case t.RevokedAt != nil:
		return "revoked"
	case t.ExpiresAt != nil && !t.ExpiresAt.After(time.Now()):
		return "expired"
	default:
		return "active"
	}
}

// renderAPITokensJSON writes the token list as JSON. The hash is included
// (it is not a secret — it cannot be replayed as a bearer value) but the raw
// token never is, because it was never stored.
func renderAPITokensJSON(tokens []database.APIToken) error {
	rows := make([]map[string]any, 0, len(tokens))
	for _, t := range tokens {
		row := map[string]any{
			"uid":        t.UID,
			"name":       t.Name,
			"scope":      t.Scope,
			"state":      apiTokenState(t),
			"created_at": t.CreatedAt.UTC().Format(time.RFC3339),
		}
		if t.ExpiresAt != nil {
			row["expires_at"] = t.ExpiresAt.UTC().Format(time.RFC3339)
		}
		if t.LastUsedAt != nil {
			row["last_used_at"] = t.LastUsedAt.UTC().Format(time.RFC3339)
		}
		if t.RevokedAt != nil {
			row["revoked_at"] = t.RevokedAt.UTC().Format(time.RFC3339)
		}
		rows = append(rows, row)
	}
	if err := json.NewEncoder(os.Stdout).Encode(rows); err != nil {
		return fmt.Errorf("encode api tokens: %w", err)
	}
	return nil
}

// renderAPITokensTable writes the token list as an aligned table.
func renderAPITokensTable(tokens []database.APIToken) error {
	if len(tokens) == 0 {
		fmt.Println("No API tokens.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "UID\tNAME\tSCOPE\tSTATE\tEXPIRES\tLAST USED\tCREATED")
	fmt.Fprintln(w, "---\t----\t-----\t-----\t-------\t---------\t-------")
	for _, t := range tokens {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			t.UID, t.Name, t.Scope, apiTokenState(t),
			formatOptionalTime(t.ExpiresAt, "never"),
			formatOptionalTime(t.LastUsedAt, "-"),
			t.CreatedAt.UTC().Format(time.RFC3339),
		)
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush table: %w", err)
	}
	return nil
}

// formatOptionalTime renders a nullable timestamp, substituting fallback for
// nil (e.g. "never" for an absent expiry, "-" for a token never used).
func formatOptionalTime(t *time.Time, fallback string) string {
	if t == nil || t.IsZero() {
		return fallback
	}
	return t.UTC().Format(time.RFC3339)
}

// runAPITokensRevoke soft-revokes a token by UID. Revoking an already-revoked
// token succeeds quietly — the desired end state is already true.
func runAPITokensRevoke(cmd *cobra.Command, args []string) error {
	uid := args[0]
	cfg := config.Load()
	deps, err := initAPITokensDeps(cfg)
	if err != nil {
		return err
	}
	if err := deps.tokens.RevokeAPIToken(cmd.Context(), uid); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return fmt.Errorf("no API token with UID %q", uid)
		}
		return fmt.Errorf("revoke api token: %w", err)
	}
	deps.logCLI(cmd.Context(), audit.ActionAPITokenRevoke, uid, nil)
	fmt.Printf("API token %s revoked.\n", uid)
	return nil
}
