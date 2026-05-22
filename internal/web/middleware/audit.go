package middleware

import (
	"net/http"
	"strings"

	"github.com/kozaktomas/photo-sorter/internal/audit"
)

// WithAuditLogger injects the audit Logger and the per-request audit
// context (user identity, client IP, User-Agent) into the request
// context. Handlers retrieve the logger via audit.FromContext(ctx) and
// call Logger.Log to record a single audit row after a successful
// mutation.
//
// The middleware is safe to use on both authenticated and anonymous
// routes: when no AuthInfo is on the context (e.g. /api/v1/auth/login
// before the credentials are checked, or the public share endpoints),
// the audit context carries an empty UserUID and the logger records
// "anonymous" rows.
func WithAuditLogger(logger *audit.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rc := extractAuditRequestContext(r)
			ctx := audit.WithRequestContext(r.Context(), rc)
			ctx = audit.WithLogger(ctx, logger)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extractAuditRequestContext builds an audit.RequestContext from a
// live HTTP request: it pulls the AuthInfo set by RequireAuth (if any),
// the client IP (already de-proxied by chiMiddleware.RealIP), and the
// User-Agent. Kept inside the middleware package to avoid a cyclic
// import between audit/ and middleware/.
func extractAuditRequestContext(r *http.Request) audit.RequestContext {
	rc := audit.RequestContext{
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	}
	if info, ok := GetAuthInfo(r.Context()); ok && info != nil {
		rc.UserUID = info.UserUID
	}
	return rc
}

// clientIP returns the most plausible source IP for the request.
// chiMiddleware.RealIP runs before us in the stack, so r.RemoteAddr is
// already the real client address when the deployment uses a trusted
// proxy. We strip any trailing port so the column stores a bare IP.
func clientIP(r *http.Request) string {
	addr := r.RemoteAddr
	if i := strings.LastIndex(addr, ":"); i > 0 && strings.Count(addr, ":") == 1 {
		return addr[:i]
	}
	if strings.HasPrefix(addr, "[") {
		if end := strings.Index(addr, "]"); end > 0 {
			return addr[1:end]
		}
	}
	return addr
}
