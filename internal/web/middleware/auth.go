package middleware

import (
	"context"
	"net/http"
)

type contextKey string

const sessionContextKey contextKey = "session"

// RequireAuth is middleware that requires a valid session. It exposes the
// session struct on the context for code paths that still need PhotoPrism
// tokens (faces / upload), and the slimmer AuthInfo for handlers that only
// care about the native user identity.
func RequireAuth(sm *SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session := sm.GetSessionFromRequest(r)
			if session == nil {
				http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), sessionContextKey, session)
			ctx = SetAuthInfoInContext(ctx, &AuthInfo{
				UserUID: session.UserUID,
				Role:    session.Role,
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
