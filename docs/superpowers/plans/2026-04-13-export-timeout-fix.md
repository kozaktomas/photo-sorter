# Photo Book Export Timeout Fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop photo-book PDF exports (and other long-running streaming endpoints) from being killed by a hardcoded 5-minute wall clock.

**Architecture:** Today `internal/web/server.go` applies `chiMiddleware.Timeout(5*time.Minute)` to every request and sets `http.Server.WriteTimeout` to 5 minutes. Any request whose total duration exceeds 5 minutes — the SSE progress stream during long `lualatex` passes, the download of a several-hundred-megabyte PDF over a residential link, a synchronous `ExportPDF` call from CLI/MCP, a large upload — has its request context cancelled (chi) and its TCP connection forcibly closed (Go). The export goroutine itself is fine because it runs on `context.Background()`; what breaks is the client's view of it.

The fix is three surgical changes:

1. **Split the API route group in `setupRoutes` into two groups:** a short group that keeps the existing 5-minute `chiMiddleware.Timeout` (default for normal CRUD endpoints), and a long group for SSE `*/events`, book export download / synchronous export / job-start, and the upload multipart endpoints. The long group has no timeout middleware.
2. **Apply a new `NoWriteDeadline` middleware on the long group** that calls `http.NewResponseController(w).SetWriteDeadline(time.Time{})` to lift the per-connection write deadline set by `http.Server.WriteTimeout` (Go 1.20+ API; repo is on Go 1.26). `WriteTimeout` stays at 5 min for short routes.
3. **Emit SSE comment heartbeats** (`: keepalive\n\n`) every 15s from `streamSSEEvents` so intermediate reverse proxies (nginx/traefik/cloudflare tunnel) don't close the idle connection during silent `compiling_pass1`/`compiling_pass2` phases.

**Tech Stack:** Go 1.26, `net/http`, `github.com/go-chi/chi/v5`, `httptest` for tests, existing `internal/web/middleware` package for new middleware.

---

## File Structure

- **Create:** `internal/web/middleware/deadline.go` — `NoWriteDeadline` chi-compatible middleware that disables the per-connection write deadline via `http.NewResponseController`.
- **Create:** `internal/web/middleware/deadline_test.go` — unit test verifying the middleware actually calls `SetWriteDeadline(time.Time{})`.
- **Modify:** `internal/web/routes.go` — split the authenticated route group into `short` (with default timeout) and `long` (no timeout, `NoWriteDeadline` applied); move SSE/download/export/upload routes into `long`.
- **Modify:** `internal/web/server.go` — add comment clarifying the split; no behavior change.
- **Modify:** `internal/web/handlers/sse.go` — add a 15-second heartbeat ticker to `streamSSEEvents`, write a comment frame on each tick, expose the interval as a package-level `var` so tests can shrink it.
- **Create:** `internal/web/handlers/sse_test.go` — unit test that uses a shrunken heartbeat interval and `httptest.NewRecorder`/`httptest.NewServer` to assert the heartbeat frame appears on a stream with no events.
- **Modify:** `internal/web/handlers/book_export_job.go` — reset the job's `expiresAt` to `bookExportUnconsumedTTL` on download completion too, so a user whose download takes >10 min (the consumed TTL) can still retry. *(safety net for large-file downloads; see Task 6)*
- **Modify:** `docs/API.md` — note that SSE streams send 15-second keepalive comments.

---

## Task 1: Add `NoWriteDeadline` middleware

**Files:**
- Create: `internal/web/middleware/deadline.go`
- Create: `internal/web/middleware/deadline_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/web/middleware/deadline_test.go
package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// deadlineRecorder captures SetWriteDeadline calls without a real TCP conn.
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

// Unwrap makes http.NewResponseController find our SetWriteDeadline.
func (d *deadlineRecorder) Unwrap() http.ResponseWriter {
	return d.ResponseWriter
}

func TestNoWriteDeadline_ZerosDeadline(t *testing.T) {
	rec := &deadlineRecorder{ResponseWriter: httptest.NewRecorder()}
	handler := NoWriteDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)

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
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)

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
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)

	if !rec.called {
		t.Fatal("expected SetWriteDeadline to be called")
	}
	// No panic/no 500 means success. Nothing else to assert.
}
```

