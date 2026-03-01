package middleware

import (
	"net/http"
	"os"
	"strings"
)

// parseAllowedOrigins reads WEB_ALLOWED_ORIGINS env var and returns a set of allowed origins.
// In addition, localhost origins on any port are always allowed for development.
func parseAllowedOrigins() map[string]struct{} {
	origins := make(map[string]struct{})
	if env := os.Getenv("WEB_ALLOWED_ORIGINS"); env != "" {
		for o := range strings.SplitSeq(env, ",") {
			o = strings.TrimSpace(o)
			if o != "" {
				origins[o] = struct{}{}
			}
		}
	}
	return origins
}

// isLocalhostOrigin returns true if the origin is http(s)://localhost:<port>.
func isLocalhostOrigin(origin string) bool {
	for _, prefix := range []string{"http://localhost:", "http://localhost", "https://localhost:", "https://localhost"} {
		if origin == prefix || strings.HasPrefix(origin, prefix) {
			return true
		}
	}
	return false
}

// isOriginAllowed checks whether a request origin should receive CORS headers.
func isOriginAllowed(origin string, allowed map[string]struct{}) bool {
	if origin == "" {
		return false
	}
	// Always allow localhost for development.
	if isLocalhostOrigin(origin) {
		return true
	}
	_, ok := allowed[origin]
	return ok
}

// CORS returns middleware that handles CORS headers with an origin whitelist.
// Allowed origins come from the WEB_ALLOWED_ORIGINS environment variable (comma-separated).
// Localhost origins are always permitted for development convenience.
func CORS() func(http.Handler) http.Handler {
	allowed := parseAllowedOrigins()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if isOriginAllowed(origin, allowed) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-Requested-With")
			w.Header().Set("Access-Control-Max-Age", "86400")

			// Handle preflight requests.
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders returns middleware that sets Content-Security-Policy and other security headers.
func SecurityHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Security-Policy",
				"default-src 'self'; img-src 'self' data: blob:; "+
					"style-src 'self' 'unsafe-inline'; font-src 'self' data:")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			next.ServeHTTP(w, r)
		})
	}
}
