package middleware

import (
	"context"
	"net/http"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
)

const photoPrismContextKey contextKey = "photoprism"

// WithPhotoPrismClient is middleware that creates a PhotoPrism client and adds it to the context.
// Requires a valid session with tokens. Should be used after RequireAuth middleware.
func WithPhotoPrismClient(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session := GetSessionFromContext(r.Context())

			if session == nil || session.Token == "" {
				http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
				return
			}

			pp, err := photoprism.NewPhotoPrismFromToken(cfg.PhotoPrism.URL, session.Token, session.DownloadToken)
			if err != nil {
				http.Error(w, `{"error": "failed to connect to PhotoPrism"}`, http.StatusInternalServerError)
				return
			}

			ctx := context.WithValue(r.Context(), photoPrismContextKey, pp)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetPhotoPrismFromContext retrieves the PhotoPrism client from the request context.
// Returns nil if no client is available.
func GetPhotoPrismFromContext(ctx context.Context) *photoprism.PhotoPrism {
	pp, ok := ctx.Value(photoPrismContextKey).(*photoprism.PhotoPrism)
	if !ok {
		return nil
	}
	return pp
}

// MustGetPhotoPrism retrieves the PhotoPrism client from context.
// If not available, writes an error response and returns nil.
// Handlers should return immediately after receiving nil.
func MustGetPhotoPrism(ctx context.Context, w http.ResponseWriter) *photoprism.PhotoPrism {
	pp := GetPhotoPrismFromContext(ctx)
	if pp == nil {
		http.Error(w, `{"error": "PhotoPrism client not available"}`, http.StatusInternalServerError)
		return nil
	}
	return pp
}