- [ ] **Step 2: Run the test and confirm it fails to compile**

Run: `go test ./internal/web/middleware/ -run TestNoWriteDeadline -count=1`
Expected: FAIL — `undefined: NoWriteDeadline`.

- [ ] **Step 3: Implement the middleware**

```go
// internal/web/middleware/deadline.go
package middleware

import (
	"errors"
	"net/http"
)

// NoWriteDeadline disables the per-connection write deadline for the wrapped
// handler. This is the per-request opt-out for http.Server.WriteTimeout,
// required for long-running streaming endpoints (SSE progress streams, large
// PDF downloads, synchronous book export, multipart uploads).
//
// Go 1.20+ http.NewResponseController lets a handler extend or clear the
// write deadline of the underlying net.Conn independently of the server-wide
// WriteTimeout. We set the zero value, which means "no deadline".
//
// If the underlying ResponseWriter doesn't support SetWriteDeadline (e.g.,
// httptest.ResponseRecorder), NewResponseController returns
// http.ErrNotSupported and we silently proceed — the handler still runs.
func NoWriteDeadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		if err := rc.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
			// Log once per request so we notice if the server config drifts,
			// but never fail the request — a stale deadline is recoverable,
			// dropping the request is not.
			// (Intentionally no log package import here to avoid cycles;
			// production deploys have access-logs + metrics to spot this.)
			_ = err
		}
		next.ServeHTTP(w, r)
	})
}
```

(Note the `time` import needed — add it.)

```go
import (
	"errors"
	"net/http"
	"time"
)
```

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test ./internal/web/middleware/ -run TestNoWriteDeadline -count=1 -v`
Expected: PASS (all three subtests).

- [ ] **Step 5: Run `make lint` on the new file**

Run: `make lint`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/middleware/deadline.go internal/web/middleware/deadline_test.go
git commit -m "feat(web): add NoWriteDeadline middleware for long-running routes"
```

---

## Task 2: Split authenticated routes into short and long groups

**Files:**
- Modify: `internal/web/routes.go:43-163`

- [ ] **Step 1: Open `internal/web/routes.go` and read the current authenticated route group**

No code change yet — confirm the structure from lines 43-163: one `r.Group` with `RequireAuth` + `WithPhotoPrismClient`, then all routes.

- [ ] **Step 2: Replace the single group with a shared auth group that nests two sub-groups**

Find this block (around `routes.go:36`):

```go
	// API routes.
	s.router.Route("/api/v1", func(r chi.Router) {
		// Auth routes (no PhotoPrism client needed for login).
		r.Post("/auth/login", authHandler.Login)
		r.Post("/auth/logout", authHandler.Logout)
		r.Get("/auth/status", authHandler.Status)

		// All other routes require authentication and get a PhotoPrism client injected.
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(sessionManager))
			r.Use(middleware.WithPhotoPrismClient(s.config))
```

Replace it with:

```go
	// API routes.
	s.router.Route("/api/v1", func(r chi.Router) {
		// Auth routes (no PhotoPrism client needed for login).
		r.Post("/auth/login", authHandler.Login)
		r.Post("/auth/logout", authHandler.Logout)
		r.Get("/auth/status", authHandler.Status)

		// All other routes require authentication and get a PhotoPrism client injected.
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(sessionManager))
			r.Use(middleware.WithPhotoPrismClient(s.config))

			// --- Short-lived endpoints ---
			// Default 5-minute chi Timeout (inherited from server.go) and
			// 5-minute http.Server WriteTimeout apply here. Suitable for
			// normal CRUD where stuck requests should be killed promptly.
			r.Group(func(r chi.Router) {
```

Then, at the point where the `}` currently closes the single group (the end of the route list around `routes.go:162`), insert `})` for the short group and open the long group:

