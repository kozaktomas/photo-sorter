package metrics

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestMiddlewareCountsRequests checks that a single handler invocation
// bumps photo_sorter_http_requests_total exactly once with the correct
// label values, and that the duration histogram observes one sample.
func TestMiddlewareCountsRequests(t *testing.T) {
	r := New()
	h := r.Middleware(func(req *http.Request) string { return "/test" })(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		}),
	)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusTeapot)
	}

	got := testutil.ToFloat64(r.httpRequests.WithLabelValues("GET", "/test", "418"))
	if got != 1 {
		t.Fatalf("requests counter: got %v, want 1", got)
	}

	hist := testutil.CollectAndCount(r.httpRequestDuration, "photo_sorter_http_request_duration_seconds")
	if hist == 0 {
		t.Fatalf("duration histogram: got 0 series, want at least 1")
	}
}

// TestMiddlewareSkipsMetricsEndpoint ensures the middleware does not count
// scrapes against /metrics, which would make the counter self-incrementing.
func TestMiddlewareSkipsMetricsEndpoint(t *testing.T) {
	r := New()
	h := r.Middleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	got := testutil.CollectAndCount(r.httpRequests, "photo_sorter_http_requests_total")
	if got != 0 {
		t.Fatalf("/metrics should not be counted; got %d series, want 0", got)
	}
}

// TestMiddlewareInflightGauge confirms inflight bumps up while a handler is
// running and goes back to zero afterwards.
func TestMiddlewareInflightGauge(t *testing.T) {
	r := New()
	released := make(chan struct{})
	started := make(chan struct{})
	h := r.Middleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-released
		w.WriteHeader(http.StatusOK)
	}))

	go func() {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/test", http.NoBody)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}()

	<-started
	if got := testutil.ToFloat64(r.httpInflight); got != 1 {
		t.Fatalf("inflight while running: got %v, want 1", got)
	}
	close(released)

	// Allow the goroutine to drain.
	for range 50 {
		if testutil.ToFloat64(r.httpInflight) == 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("inflight after release: got %v, want 0", testutil.ToFloat64(r.httpInflight))
}

// TestJobCounters confirms each kind/state combo lands on a distinct
// series.
func TestJobCounters(t *testing.T) {
	r := New()
	r.IncJobStarted("upload")
	r.IncJobStarted("upload")
	r.IncJobCompleted("upload")
	r.IncJobFailed("sort")
	r.IncJobCancelled("process")

	cases := []struct {
		vec  string
		kind string
		want float64
	}{
		{"started", "upload", 2},
		{"completed", "upload", 1},
		{"failed", "sort", 1},
		{"cancelled", "process", 1},
	}
	for _, c := range cases {
		var got float64
		switch c.vec {
		case "started":
			got = testutil.ToFloat64(r.jobsStarted.WithLabelValues(c.kind))
		case "completed":
			got = testutil.ToFloat64(r.jobsCompleted.WithLabelValues(c.kind))
		case "failed":
			got = testutil.ToFloat64(r.jobsFailed.WithLabelValues(c.kind))
		case "cancelled":
			got = testutil.ToFloat64(r.jobsCancelled.WithLabelValues(c.kind))
		}
		if got != c.want {
			t.Errorf("%s{kind=%q}: got %v, want %v", c.vec, c.kind, got, c.want)
		}
	}
}

// TestHandlerSerializesMetrics asserts the /metrics text output contains
// the families we promised in CLAUDE.md / the alert rules.
func TestHandlerSerializesMetrics(t *testing.T) {
	r := New()
	r.IncJobStarted("upload")
	r.SetEmbeddingUp(true)
	r.SetLastBackupTimestamp(time.Unix(1700000000, 0))

	srv := httptest.NewServer(r.Handler())
	defer srv.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, http.NoBody)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	wantContains := []string{
		"photo_sorter_jobs_started_total",
		`photo_sorter_jobs_started_total{kind="upload"} 1`,
		"photo_sorter_embedding_service_up 1",
		"photo_sorter_last_backup_timestamp_seconds 1.7e+09",
	}
	for _, want := range wantContains {
		if !strings.Contains(text, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}

// TestBackupWatcherPicksLatest seeds two backup dirs with different
// timestamps and verifies the gauge reports the newer one.
func TestBackupWatcherPicksLatest(t *testing.T) {
	root := t.TempDir()
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	writeBackup(t, root, "photo-sorter-20260101-000000", older)
	writeBackup(t, root, "photo-sorter-20260501-000000", newer)
	// Sanity: a directory with the right prefix but no metadata.json must
	// be silently skipped rather than crashing the scan.
	if err := os.MkdirAll(filepath.Join(root, "photo-sorter-broken"), 0o755); err != nil {
		t.Fatal(err)
	}

	r := New()
	r.scanBackups(root)

	got := testutil.ToFloat64(r.lastBackupTimestamp)
	if got != float64(newer.Unix()) {
		t.Fatalf("last_backup_timestamp: got %v, want %v", got, newer.Unix())
	}
}

// TestBackupWatcherEmptyDir keeps the gauge at zero when no backups exist
// yet — the alert rules treat zero as the "backups not wired up" signal.
func TestBackupWatcherEmptyDir(t *testing.T) {
	root := t.TempDir()

	r := New()
	r.scanBackups(root)

	if got := testutil.ToFloat64(r.lastBackupTimestamp); got != 0 {
		t.Fatalf("empty backup dir: got %v, want 0", got)
	}
}

// writeBackup creates a backup directory + metadata.json with the supplied
// created_at timestamp. Used by the freshness tests.
func writeBackup(t *testing.T, root, name string, createdAt time.Time) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := backupMetadata{CreatedAt: createdAt}
	data, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
