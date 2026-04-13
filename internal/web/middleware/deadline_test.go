package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// deadlineRecorder captures SetWriteDeadline calls without a real TCP conn.
// http.NewResponseController walks Unwrap() chains to find our method.
type deadlineRecorder struct {
	http.ResponseWriter
	called   bool
	deadline time.Time
	err      error
}

func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	d.called = true
	d.deadline = t
	return d.err
}

func (d *deadlineRecorder) Unwrap() http.ResponseWriter {
	return d.ResponseWriter
}

func TestNoWriteDeadline_ZerosDeadline(t *testing.T) {
	rec := &deadlineRecorder{ResponseWriter: httptest.NewRecorder()}
	handler := NoWriteDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))

	if !rec.called {
		t.Fatal("expected SetWriteDeadline to be called")
	}
	if !rec.deadline.IsZero() {
		t.Errorf("expected zero-value deadline, got %v", rec.deadline)
	}
}

func TestNoWriteDeadline_IgnoresUnsupportedResponseWriter(t *testing.T) {
	// Plain httptest.ResponseRecorder does not implement SetWriteDeadline,
	// so NewResponseController returns ErrNotSupported. Middleware must
	// swallow that without breaking the handler chain.
	rec := httptest.NewRecorder()
	called := false
	handler := NoWriteDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	}))
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))

	if !called {
		t.Fatal("handler should still run when deadline is unsupported")
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("expected status %d, got %d", http.StatusTeapot, rec.Code)
	}
}

func TestNoWriteDeadline_IgnoresSetDeadlineError(t *testing.T) {
	rec := &deadlineRecorder{
		ResponseWriter: httptest.NewRecorder(),
		err:            errors.New("boom"),
	}
	handler := NoWriteDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))

	if !rec.called {
		t.Fatal("expected SetWriteDeadline to be called")
	}
}