```go
				r.Get("/text-versions", textVersionsHandler.List)
				r.Post("/text-versions/{id}/restore", textVersionsHandler.Restore)
			})

			// --- Long-running / streaming endpoints ---
			// No chi Timeout. NoWriteDeadline lifts the per-connection
			// WriteTimeout so SSE progress streams, synchronous PDF
			// generation, large downloads, and multipart uploads can run
			// as long as the client stays connected. Cancellation still
			// propagates via r.Context() when the client disconnects.
			r.Group(func(r chi.Router) {
				// Explicitly zero the chi Timeout that's applied at the
				// server level, and disable the http.Server WriteTimeout.
				r.Use(noTimeout)
				r.Use(middleware.NoWriteDeadline)

				// SSE progress streams.
				r.Get("/sort/{jobId}/events", sortHandler.Events)
				r.Get("/upload/{jobId}/events", uploadHandler.GetJobEvents)
				r.Get("/process/{jobId}/events", processHandler.Events)
				r.Get("/book-export/{jobId}/events", booksHandler.StreamExportJobEvents)

				// Large or slow payloads.
				r.Post("/upload", uploadHandler.Upload)
				r.Post("/upload/job", uploadHandler.StartJob)
				r.Get("/books/{id}/export-pdf", booksHandler.ExportPDF)
				r.Get("/pages/{id}/export-pdf", booksHandler.ExportPagePDF)
				r.Get("/book-export/{jobId}/download", booksHandler.DownloadExport)
			})
		})
	})
```

Now delete the duplicate SSE/upload/download/export routes from the short group, since they have been moved into the long group. The short group must still contain every other route (`/albums`, `/labels`, `/photos`, `/sort` POST/GET/DELETE, the `/upload/{jobId}` cancel DELETE, `/config`, `/stats`, `/subjects`, `/faces/*`, `/process` POST/DELETE + `/rebuild-index` + `/sync-cache`, `/fonts`, books/chapters/sections/pages/slots CRUD, `/books/{id}/export-pdf/job` POST (just returns 202, stays short), `/book-export/{jobId}` GET/DELETE (quick polling), `/text/*`, `/text-versions/*`).

Concretely — **routes to keep in the short group:**
- All `/albums/*`
- All `/labels/*`
- All `/photos/*` (list/get/update/thumb/faces/etc.)
- `POST /sort`, `GET /sort/{jobId}`, `DELETE /sort/{jobId}` *(keep — the only SSE endpoint moves)*
- `DELETE /upload/{jobId}` *(cancel is quick; the events/upload endpoints move)*
- `/config`, `/stats`, `/fonts`
- `/subjects/*`, `/faces/*`
- `POST /process`, `DELETE /process/{jobId}`, `POST /process/rebuild-index`, `POST /process/sync-cache` *(keep — the events endpoint moves)*
- All books/chapters/sections/pages/slots CRUD
- `POST /books/{id}/export-pdf/job` *(just creates the job and returns 202)*
- `GET /book-export/{jobId}`, `DELETE /book-export/{jobId}` *(quick polling/cancel; events + download move)*
- `/text/*`
- `/text-versions/*`
- `GET /books/{id}/text-check-status`
- `POST /books/{id}/sections/{sectionId}/auto-layout`
- `GET /books/{id}/preflight`

**Routes moved to the long group** (remove from the short group):
- `GET /sort/{jobId}/events`
- `POST /upload`
- `POST /upload/job`
- `GET /upload/{jobId}/events`
- `GET /process/{jobId}/events`
- `GET /books/{id}/export-pdf`
- `GET /pages/{id}/export-pdf`
- `GET /book-export/{jobId}/events`
- `GET /book-export/{jobId}/download`

- [ ] **Step 3: Add the `noTimeout` helper to `routes.go`**

At the bottom of `routes.go` (above `contentTypeByExt`), add:

```go
// noTimeout undoes the server-level chiMiddleware.Timeout by replacing the
// request context with a fresh one inheriting only the values, not the
// deadline. Used for routes in the "long" group where the 5-minute timeout
// doesn't make sense (SSE streams, large downloads, multipart uploads).
//
// Cancellation still works via the client disconnect path: net/http cancels
// the request context when the underlying TCP connection closes, and that
// signal is independent of chi's timeout.
func noTimeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithoutCancel(r.Context())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
```

Note: `context.WithoutCancel` is Go 1.21+; repo is on Go 1.26. It gives us a context that inherits values (auth session, request ID) but drops the chi timeout's `Done` channel. **Important:** client disconnect cancellation is preserved because `net/http` sets it on a *different* context chain via the request body reader; it propagates via `r.Context()` being cancelled, and since we `context.WithoutCancel` *before* the handler runs we do lose the client-disconnect signal. Verify this behavior in Task 3.

