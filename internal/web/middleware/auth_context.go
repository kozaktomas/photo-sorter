package middleware

import (
	"context"
	"net/http"
	"slices"
)

const authInfoContextKey contextKey = "auth_info"

// AuthInfo is the slim view of the authenticated caller that downstream
// handlers read from the request context. It is populated by RequireAuth
// from the active session, so handlers can role-gate without re-fetching
// the user row.
type AuthInfo struct {
	UserUID  string
	Username string
	Role     string
	// ReadOnly is true for a machine principal authenticated by a read-scope
	// API token. Such a caller already carries the viewer role, so every
	// existing role check rejects its writes; this flag exists so a handler
	// can tell "a human viewer" from "an export bot" when that distinction
	// matters (e.g. in an error message).
	ReadOnly bool
}

// SetAuthInfoInContext attaches an AuthInfo to the context. RequireAuth
// uses this internally; tests can call it directly to construct a context
// that mimics a logged-in caller.
func SetAuthInfoInContext(ctx context.Context, info *AuthInfo) context.Context {
	return context.WithValue(ctx, authInfoContextKey, info)
}

// GetAuthInfo returns the AuthInfo previously attached by RequireAuth. The
// second return value is false when no AuthInfo is present (e.g. on
// unauthenticated routes).
func GetAuthInfo(ctx context.Context) (*AuthInfo, bool) {
	info, ok := ctx.Value(authInfoContextKey).(*AuthInfo)
	if !ok || info == nil {
		return nil, false
	}
	return info, true
}

// MustGetAuthInfo returns the AuthInfo from the context. If none is
// present, it writes a 401 JSON response and returns nil — callers must
// return immediately when they receive a nil result.
func MustGetAuthInfo(ctx context.Context, w http.ResponseWriter) *AuthInfo {
	info, ok := GetAuthInfo(ctx)
	if !ok {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return nil
	}
	return info
}

// RequireRole returns a middleware that allows the request through only when
// the caller's role matches one of the supplied values. Missing AuthInfo
// yields 401; a role mismatch yields 403. Pass at least one role — calling
// with no roles is a programmer error and produces an always-403 handler.
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info, ok := GetAuthInfo(r.Context())
			if !ok {
				http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
				return
			}
			if !slices.Contains(roles, info.Role) {
				http.Error(w, `{"error": "forbidden"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
