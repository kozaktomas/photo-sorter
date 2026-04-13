package handlers

import (
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// sseHeartbeatInterval is the period at which streamSSEEvents emits an SSE
// comment frame ("keepalive") to keep intermediate reverse proxies (nginx,
// traefik, cloudflare tunnel) from closing an idle connection during long
// silent phases — e.g., the lualatex compile passes of a book export, which
// can run for several minutes without emitting any progress event.
//
// Exposed as a var so tests can shrink it.
var sseHeartbeatInterval = 15 * time.Second

// isJobTerminal returns true if the job status is a terminal state.
func isJobTerminal(status JobStatus) bool {
	return status == JobStatusCompleted || status == JobStatusFailed || status == JobStatusCancelled
}

// setupSSEConnection validates the request, finds the job, and sets up SSE headers.
// Returns the job, flusher, and true on success. On failure, writes an error.
// response and returns zero values with false.
func setupSSEConnection(
	w http.ResponseWriter, r *http.Request, lookupJob func(string) SSEJob,
) (SSEJob, http.Flusher, bool) {
	jobID := chi.URLParam(r, "jobId")
	if jobID == "" {
		respondError(w, http.StatusBadRequest, "missing job ID")
		return nil, nil, false
	}

	job := lookupJob(jobID)
	if job == nil {
		respondError(w, http.StatusNotFound, "job not found")
		return nil, nil, false
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, http.StatusInternalServerError, "streaming not supported")
		return nil, nil, false
	}

	return job, flusher, true
}

// streamSSEEvents sets up SSE headers and streams events from an SSEJob until the job.
// completes, the client disconnects, or the event channel closes.
// The lookupJob function retrieves the job by ID from the URL parameter "jobId".
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