Actually — revise: `context.WithoutCancel` drops **all** cancellation, including client disconnect. That's wrong for SSE, where we need to return when the client disconnects. Use this pattern instead, which only strips the chi deadline but preserves client cancellation by reusing the request body reader's context:

Use this implementation instead:

```go
// noTimeout removes the chi Timeout middleware's deadline by swapping the
// request context for one without it. Client-disconnect cancellation is
// preserved by watching r.Body's context indirectly via a goroutine that
// cancels on parent context's Done... this is tricky. Use the simpler
// approach: rely on the server-level detection of client disconnect (which
// goes through a different path via net/http's conn.cancelCtx).
//
// Actually — the simplest and correct approach is: instantiate a fresh
// context that gets cancelled when the underlying connection's context
// fires. But net/http doesn't expose that directly.
//
// The pragmatic fix: the chi Timeout context is the parent we want to
// escape, BUT its parent (the real request context from net/http) is what
// signals client disconnect. chi Timeout middleware does:
//     ctx, cancel := context.WithTimeout(r.Context(), timeout)
// so the disconnect signal lives in the grandparent. We can't access it
// without reimplementing chi middleware.
//
// The clean solution is to NOT apply chi Timeout to this group in the
// first place, rather than trying to strip it. See Step 4.
func noTimeout(next http.Handler) http.Handler {
	// Intentionally a no-op sentinel. The real trick is that chi Timeout
	// is applied at the top-level server.go via r.Use(), and there's no
	// way to opt a sub-group out of a middleware that was Use()d at the
	// parent router. We must therefore move chi Timeout off the parent
	// and onto the short group explicitly. See Task 3.
	return next
}
```

This step is primarily to reveal that stripping a parent middleware is impossible in chi, and force the real fix in Task 3. **Do not commit** this intermediate state — it's only here so the next step has context.

- [ ] **Step 4: Skip this intermediate commit; continue to Task 3**

Task 3 moves `chiMiddleware.Timeout` off the top-level router and onto the short group, which is the right way to opt the long group out of it. No commit yet.

---

## Task 3: Move `chiMiddleware.Timeout` from server-level to short-group-only

**Files:**
- Modify: `internal/web/server.go:56`
- Modify: `internal/web/routes.go` (the short group added in Task 2)

- [ ] **Step 1: Remove the server-level Timeout middleware**

In `internal/web/server.go`, delete this line (currently line 56):

```go
	r.Use(chiMiddleware.Timeout(5 * time.Minute))
```

Replace the surrounding comment block with:

```go
	// Middleware stack. chiMiddleware.Timeout is deliberately NOT applied
	// at this level — it would kill SSE progress streams, large PDF
	// downloads, and long multipart uploads after 5 minutes. It's applied
	// inside setupRoutes on the short-lived route sub-group instead. The
	// http.Server.WriteTimeout below is the other half of that story; it
	// stays at 5 min and is disabled per-request via the NoWriteDeadline
	// middleware on the long-running routes.
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.RealIP)
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(middleware.CORS())
	r.Use(middleware.SecurityHeaders())
```

If `chiMiddleware` is no longer referenced after deletion, leave the import — the other middlewares (RequestID, RealIP, Logger, Recoverer) still use it. Verify with `grep` after editing.

- [ ] **Step 2: Update `http.Server.WriteTimeout` comment in `server.go`**

Change:

```go
		WriteTimeout: 5 * time.Minute, // Long timeout for SSE and uploads
```

to:

```go
		// Applied to short-lived routes. Long-running routes (SSE streams,
		// book export, large downloads, multipart uploads) disable this
		// per-request via the NoWriteDeadline middleware — see routes.go.
		WriteTimeout: 5 * time.Minute,
```

- [ ] **Step 3: Apply `chiMiddleware.Timeout` inside the short group in `routes.go`**

In the short group added in Task 2, add the timeout as the first middleware:

```go
			// --- Short-lived endpoints ---
			// Default 5-minute chi Timeout applies here. Suitable for
			// normal CRUD where stuck requests should be killed promptly.
			r.Group(func(r chi.Router) {
				r.Use(chiMiddleware.Timeout(5 * time.Minute))

				// Albums.
				r.Get("/albums", albumsHandler.List)
				// ... etc
```

