// Package metrics exposes Prometheus metrics for the photo-sorter HTTP
// server, background job managers, and supporting infrastructure (DB pool,
// embedding service, backup freshness).
//
// All metrics live in a single isolated registry rather than the global
// prometheus.DefaultRegisterer so tests can construct independent metric
// surfaces. The Default() helper returns the process-wide registry used by
// cmd/serve.go.
package metrics

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metric name prefix shared by every series this binary exposes.
const namespace = "photo_sorter"

// Registry bundles every photo-sorter metric so handlers + background jobs
// can take a single dependency rather than reaching into module globals.
// Tests can construct fresh registries to assert on counters without
// process-wide leakage.
type Registry struct {
	reg *prometheus.Registry

	// HTTP middleware.
	httpRequests        *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec
	httpInflight        prometheus.Gauge

	// Background job lifecycle counters.
	jobsStarted   *prometheus.CounterVec
	jobsCompleted *prometheus.CounterVec
	jobsFailed    *prometheus.CounterVec
	jobsCancelled *prometheus.CounterVec

	// Embedding service health (1 = reachable, 0 = unreachable, absent =
	// EMBEDDING_URL not configured).
	embeddingUp prometheus.Gauge

	// Last successful backup as a Unix timestamp. Populated by the backup
	// freshness collector.
	lastBackupTimestamp prometheus.Gauge
}

// New constructs an empty Registry. Tests use this directly to get an
// isolated surface; production code calls Default() instead.
func New() *Registry {
	r := &Registry{reg: prometheus.NewRegistry()}
	r.registerCore()
	return r
}

// registerCore creates and registers every metric series shared by all
// callers (HTTP middleware, job managers, embedding probe). Per-process
// collectors (process resident memory, go runtime stats) are also wired up
// here so /metrics returns the standard "process_" and "go_" families
// without callers needing to know about them.
func (r *Registry) registerCore() {
	r.httpRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total HTTP requests served, partitioned by method, route, and response status.",
		},
		[]string{"method", "route", "status"},
	)
	r.httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration in seconds, partitioned by method and route.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "route"},
	)
	r.httpInflight = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "http",
		Name:      "inflight_requests",
		Help:      "Number of HTTP requests currently being served.",
	})

	r.jobsStarted = newJobCounter("started_total", "Total background jobs started.")
	r.jobsCompleted = newJobCounter("completed_total", "Total background jobs that ran to completion successfully.")
	r.jobsFailed = newJobCounter("failed_total", "Total background jobs that ended in an error state.")
	r.jobsCancelled = newJobCounter("cancelled_total", "Total background jobs that were cancelled before completion.")

	r.embeddingUp = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "embedding_service_up",
		Help:      "Whether the configured embedding service responded to its last health probe (1 = up, 0 = down).",
	})

	r.lastBackupTimestamp = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "last_backup_timestamp_seconds",
		Help: "Unix timestamp of the latest completed backup directory under the configured backup root. " +
			"0 when no backups exist yet or the backup directory has not been wired in.",
	})

	r.reg.MustRegister(
		r.httpRequests,
		r.httpRequestDuration,
		r.httpInflight,
		r.jobsStarted,
		r.jobsCompleted,
		r.jobsFailed,
		r.jobsCancelled,
		r.embeddingUp,
		r.lastBackupTimestamp,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

// newJobCounter is a small factory that builds the four background_jobs
// counter families with consistent labels (kind ∈ {upload, sort, process,
// book_export}).
func newJobCounter(name, help string) *prometheus.CounterVec {
	return prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "jobs",
			Name:      name,
			Help:      help,
		},
		[]string{"kind"},
	)
}

// Handler returns an http.Handler that serves the registered metrics in the
// Prometheus text exposition format. Mount it at /metrics on the same chi
// router that backs the rest of the API.
func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.reg, promhttp.HandlerOpts{
		Registry: r.reg,
	})
}

// Registerer exposes the underlying registry so callers can install custom
// collectors (e.g. the DB pool gauges that pull sql.DBStats on scrape).
func (r *Registry) Registerer() prometheus.Registerer { return r.reg }

// Gatherer exposes the registry for tests that want to introspect a metric
// without rendering the full /metrics text output.
func (r *Registry) Gatherer() prometheus.Gatherer { return r.reg }

