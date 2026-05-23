package handlers

import (
	"sync/atomic"

	"github.com/kozaktomas/photo-sorter/internal/metrics"
)

// Background job kinds emitted as the {kind=...} label on the
// photo_sorter_jobs_*_total counters. Kept here (rather than in the
// metrics package) because the job managers themselves live in handlers/.
const (
	JobKindUpload     = "upload"
	JobKindSort       = "sort"
	JobKindProcess    = "process"
	JobKindBookExport = "book_export"
)

// metricsRegistry is the process-wide registry the handlers report job
// lifecycle events to. atomic.Pointer keeps the read path on the hot job-
// completion code lock-free and allows tests to swap registries in and out.
var metricsRegistry atomic.Pointer[metrics.Registry]

// SetMetricsRegistry installs the registry that job managers report into.
// Called once from cmd/serve.go right before web.NewServer. Passing nil
// disables metric reporting (used by tests and the legacy CLI commands
// that share the handlers package but never start the HTTP server).
func SetMetricsRegistry(r *metrics.Registry) {
	metricsRegistry.Store(r)
}

// recordJobStarted increments photo_sorter_jobs_started_total{kind=...}.
// A nil registry is the documented "metrics disabled" case and is a no-op.
func recordJobStarted(kind string) {
	if r := metricsRegistry.Load(); r != nil {
		r.IncJobStarted(kind)
	}
}

// recordJobCompleted increments photo_sorter_jobs_completed_total.
func recordJobCompleted(kind string) {
	if r := metricsRegistry.Load(); r != nil {
		r.IncJobCompleted(kind)
	}
}

// recordJobFailed increments photo_sorter_jobs_failed_total.
func recordJobFailed(kind string) {
	if r := metricsRegistry.Load(); r != nil {
		r.IncJobFailed(kind)
	}
}

// recordJobCancelled increments photo_sorter_jobs_cancelled_total.
func recordJobCancelled(kind string) {
	if r := metricsRegistry.Load(); r != nil {
		r.IncJobCancelled(kind)
	}
}