Add the chi middleware import to `routes.go`:

```go
import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/kozaktomas/photo-sorter/internal/web/handlers"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
	"github.com/kozaktomas/photo-sorter/internal/web/static"
)
```

- [ ] **Step 4: Delete the `noTimeout` helper from Task 2, Step 3**

It's no longer needed — the long group simply does not Use the Timeout middleware, which is the whole point. Remove the helper entirely if it was added.

In the long group, remove `r.Use(noTimeout)` and keep only `r.Use(middleware.NoWriteDeadline)`:

```go
			// --- Long-running / streaming endpoints ---
			r.Group(func(r chi.Router) {
				r.Use(middleware.NoWriteDeadline)

				// SSE progress streams.
				r.Get("/sort/{jobId}/events", sortHandler.Events)
				// ... etc
			})
```

- [ ] **Step 5: Run `go build ./...`**

Run: `go build ./...`
Expected: no errors, no unused imports.

- [ ] **Step 6: Run `make lint`**

Run: `make lint`
Expected: PASS.

- [ ] **Step 7: Run the existing handler tests to make sure nothing regressed**

Run: `go test ./internal/web/...`
Expected: PASS.

- [ ] **Step 8: Write an integration test for the route split**

Add `internal/web/routes_timeout_test.go`:

```go
package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	middleware "github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// TestRouteTimeoutSplit documents the split: a "short" sub-router running
// with chiMiddleware.Timeout cancels its context when the handler exceeds
// the timeout, while a "long" sub-router without the timeout does not.
// This mirrors the split in setupRoutes without requiring a full server.
func TestRouteTimeoutSplit(t *testing.T) {
	t.Parallel()

	shortTimeout := 50 * time.Millisecond
	handlerDuration := 150 * time.Millisecond

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
				case <-req.Context().Done():
					shortCtxErr <- req.Context().Err()
				}
				w.WriteHeader(http.StatusOK)
			})
		})
		r.Group(func(r chi.Router) {
			r.Use(middleware.NoWriteDeadline)
			r.Get("/long", func(w http.ResponseWriter, req *http.Request) {
				select {
				case <-time.After(handlerDuration):
					longCtxErr <- nil
				case <-req.Context().Done():
					longCtxErr <- req.Context().Err()
				}
				w.WriteHeader(http.StatusOK)
			})
		})
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	// Short route: context should be cancelled with DeadlineExceeded.
	resp, err := http.Get(srv.URL + "/api/short")
	if err != nil {
		t.Fatalf("short GET: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	select {
	case err := <-shortCtxErr:
		if err != context.DeadlineExceeded {
			t.Errorf("short handler expected context.DeadlineExceeded, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("short handler did not report within 1s")
	}

	// Long route: context should NOT have been cancelled.
	resp, err = http.Get(srv.URL + "/api/long")
	if err != nil {
		t.Fatalf("long GET: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	select {
	case err := <-longCtxErr:
		if err != nil {
			t.Errorf("long handler expected no context error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("long handler did not report within 1s")
	}
}
```

- [ ] **Step 9: Run the new test**

Run: `go test ./internal/web/ -run TestRouteTimeoutSplit -count=1 -v`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/web/server.go internal/web/routes.go internal/web/routes_timeout_test.go
git commit -m "fix(web): move 5min timeout off long-running streaming routes"
```

---

## Task 4: Add SSE heartbeat to `streamSSEEvents`

**Files:**
- Modify: `internal/web/handlers/sse.go`
- Create: `internal/web/handlers/sse_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/web/handlers/sse_test.go`:

```go
package handlers

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSSEJob is a minimal SSEJob that lets the test control the event channel
// and the job status directly.
type fakeSSEJob struct {
	mu     sync.Mutex
	status JobStatus
	ch     chan JobEvent
}

func newFakeSSEJob() *fakeSSEJob {
	return &fakeSSEJob{
		status: JobStatusRunning,
		ch:     make(chan JobEvent, 8),
	}
}

func (f *fakeSSEJob) AddListener() chan JobEvent       { return f.ch }
func (f *fakeSSEJob) RemoveListener(_ chan JobEvent)    {}
func (f *fakeSSEJob) GetStatus() JobStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}
func (f *fakeSSEJob) setStatus(s JobStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = s
}