// RegisterDBPool installs a collector that pulls sql.DB.Stats() on every
// scrape and exports them as gauges. Passing a nil pool is a no-op so
// startup paths that delay DB initialisation can defer wiring.
func (r *Registry) RegisterDBPool(db *sql.DB) {
	if db == nil {
		return
	}
	r.reg.MustRegister(newDBPoolCollector(db))
}

// SetEmbeddingUp records the current embedding-service health. Pass true
// when a probe succeeded, false when it failed. The metric is intentionally
// always registered so the absence/presence distinction in PromQL is
// driven by Registry construction, not by runtime probe wiring.
func (r *Registry) SetEmbeddingUp(up bool) {
	if up {
		r.embeddingUp.Set(1)
	} else {
		r.embeddingUp.Set(0)
	}
}

// SetLastBackupTimestamp records the Unix-second timestamp of the most
// recent completed backup directory. A zero ts publishes 0 (no backup yet).
func (r *Registry) SetLastBackupTimestamp(ts time.Time) {
	if ts.IsZero() {
		r.lastBackupTimestamp.Set(0)
		return
	}
	r.lastBackupTimestamp.Set(float64(ts.Unix()))
}

// IncJobStarted records a job-lifecycle "started" event. kind is one of
// "upload", "sort", "process", "book_export". Unknown kinds are still
// recorded so we never silently lose a counter increment.
func (r *Registry) IncJobStarted(kind string) { r.jobsStarted.WithLabelValues(kind).Inc() }

// IncJobCompleted records a job-lifecycle "completed" event.
func (r *Registry) IncJobCompleted(kind string) { r.jobsCompleted.WithLabelValues(kind).Inc() }

// IncJobFailed records a job-lifecycle "failed" event.
func (r *Registry) IncJobFailed(kind string) { r.jobsFailed.WithLabelValues(kind).Inc() }

// IncJobCancelled records a job-lifecycle "cancelled" event (user pressed
// cancel or the context was cut before the job ran to completion).
func (r *Registry) IncJobCancelled(kind string) { r.jobsCancelled.WithLabelValues(kind).Inc() }

// Middleware wraps an http.Handler with the photo_sorter_http_* metrics.
// It increments requests_total, samples request_duration_seconds, and
// tracks inflight_requests. The /metrics endpoint itself is skipped so the
// scrape does not pollute its own counters.
//
// The route label is the chi route pattern when available
// (e.g. "/api/v1/photos/{uid}"), falling back to the literal request path
// when no pattern is registered. This keeps the label set bounded —
// raw URL paths would explode cardinality.
func (r *Registry) Middleware(routeOf func(*http.Request) string) func(http.Handler) http.Handler {
	if routeOf == nil {
		routeOf = func(req *http.Request) string { return req.URL.Path }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/metrics" {
				next.ServeHTTP(w, req)
				return
			}
			r.httpInflight.Inc()
			defer r.httpInflight.Dec()

			start := time.Now()
			rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, req)

			route := routeOf(req)
			r.httpRequests.
				WithLabelValues(req.Method, route, strconv.Itoa(rw.status)).
				Inc()
			r.httpRequestDuration.
				WithLabelValues(req.Method, route).
				Observe(time.Since(start).Seconds())
		})
	}
}

// statusRecorder is a minimal ResponseWriter wrapper that captures the
// status code so the middleware can attribute it to the right counter
// without requiring callers to use chi's middleware.WrapResponseWriter.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

// WriteHeader captures the status code on the first call. Subsequent
// WriteHeader calls are forwarded to the wrapped writer but not re-
// recorded — net/http itself logs a warning when handlers double-write.
func (s *statusRecorder) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}

// Write records an implicit 200 OK status when the handler writes the
// response body without calling WriteHeader (net/http's default behavior).
func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wroteHeader {
		s.status = http.StatusOK
		s.wroteHeader = true
	}
	n, err := s.ResponseWriter.Write(b)
	if err != nil {
		return n, fmt.Errorf("status recorder write: %w", err)
	}
	return n, nil
}

var (
	defaultOnce     sync.Once
	defaultRegistry *Registry
)

// Default returns the process-wide registry. The first caller constructs
// it; subsequent callers receive the same instance. cmd/serve.go is the
// expected first caller.
func Default() *Registry {
	defaultOnce.Do(func() {
		defaultRegistry = New()
	})
	return defaultRegistry
}
