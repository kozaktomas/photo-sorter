package middleware

import (
	"context"
	"net/http"
	"sync"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
)

const (
	photoPrismContextKey        contextKey = "photoprism"
	photoPrismFactoryContextKey contextKey = "photoprism_factory"
)

// photoPrismFactory is the request-scoped, lazily evaluated builder behind
// MustGetPhotoPrism. Keeping it lazy means a request to one of the native
// (Postgres-backed) endpoints never triggers a PhotoPrism login, while the
// face / upload paths that still need a real client transparently get one
// on first call.
type photoPrismFactory struct {
	cfg     *config.Config
	session *Session
	mu      sync.Mutex
	cached  *photoprism.PhotoPrism
	err     error
}

// get returns the request-scoped PhotoPrism client, creating it on the
// first call and caching the result for the rest of the request. The
// session token is preferred; an empty token (native session) falls back
// to the server-level PHOTOPRISM_USERNAME / PHOTOPRISM_PASSWORD so the
// face and upload code paths keep working until they are themselves
// migrated off PhotoPrism.
func (f *photoPrismFactory) get() (*photoprism.PhotoPrism, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cached != nil || f.err != nil {
		return f.cached, f.err
	}
	if f.session != nil && f.session.Token != "" {
		f.cached, f.err = photoprism.NewPhotoPrismFromToken(
			f.cfg.PhotoPrism.URL,
			f.session.Token, f.session.DownloadToken, f.session.UserUID,
		)
		return f.cached, f.err
	}
	f.cached, f.err = photoprism.NewPhotoPrism(
		f.cfg.PhotoPrism.URL, f.cfg.PhotoPrism.Username, f.cfg.PhotoPrism.GetPassword(),
	)
	return f.cached, f.err
}

// WithPhotoPrismClient attaches a lazy PhotoPrism client factory to the
// request context. It must run after RequireAuth so a session is already
// available. The factory is evaluated on demand by MustGetPhotoPrism — a
// request handled entirely by native code paths costs nothing extra.
func WithPhotoPrismClient(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session := GetSessionFromContext(r.Context())
			if session == nil {
				http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
				return
			}
			factory := &photoPrismFactory{cfg: cfg, session: session}
			ctx := context.WithValue(r.Context(), photoPrismFactoryContextKey, factory)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetPhotoPrismFromContext retrieves the PhotoPrism client that was put on
// the context directly (test path via SetPhotoPrismInContext). Returns nil
// if none was set; callers that want lazy creation should use
// MustGetPhotoPrism instead.
func GetPhotoPrismFromContext(ctx context.Context) *photoprism.PhotoPrism {
	pp, ok := ctx.Value(photoPrismContextKey).(*photoprism.PhotoPrism)
	if !ok {
		return nil
	}
	return pp
}

// MustGetPhotoPrism returns the PhotoPrism client for the current request.
// It first honours any explicit client placed on the context (used by
// tests), then falls back to invoking the request-scoped factory installed
// by WithPhotoPrismClient. On failure it writes an error response and
// returns nil — callers must return immediately when they receive a nil.
func MustGetPhotoPrism(ctx context.Context, w http.ResponseWriter) *photoprism.PhotoPrism {
	if pp := GetPhotoPrismFromContext(ctx); pp != nil {
		return pp
	}
	factory, ok := ctx.Value(photoPrismFactoryContextKey).(*photoPrismFactory)
	if !ok {
		http.Error(w, `{"error": "PhotoPrism client not available"}`, http.StatusInternalServerError)
		return nil
	}
	pp, err := factory.get()
	if err != nil {
		http.Error(w, `{"error": "failed to connect to PhotoPrism"}`, http.StatusInternalServerError)
		return nil
	}
	return pp
}

// SetPhotoPrismInContext adds a PhotoPrism client to the context.
// This is primarily for testing - use WithPhotoPrismClient middleware in production.
func SetPhotoPrismInContext(ctx context.Context, pp *photoprism.PhotoPrism) context.Context {
	return context.WithValue(ctx, photoPrismContextKey, pp)
}
