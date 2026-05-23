package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// fakeSSEJob is a minimal SSEJob that lets the test control the event
// channel and the job status directly.
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

func (f *fakeSSEJob) AddListener() chan JobEvent     { return f.ch }
func (f *fakeSSEJob) RemoveListener(_ chan JobEvent) {}
func (f *fakeSSEJob) GetStatus() JobStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

// flushRecorder is an http.ResponseWriter + http.Flusher that records bytes
// written. httptest.ResponseRecorder is not a Flusher by default, so
// streamSSEEvents would fail its `ok := w.(http.Flusher)` check.
type flushRecorder struct {
	header http.Header
	mu     sync.Mutex
	buf    bytes.Buffer
	status int
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{header: make(http.Header)}
}

func (r *flushRecorder) Header() http.Header { return r.header }
func (r *flushRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}
func (r *flushRecorder) WriteHeader(status int) { r.status = status }
func (r *flushRecorder) Flush()                 {}
func (r *flushRecorder) Body() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

// requestWithJobID returns a request with chi's URL param "jobId" set to
// the given value, so setupSSEConnection's chi.URLParam lookup succeeds.
func requestWithJobID(jobID string) *http.Request {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/events", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobId", jobID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// TestStreamSSEEvents_TerminalJobReturnsImmediately verifies that a client
// subscribing to a job that has ALREADY finished (status = completed /
// failed / cancelled) receives the final status event and the handler
// returns instead of idling on heartbeats forever. Without this guard,
// a reconnect after the worker emitted "completed" left the SSE handler
// blocked on eventCh — the worker had exited and no further events would
// arrive, so the client hung until its request timeout.
func TestStreamSSEEvents_TerminalJobReturnsImmediately(t *testing.T) {
	prev := sseHeartbeatInterval
	sseHeartbeatInterval = 30 * time.Millisecond
	t.Cleanup(func() { sseHeartbeatInterval = prev })

	job := newFakeSSEJob()
	job.mu.Lock()
	job.status = JobStatusCompleted
	job.mu.Unlock()

	req := requestWithJobID("job-final")
	rec := newFlushRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		streamSSEEvents(rec, req,
			func(_ string) SSEJob { return job },
			func(_ SSEJob) any { return map[string]string{"hello": "world"} },
		)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("streamSSEEvents did not return for a terminal job")
	}

	body := rec.Body()
	if !strings.Contains(body, "event: status") {
		t.Fatalf("expected initial status event, got %q", body)
	}
	// No heartbeat should have been written — the handler returned before
	// the ticker would have fired even once.
	if strings.Contains(body, ": keepalive") {
		t.Fatalf("did not expect a heartbeat for a terminal job, got %q", body)
	}
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

	baseReq := requestWithJobID("job-123")
	ctx, cancel := context.WithCancel(baseReq.Context())
	req := baseReq.WithContext(ctx)
	rec := newFlushRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		streamSSEEvents(rec, req,
			func(_ string) SSEJob { return job },
			func(_ SSEJob) any { return map[string]string{"hello": "world"} },
		)
	}()

	// Wait long enough for at least 2 heartbeats (interval = 30ms).
	time.Sleep(120 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("streamSSEEvents did not return after ctx cancel")
	}

	body := rec.Body()
	if !strings.Contains(body, ": keepalive\n\n") {
		t.Fatalf("expected heartbeat comment in body, got %q", body)
	}
	if got := strings.Count(body, ": keepalive\n\n"); got < 2 {
		t.Errorf("expected at least 2 heartbeats, got %d: %q", got, body)
	}
}