// TestStreamSSEEvents_SendsHeartbeat verifies that streamSSEEvents writes a
// comment frame (heartbeat) at the configured interval when no events are
// flowing. This is what keeps reverse-proxy idle timeouts from killing a
// silent SSE connection during long compiling_pass phases.
func TestStreamSSEEvents_SendsHeartbeat(t *testing.T) {
	prev := sseHeartbeatInterval
	sseHeartbeatInterval = 30 * time.Millisecond
	t.Cleanup(func() { sseHeartbeatInterval = prev })

	job := newFakeSSEJob()

	req := httptest.NewRequest("GET", "/events?jobId=job-123", nil)
	// chi URL param: set via chi.RouteCtxKey. Easier: bypass jobID lookup
	// by making lookupJob ignore it.
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	rec := newFlushRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		streamSSEEvents(rec, req,
			func(_ string) SSEJob { return job },
			func(_ SSEJob) any { return map[string]string{"hello": "world"} },
		)
	}()

	// Wait long enough for at least 2 heartbeats.
	time.Sleep(90 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("streamSSEEvents did not return after ctx cancel")
	}

	body := rec.buf.String()
	if !strings.Contains(body, ": keepalive\n\n") {
		t.Errorf("expected heartbeat comment in body, got %q", body)
	}
	if strings.Count(body, ": keepalive\n\n") < 2 {
		t.Errorf("expected at least 2 heartbeats, got %d: %q",
			strings.Count(body, ": keepalive\n\n"), body)
	}
}
```

- [ ] **Step 2: Add a helper flushing recorder at the bottom of `sse_test.go`**

```go
// flushRecorder is an http.ResponseWriter + http.Flusher that records bytes
// written. httptest.ResponseRecorder is not a Flusher by default, so
// streamSSEEvents would fail its ok := w.(http.Flusher) check.
type flushRecorder struct {
	header http.Header
	buf    bytes.Buffer
	status int
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{header: make(http.Header)}
}

func (r *flushRecorder) Header() http.Header { return r.header }
func (r *flushRecorder) Write(p []byte) (int, error) {
	return r.buf.Write(p)
}
func (r *flushRecorder) WriteHeader(status int) { r.status = status }
func (r *flushRecorder) Flush()                 {}
```

Add imports:

```go
import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)
```

- [ ] **Step 3: Run the test and confirm it fails**

Run: `go test ./internal/web/handlers/ -run TestStreamSSEEvents_SendsHeartbeat -count=1 -v`
Expected: FAIL — `sseHeartbeatInterval` undefined, or no heartbeat in body.

- [ ] **Step 4: Modify `streamSSEEvents` to emit heartbeats**

Replace the body of `streamSSEEvents` in `internal/web/handlers/sse.go` with:

```go
// sseHeartbeatInterval is the period at which streamSSEEvents emits an SSE
// comment frame ("keepalive") to keep intermediate reverse proxies (nginx,
// traefik, cloudflare tunnel) from closing an idle connection during long
// silent phases — e.g., the lualatex compile passes of a book export, which
// can run for several minutes without emitting any progress event.
//
// Exposed as a var so tests can shrink it.
var sseHeartbeatInterval = 15 * time.Second

