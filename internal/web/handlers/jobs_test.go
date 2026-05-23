package handlers

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestEventBroadcaster_CancelBeforeInit verifies that calling Cancel() on a
// freshly-constructed broadcaster (where InitContext has not yet been
// invoked) is a safe no-op for the underlying ctx and still emits the
// "cancelled" event to any subscribed listener. This protects against a
// regression where Cancel raced with the worker goroutine assigning
// b.cancel and either panicked on a nil call or silently dropped the
// cancellation.
func TestEventBroadcaster_CancelBeforeInit(t *testing.T) {
	t.Parallel()

	var b EventBroadcaster
	ch := b.AddListener()
	t.Cleanup(func() { b.RemoveListener(ch) })

	b.Cancel()

	select {
	case ev := <-ch:
		if ev.Type != "cancelled" {
			t.Fatalf("event type = %q, want cancelled", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancelled event")
	}
}

// TestEventBroadcaster_CancelBetweenInitAndWorker verifies the
// race-condition fix: when Cancel() arrives in the window after
// InitContext but before the worker goroutine reads job.Context(), the
// worker must observe the context as already cancelled. Previously, the
// worker created a fresh ctx itself inside the goroutine, so a Cancel
// that arrived first found b.cancel still nil and silently dropped.
func TestEventBroadcaster_CancelBetweenInitAndWorker(t *testing.T) {
	t.Parallel()

	var b EventBroadcaster
	ctx := b.InitContext(context.Background())

	// Simulate the HTTP-handler-side cancel arriving before the goroutine
	// has even started.
	b.Cancel()

	if ctx.Err() == nil {
		t.Fatal("expected ctx to be cancelled, got nil error")
	}

	// A second cancel must not panic and the context stays cancelled.
	b.Cancel()
	if ctx.Err() == nil {
		t.Fatal("second Cancel must keep context cancelled")
	}
}

// TestEventBroadcaster_ReleaseDoesNotEmit verifies that Release cancels the
// context like Cancel but without emitting an SSE "cancelled" event. This
// is the path workers take when they exit normally (defer job.Release).
func TestEventBroadcaster_ReleaseDoesNotEmit(t *testing.T) {
	t.Parallel()

	var b EventBroadcaster
	ctx := b.InitContext(context.Background())
	ch := b.AddListener()
	t.Cleanup(func() { b.RemoveListener(ch) })

	b.Release()
	if ctx.Err() == nil {
		t.Fatal("expected ctx to be cancelled by Release")
	}

	select {
	case ev := <-ch:
		t.Fatalf("Release should not emit an event, got %q", ev.Type)
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

// TestEventBroadcaster_InitContextIdempotent verifies that calling
// InitContext twice returns the same context, so a second-pass caller
// (e.g. CreateJob followed by an explicit handler-side init) never
// silently swaps the active context out from under a running worker.
func TestEventBroadcaster_InitContextIdempotent(t *testing.T) {
	t.Parallel()

	var b EventBroadcaster
	first := b.InitContext(context.Background())
	second := b.InitContext(context.Background())
	if first != second {
		t.Fatalf("InitContext returned different contexts on re-init")
	}
}

// TestUploadJobManager_StartIfIdle_RejectsConcurrent verifies the
// atomic check-and-set: two concurrent StartIfIdle calls must end with
// exactly one job installed as the active job, and the other receives
// the existing one. This is the fix for the TOCTOU race where two
// concurrent StartJob requests could both pass the GetActiveJob check.
func TestUploadJobManager_StartIfIdle_RejectsConcurrent(t *testing.T) {
	t.Parallel()

	const n = 32
	mgr := NewUploadJobManager()

	var (
		wg           sync.WaitGroup
		successes    atomic.Int32
		rejections   atomic.Int32
		winningJob   atomic.Pointer[UploadJob]
		startBarrier sync.WaitGroup
	)
	startBarrier.Add(1)

	for i := range n {
		wg.Add(1)
		job := &UploadJob{
			ID:        idFor(i),
			Status:    JobStatusPending,
			StartedAt: time.Now(),
		}
		go func(j *UploadJob) {
			defer wg.Done()
			startBarrier.Wait()
			ok, existing := mgr.StartIfIdle(j)
			if ok {
				successes.Add(1)
				winningJob.Store(j)
			} else {
				rejections.Add(1)
				if existing == nil {
					t.Errorf("rejected start: expected existing job")
				}
			}
		}(job)
	}

	startBarrier.Done()
	wg.Wait()

	if successes.Load() != 1 {
		t.Fatalf("expected exactly 1 successful StartIfIdle, got %d", successes.Load())
	}
	if rejections.Load() != n-1 {
		t.Fatalf("expected %d rejections, got %d", n-1, rejections.Load())
	}
	got := mgr.GetActiveJob()
	if got == nil || got != winningJob.Load() {
		t.Fatalf("active job mismatch: got %v, want %v", got, winningJob.Load())
	}
	// Verify the winning job's context was initialised by StartIfIdle.
	if got.Context() == context.Background() {
		t.Fatal("StartIfIdle should have initialised the job context")
	}
}

// TestProcessJobManager_StartIfIdle_RejectsConcurrent mirrors the upload
// manager test for the process-job manager so the shared "only one of
// {embeddings, build-thumbs} at a time" invariant has direct coverage.
func TestProcessJobManager_StartIfIdle_RejectsConcurrent(t *testing.T) {
	t.Parallel()

	const n = 32
	mgr := NewProcessJobManager()

	var (
		wg           sync.WaitGroup
		successes    atomic.Int32
		rejections   atomic.Int32
		startBarrier sync.WaitGroup
	)
	startBarrier.Add(1)

	for i := range n {
		wg.Add(1)
		job := &ProcessJob{
			ID:        idFor(i),
			Status:    JobStatusPending,
			StartedAt: time.Now(),
		}
		go func(j *ProcessJob) {
			defer wg.Done()
			startBarrier.Wait()
			ok, existing := mgr.StartIfIdle(j)
			if ok {
				successes.Add(1)
			} else {
				rejections.Add(1)
				if existing == nil {
					t.Errorf("rejected start: expected existing job")
				}
			}
		}(job)
	}

	startBarrier.Done()
	wg.Wait()

	if successes.Load() != 1 {
		t.Fatalf("expected exactly 1 successful StartIfIdle, got %d", successes.Load())
	}
	if rejections.Load() != n-1 {
		t.Fatalf("expected %d rejections, got %d", n-1, rejections.Load())
	}
}

// idFor returns a deterministic ID for a goroutine index — keeps the
// concurrent-start tests deterministic so a failure diff is readable.
func idFor(i int) string {
	const hex = "0123456789abcdef"
	return "job-" + string(hex[i&0xf])
}
