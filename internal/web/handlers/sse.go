package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// streamSSEEvents sets up SSE headers and streams events from an SSEJob until the job
// completes, the client disconnects, or the event channel closes.
// The lookupJob function retrieves the job by ID from the URL parameter "jobId".
func streamSSEEvents(w http.ResponseWriter, r *http.Request, lookupJob func(string) SSEJob, getInitialData func(SSEJob) interface{}) {
	jobID := chi.URLParam(r, "jobId")
	if jobID == "" {
		respondError(w, http.StatusBadRequest, "missing job ID")
		return
	}

	job := lookupJob(jobID)
	if job == nil {
		respondError(w, http.StatusNotFound, "job not found")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Subscribe to job events
	eventCh := job.AddListener()
	defer job.RemoveListener(eventCh)

	// Send initial status
	sendSSEEvent(w, flusher, "status", getInitialData(job))

	// Stream events
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			sendSSEEvent(w, flusher, event.Type, event)

			// Close connection if job is done
			status := job.GetStatus()
			if status == JobStatusCompleted || status == JobStatusFailed || status == JobStatusCancelled {
				return
			}
		}
	}
}
