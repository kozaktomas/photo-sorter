package middleware

import (
	"net/http"
	"time"
)

// NoWriteDeadline disables the per-connection write deadline for the wrapped
// handler. This is the per-request opt-out for http.Server.WriteTimeout,
// required for long-running streaming endpoints (SSE progress streams, large
// PDF downloads, synchronous book export, multipart uploads).
//
// If the underlying ResponseWriter doesn't support SetWriteDeadline
// (e.g. httptest.ResponseRecorder) the call returns http.ErrNotSupported;
// we ignore the error — the request still proceeds under the server-level
// WriteTimeout, which is the existing behaviour.
func NoWriteDeadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
		next.ServeHTTP(w, r)
	})
}