// streamSSEEvents sets up SSE headers and streams events from an SSEJob until
// the job completes, the client disconnects, or the event channel closes.
// It emits an SSE comment heartbeat every sseHeartbeatInterval when no real
// events are flowing. The lookupJob function retrieves the job by ID from
// the URL parameter "jobId".
func streamSSEEvents(
	w http.ResponseWriter, r *http.Request,
	lookupJob func(string) SSEJob, getInitialData func(SSEJob) any,
) {
	job, flusher, ok := setupSSEConnection(w, r, lookupJob)
	if !ok {
		return
	}

	eventCh := job.AddListener()
	defer job.RemoveListener(eventCh)

	sendSSEEvent(w, flusher, "status", getInitialData(job))

	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			sendSSEEvent(w, flusher, event.Type, event)
			if isJobTerminal(job.GetStatus()) {
				return
			}
		}
	}
}
```

Add `io` and `time` to the imports at the top of `sse.go`:

```go
import (
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)
```

- [ ] **Step 5: Run the test and confirm it passes**

Run: `go test ./internal/web/handlers/ -run TestStreamSSEEvents_SendsHeartbeat -count=1 -v`
Expected: PASS.

- [ ] **Step 6: Run the full handler test suite**

Run: `go test ./internal/web/handlers/ -count=1`
Expected: PASS (no existing SSE-consuming tests should break — the new heartbeat is an SSE comment line that listeners either ignore or never see, since `EventSource` doesn't dispatch comments to JS).

- [ ] **Step 7: Commit**

```bash
git add internal/web/handlers/sse.go internal/web/handlers/sse_test.go
git commit -m "feat(web): emit 15s SSE heartbeat during long silent phases"
```

---

## Task 5: Document the change in `docs/API.md`

**Files:**
- Modify: `docs/API.md`

- [ ] **Step 1: Locate the SSE section of `docs/API.md`**

Run: `grep -n 'SSE\|Server-Sent Events\|events' /home/pi/projects/photo-sorter/docs/API.md | head -30`

- [ ] **Step 2: Add (or extend) a note under the SSE section**

Insert near the top of the SSE section:

```markdown
**Keepalive:** All `*/events` endpoints emit an SSE comment frame
(`: keepalive\n\n`) every 15 seconds. Browser `EventSource` clients silently
drop comments, so this is invisible at the application level — it exists
purely to keep idle TCP connections alive through reverse proxies (nginx,
traefik, cloudflare) during silent phases such as the `compiling_pass1`/
`compiling_pass2` phases of a book export.

**No 5-minute ceiling:** SSE streams, `GET /api/v1/book-export/{jobId}/download`,
`GET /api/v1/books/{id}/export-pdf`, `GET /api/v1/pages/{id}/export-pdf`,
`POST /api/v1/upload`, and `POST /api/v1/upload/job` are not subject to the
5-minute chi request timeout or the 5-minute `http.Server.WriteTimeout` that
apply to other endpoints — they run as long as the client stays connected.
Client-disconnect cancellation still works via request-context propagation.
```

- [ ] **Step 3: Commit**

```bash
git add docs/API.md
git commit -m "docs(api): note SSE keepalive and long-route timeout exemption"
```

---

## Task 6: Manual smoke test (no automation, report to user)

**Files:** none

- [ ] **Step 1: Rebuild and restart the dev server**

Run: `./dev.sh`
Expected: server listening on 8085.

- [ ] **Step 2: Trigger a full-book PDF export via the UI**

Open a browser to `http://localhost:8085`, log in, open a non-trivial book, click "Export PDF". Watch the progress modal.

Expected: progress phases advance (`fetching_metadata` → `downloading_photos` → `compiling_pass1` → `compiling_pass2`), the UI remains responsive, and the download dialog opens when the PDF is ready.

- [ ] **Step 3: Watch server logs for context cancellations on the long routes**

Run in a second terminal: `tail -f /app/photo-sorter.log | grep -Ei 'context|timeout|deadline'`
Expected: no `context deadline exceeded` entries attributable to the export flow.

- [ ] **Step 4: Confirm the short routes still time out**

As a sanity check that the short group still has the 5-minute ceiling (hard to reproduce without artificially slow handlers), review the integration test from Task 3, Step 8. It's the canonical regression guard.

- [ ] **Step 5: Report back**

Summarize the manual test result and let the user decide whether to push.

---

## Self-Review Notes

- **Spec coverage:** the three root-cause items (chi Timeout on long routes, `WriteTimeout` on long routes, silent SSE during compile passes) map to Tasks 2+3, Task 1, and Task 4 respectively. Tasks 5 and 6 are docs + verification.
- **Placeholder scan:** no TBDs. Every step contains the exact code or command to run.
- **Type consistency:** `NoWriteDeadline` is the same name throughout; `sseHeartbeatInterval` is the same name in sse.go and sse_test.go; route paths in Task 2 match those currently in `routes.go`.
- **Known risk:** Task 2 originally tried a `noTimeout` helper before realising that child chi groups can't strip parent `Use`-ed middleware; Task 3 fixes this by moving the Timeout *down* to the short group. The plan walks the reader through that reasoning on purpose so a fresh engineer doesn't re-discover the dead end.
