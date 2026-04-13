package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"

	middleware "github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// TestRouteTimeoutSplit documents the routing split used by setupRoutes:
// a "short" sub-group carries chiMiddleware.Timeout and its handler's
// request context is cancelled with context.DeadlineExceeded once the
// handler runs past the timeout, while a "long" sub-group has no timeout
// and the handler's context is not cancelled. Mirrors setupRoutes without
// wiring a real server.
func TestRouteTimeoutSplit(t *testing.T) {
	t.Parallel()

	shortTimeout := 50 * time.Millisecond
	handlerDuration := 200 * time.Millisecond

	shortCtxErr := make(chan error, 1)
	longCtxErr := make(chan error, 1)

	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(chiMiddleware.Timeout(shortTimeout))
			r.Get("/short", func(w http.ResponseWriter, req *http.Request) {
				select {
				case <-time.After(handlerDuration):
					shortCtxErr <- nil
					w.WriteHeader(http.StatusOK)
				case <-req.Context().Done():
					shortCtxErr <- req.Context().Err()
					// Don't write — chi's Timeout middleware already wrote 504.
				}
			})
		})
		r.Group(func(r chi.Router) {
			r.Use(middleware.NoWriteDeadline)
			r.Get("/long", func(w http.ResponseWriter, req *http.Request) {
				select {
				case <-time.After(handlerDuration):
					longCtxErr <- nil
					w.WriteHeader(http.StatusOK)
				case <-req.Context().Done():
					longCtxErr <- req.Context().Err()
				}
			})
		})
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	doGet := func(path string) {
		t.Helper()
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+path, nil)
		if err != nil {
			t.Fatalf("build %s request: %v", path, err)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}

	// Short route: context should be cancelled with DeadlineExceeded.
	doGet("/api/short")

	select {
	case err := <-shortCtxErr:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("short handler expected context.DeadlineExceeded, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("short handler did not report within 2s")
	}

	// Long route: context should NOT have been cancelled.
	doGet("/api/long")

	select {
	case err := <-longCtxErr:
		if err != nil {
			t.Errorf("long handler expected no context error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("long handler did not report within 2s")
	}
}
