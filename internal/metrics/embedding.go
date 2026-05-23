package metrics

import (
	"context"
	"log"
	"net/http"
	"time"
)

// EmbeddingProbeInterval is the default cadence for the background
// embedding-service health probe. Exported so the serve command can pass a
// shorter interval in tests.
const EmbeddingProbeInterval = 30 * time.Second

// StartEmbeddingProbe runs a periodic best-effort GET against the
// embedding service and updates photo_sorter_embedding_service_up. Passing
// an empty url disables the probe entirely — the metric stays at its
// initial zero value, which is the documented signal for "not configured".
//
// The probe stops when ctx is cancelled. Failures are logged at most once
// per state transition (up→down or down→up) so a long outage does not spam
// the journal.
func (r *Registry) StartEmbeddingProbe(ctx context.Context, url string, interval time.Duration) {
	if url == "" {
		return
	}
	if interval <= 0 {
		interval = EmbeddingProbeInterval
	}
	client := &http.Client{Timeout: 5 * time.Second}
	go r.runEmbeddingProbe(ctx, client, url, interval)
}

// runEmbeddingProbe is the goroutine body. Kicks off with one immediate
// probe so /metrics reflects reality before the first ticker fires.
func (r *Registry) runEmbeddingProbe(
	ctx context.Context, client *http.Client, url string, interval time.Duration,
) {
	lastUp := r.probeEmbeddingOnce(ctx, client, url, true)

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			lastUp = r.probeEmbeddingOnce(ctx, client, url, lastUp)
		}
	}
}

// probeEmbeddingOnce issues a single GET / against the embedding URL and
// returns whether the service was reachable. The previousUp argument is
// used to suppress log spam when the state has not changed.
func (r *Registry) probeEmbeddingOnce(
	ctx context.Context, client *http.Client, url string, previousUp bool,
) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		r.SetEmbeddingUp(false)
		if previousUp {
			log.Printf("metrics: embedding probe build failed: %v", err)
		}
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		r.SetEmbeddingUp(false)
		if previousUp {
			log.Printf("metrics: embedding probe failed (%s): %v", url, err)
		}
		return false
	}
	resp.Body.Close()
	up := resp.StatusCode < 500
	r.SetEmbeddingUp(up)
	if up && !previousUp {
		log.Printf("metrics: embedding probe recovered (%s)", url)
	}
	if !up && previousUp {
		log.Printf("metrics: embedding probe returned HTTP %d", resp.StatusCode)
	}
	return up
}
