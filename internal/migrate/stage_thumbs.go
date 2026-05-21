package migrate

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/thumb"
)

// stageThumbs regenerates the cached thumbnails for every photo migrated
// in this run. PhotoPrism's own cache layout differs from photo-sorter's
// (file hash sharded into aa/bb/cc/<hash>_<size>.jpg), so we always
// regenerate from the freshly-written originals.
//
// The work is embarrassingly parallel: each photo decodes once and the
// thumbnail registry is iterated locally. The bottleneck is the JPEG
// decode + encode loop, so the default worker count of 4 matches the
// number of CPU cores on the Pi.
func (m *migrator) stageThumbs(ctx context.Context) error {
	if m.opts.DryRun {
		summary := StageSummary{Stage: StageThumbs, Read: len(m.photoMap), Skipped: len(m.photoMap)}
		m.report.AppendStage(summary)
		return nil
	}

	uids := make([]string, 0, len(m.photoMap))
	for _, native := range m.photoMap {
		uids = append(uids, native)
	}
	summary := StageSummary{Stage: StageThumbs, Read: len(uids)}
	if len(uids) == 0 {
		m.report.AppendStage(summary)
		return nil
	}

	bar := newStageBar(len(uids), "thumbs")
	defer finishBar(bar)
	m.runThumbWorkers(ctx, uids, bar, &summary)
	m.report.AppendStage(summary)
	return nil
}

// runThumbWorkers spawns Concurrency goroutines that pull native photo
// UIDs from a buffered channel and decode/encode their thumbnails. The
// summary's Created/Failed counters are populated when all workers
// finish.
func (m *migrator) runThumbWorkers(
	ctx context.Context, uids []string, bar progressBar, summary *StageSummary,
) {
	jobs := make(chan string, len(uids))
	for _, u := range uids {
		jobs <- u
	}
	close(jobs)

	var created, failed atomic.Int64
	var wg sync.WaitGroup
	for range m.opts.Concurrency {
		wg.Add(1)
		go m.thumbWorker(ctx, jobs, bar, &created, &failed, &wg)
	}
	wg.Wait()
	summary.Created = int(created.Load())
	summary.Failed = int(failed.Load())
}

// thumbWorker is the body of one worker goroutine. Keeping it separate
// from runThumbWorkers keeps both functions inside the complexity budget
// and makes the cancellation flow easier to read.
func (m *migrator) thumbWorker(
	ctx context.Context, jobs <-chan string, bar progressBar,
	created, failed *atomic.Int64, wg *sync.WaitGroup,
) {
	defer wg.Done()
	for uid := range jobs {
		if ctx.Err() != nil {
			return
		}
		if err := m.generateOneThumb(ctx, uid); err != nil {
			fmt.Fprintf(m.out, "\nthumb %s: %v\n", uid, err)
			failed.Add(1)
		} else {
			created.Add(1)
		}
		_ = bar.Add(1)
	}
}

// progressBar is the subset of *progressbar.ProgressBar the worker
// touches. Pulling it behind an interface keeps the test scaffolding
// from needing the third-party progress bar in unit tests.
type progressBar interface {
	Add(int) error
}

// generateOneThumb produces every registered thumbnail size for the
// given native photo. The function fetches the photo's stored
// orientation + file_hash so the thumbnail names line up with what the
// web layer expects.
func (m *migrator) generateOneThumb(ctx context.Context, photoUID string) error {
	photo, err := m.opts.Photos.GetPhoto(ctx, photoUID)
	if err != nil {
		return fmt.Errorf("load photo: %w", err)
	}
	if photo == nil {
		return database.ErrNotFound
	}
	abs, err := m.opts.Store.AbsOriginal(photo.FilePath)
	if err != nil {
		return fmt.Errorf("resolve original: %w", err)
	}
	if _, err := thumb.GenerateAll(thumb.Source{
		Path:        abs,
		Orientation: photo.FileOrientation,
	}, m.opts.Store, photo.FileHash); err != nil {
		return fmt.Errorf("generate: %w", err)
	}
	return nil
}
