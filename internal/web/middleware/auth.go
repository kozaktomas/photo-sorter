package middleware

import (
	"context"
	"net/http"

	"github.com/kozaktomas/photo-sorter/internal/audit"
)

type contextKey string

const sessionContextKey contextKey = "session"

// RequireAuth is middleware that requires a valid session. It exposes the
// session struct on the context for code paths that still need PhotoPrism
// tokens (faces / upload), and the slimmer AuthInfo for handlers that only
// care about the native user identity.
//
// It also enforces the read-only scope of an API token principal: such a
// caller may only use safe HTTP methods. This is the outermost of three
// independent write gates (the other two being the viewer role, which fails
// both auth.HasWriteAccess and handlers.requireWriteRole), and the only one
// that holds even for a handler that forgets to check a role at all.
func RequireAuth(sm *SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session := sm.GetSessionFromRequest(r)
			if session == nil {
				http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
				return
			}
			if session.ReadOnly && !isSafeMethod(r.Method) {
				http.Error(w, `{"error": "read-only credential"}`, http.StatusForbidden)
				return
			}

			ctx := context.WithValue(r.Context(), sessionContextKey, session)
			ctx = SetAuthInfoInContext(ctx, &AuthInfo{
				UserUID:  session.UserUID,
				Role:     session.Role,
				ReadOnly: session.ReadOnly,
			})
			// Refresh the audit request context so audit.Logger.Log
			// records the authenticated user_uid. The global audit
			// middleware ran before us and stamped only IP/UA; now
			// that we know who the caller is, replace the RC.
			ctx = audit.WithRequestContext(ctx, audit.RequestContext{
				UserUID:   session.UserUID,
				IP:        clientIP(r),
				UserAgent: r.UserAgent(),
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetSessionFromContext retrieves the session from the request context.
func GetSessionFromContext(ctx context.Context) *Session {
	session, ok := ctx.Value(sessionContextKey).(*Session)
	if !ok {
		return nil
	}
	return session
}

// SetSessionInContext adds a session to the context.
// This is primarily for testing - use RequireAuth middleware in production.
func SetSessionInContext(ctx context.Context, session *Session) context.Context {
	return context.WithValue(ctx, sessionContextKey, session)
}
