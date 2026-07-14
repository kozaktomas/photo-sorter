package middleware

import (
	"context"
	"log"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/auth"
	"github.com/kozaktomas/photo-sorter/internal/database"
)

// APITokenStore resolves raw bearer tokens against the api_tokens table. It
// is the narrow slice of database.APITokenWriter that the auth path needs;
// declaring it here (rather than importing the full writer) keeps the
// middleware unable to mint or revoke tokens.
type APITokenStore interface {
	ResolveAPIToken(ctx context.Context, rawToken string) (*database.APIToken, error)
	TouchAPIToken(ctx context.Context, uid string) error
}

// apiTokenResolveTimeout bounds the auth-path lookup. It is generous relative
// to a single indexed SELECT but keeps a stalled database from pinning the
// request goroutine indefinitely.
const apiTokenResolveTimeout = 5 * time.Second

// SetAPITokenStore attaches the api_tokens store to the session manager,
// enabling `Authorization: Bearer psat_...` credentials. Sessions keep
// working unchanged when it is never called (e.g. in tests), because
// sessionFromAPIToken treats a nil store as "no token auth configured".
func (sm *SessionManager) SetAPITokenStore(store APITokenStore) {
	sm.apiTokens = store
}

// sessionFromAPIToken resolves a raw API token into a synthetic, read-only
// Session — or nil when the token is unknown, revoked, or expired.
//
// Returning a *Session (rather than only populating AuthInfo) is deliberate
// and load-bearing. The repo has two parallel write gates: the newer
// handlers check auth.HasWriteAccess(AuthInfo.Role), but ~57 older ones call
// handlers.requireWriteRole, which reads the *Session off the context and
// **treats a missing session as an admin** so that CLI/MCP callers keep
// working. A token principal that set only AuthInfo would therefore sail
// straight through that gate with full write access. Minting a Session with
// the viewer role satisfies both gates at once.
//
// RequireAuth applies a third, independent guard (unsafe HTTP methods are
// rejected outright for a read-only principal), so a scope escape would have
// to defeat all three.
func (sm *SessionManager) sessionFromAPIToken(ctx context.Context, raw string) *Session {
	if sm.apiTokens == nil {
		return nil
	}

	lookupCtx, cancel := context.WithTimeout(ctx, apiTokenResolveTimeout)
	defer cancel()

	token, err := sm.apiTokens.ResolveAPIToken(lookupCtx, raw)
	if err != nil {
		log.Printf("Warning: failed to resolve API token: %v", err)
		return nil
	}
	if token == nil {
		return nil
	}

	// Record usage without blocking the request or inheriting its
	// cancellation — the client gets its response either way, and the store
	// throttles the write internally. The detached context is the point: a
	// bookkeeping write must survive the request it describes finishing.
	//nolint:gosec // G118: detaching from the request context is intentional here.
	go sm.touchAPIToken(token.UID)

	expiresAt := time.Now().Add(sessionDuration)
	if token.ExpiresAt != nil {
		expiresAt = *token.ExpiresAt
	}
	return &Session{
		ID:          apiTokenSessionID(token.UID),
		UserUID:     token.CreatedByUserUID,
		Role:        auth.RoleViewer,
		APITokenUID: token.UID,
		ReadOnly:    true,
		CreatedAt:   token.CreatedAt,
		ExpiresAt:   expiresAt,
	}
}

// touchAPIToken updates last_used_at on a detached context. Failures are
// logged and swallowed: a bookkeeping write must never fail a read.
func (sm *SessionManager) touchAPIToken(uid string) {
	ctx, cancel := context.WithTimeout(context.Background(), apiTokenResolveTimeout)
	defer cancel()
	if err := sm.apiTokens.TouchAPIToken(ctx, uid); err != nil {
		log.Printf("Warning: failed to record API token usage for %s: %v", uid, err)
	}
}

// apiTokenSessionID builds the synthetic session ID for a token principal.
// The prefix keeps it from ever colliding with a real session ID (which is
// base64 of 32 random bytes and contains no colon).
func apiTokenSessionID(tokenUID string) string {
	return "apitoken:" + tokenUID
}

// isSafeMethod reports whether an HTTP method only reads state. These are the
// only methods a read-only API token may use.
func isSafeMethod(method string) bool {
	switch method {
	case "GET", "HEAD", "OPTIONS":
		return true
	default:
		return false
	}
}

// tokenPrefixed reports whether a bearer value should be resolved against
// api_tokens rather than the session store.
func tokenPrefixed(bearer string) bool {
	return auth.IsAPIToken(bearer)
}
