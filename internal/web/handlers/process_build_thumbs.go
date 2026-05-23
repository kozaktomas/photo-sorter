package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/kozaktomas/photo-sorter/internal/audit"
	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/imgconvert"
	"github.com/kozaktomas/photo-sorter/internal/storage"
	"github.com/kozaktomas/photo-sorter/internal/thumb"
)

// buildThumbsPageSize is the page size used when paginating through the
// photos table to enumerate UIDs for the backfill. Stays well below the
// PostgreSQL repository's maxPhotoListLimit so the call always succeeds.
const buildThumbsServicePageSize = 200

// BuildThumbsRequest is the JSON body accepted by POST
// /api/v1/process/build-thumbs. Every field is optional and a zero value
// falls back to the same defaults as the CLI.
type BuildThumbsRequest struct {
	Concurrency int      `json:"concurrency,omitempty"`
	Sizes       []string `json:"sizes,omitempty"`
	OnlyMissing *bool    `json:"only_missing,omitempty"`
	Limit       int      `json:"limit,omitempty"`
	PhotoUID    string   `json:"photo_uid,omitempty"`
}

// BuildThumbs starts a background build-thumbs job. Returns 409 if a job
// is already running on the shared ProcessJobManager (embeddings + build-
// thumbs share the same slot — only one runs at a time), 400 if the
// request body is malformed or references an unknown size, 500 on storage
// / DB initialisation failures, and 202 with `{job_id}` on success.
func (h *ProcessHandler) BuildThumbs(w http.ResponseWriter, r *http.Request) {
	if !database.IsInitialized() {
		respondError(w, http.StatusBadRequest, "DATABASE_URL is not configured")
		return
	}
	if !h.acceptNewProcessJob(w) {
		return
	}

	req, ok := decodeBuildThumbsRequest(w, r)
	if !ok {
		return
	}

	sizes, err := validateThumbSizes(req.Sizes)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	job := newBuildThumbsJob(req, sizes)
	if startedOK, existing := h.jobManager.StartIfIdle(job); !startedOK {
		respondJSON(w, http.StatusConflict, map[string]any{
			"error":  "a process job is already running",
			"job_id": existing.ID,
			"status": existing.Status,
		})
		return
	}

	// runBuildThumbsJob's cancellation context was initialised by
	// StartIfIdle so a DELETE arriving before the goroutine is scheduled
	// still propagates.
	go h.runBuildThumbsJob(job, req.PhotoUID)

	auditMeta := map[string]any{
		"job_id":       job.ID,
		"concurrency":  job.Options.Concurrency,
		"sizes":        job.Options.Sizes,
		"only_missing": job.Options.OnlyMissing,
		"limit":        job.Options.Limit,
	}
	if req.PhotoUID != "" {
		auditMeta["photo_uid"] = req.PhotoUID
	}
	audit.FromContext(r.Context()).Log(
		r.Context(), audit.ActionProcessBuildThumb, audit.EntityProcessJob, job.ID,
		auditMeta,
	)
	respondJSON(w, http.StatusAccepted, map[string]string{
		"job_id": job.ID,
		"status": string(JobStatusPending),
	})
}

// acceptNewProcessJob rejects the request with 409 if a process job is
// already running or pending, returning false in that case. Otherwise it
// returns true and the caller proceeds.
func (h *ProcessHandler) acceptNewProcessJob(w http.ResponseWriter) bool {
	active := h.jobManager.GetActiveJob()
	if active == nil {
		return true
	}
	if active.Status == JobStatusRunning || active.Status == JobStatusPending {
		respondError(w, http.StatusConflict, "a process job is already running")
		return false
	}
	return true
}

// decodeBuildThumbsRequest decodes the JSON body, tolerating an empty
// payload (every field is optional). Writes a 400 and returns ok=false on
// a malformed body.
func decodeBuildThumbsRequest(w http.ResponseWriter, r *http.Request) (BuildThumbsRequest, bool) {
	var req BuildThumbsRequest
	if r.ContentLength == 0 || r.Body == nil {
		return req, true
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return req, false
	}
	return req, true
}

// newBuildThumbsJob assembles a ProcessJob for the build-thumbs pipeline
// using the request defaults (concurrency falls back to
// constants.DefaultConcurrency, onlyMissing defaults to true).
func newBuildThumbsJob(req BuildThumbsRequest, sizes []string) *ProcessJob {
	concurrency := req.Concurrency
	if concurrency <= 0 {
		concurrency = constants.DefaultConcurrency
	}
	onlyMissing := true
	if req.OnlyMissing != nil {
		onlyMissing = *req.OnlyMissing
	}
	return &ProcessJob{
		ID:        uuid.New().String(),
		Kind:      ProcessJobKindBuildThumbs,
		Status:    JobStatusPending,
		StartedAt: time.Now(),
		Options: ProcessJobOptions{
			Concurrency: concurrency,
			Limit:       req.Limit,
			Sizes:       sizes,
			OnlyMissing: onlyMissing,
		},
	}
}

// validateThumbSizes maps an empty input to the full registered set and
// otherwise checks every entry against thumb.IsValidSize so a typo fails
// fast instead of silently no-opping.
func validateThumbSizes(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return thumb.SizeNames(), nil
	}
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if s == "" {
			continue
		}
		if !thumb.IsValidSize(s) {
			return nil, fmt.Errorf("unknown thumbnail size %q", s)
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return thumb.SizeNames(), nil
	}
	return out, nil
}

// runBuildThumbsJob executes the build-thumbs pipeline in the background.
// The job's cancellation context is initialised by the BuildThumbs handler
// before the goroutine is spawned, so DELETE arriving in the scheduling
// gap still propagates here.
func (h *ProcessHandler) runBuildThumbsJob(job *ProcessJob, photoUID string) {
	ctx := job.Context()
	defer job.Release()

	job.mu.Lock()
	job.Status = JobStatusRunning
	job.mu.Unlock()
	recordJobStarted(JobKindProcess)
	job.SendEvent(JobEvent{Type: "started", Message: "Thumbnail backfill started"})

	reader, err := database.GetPhotoReader(ctx)
	if err != nil {
		h.failJob(job, fmt.Sprintf("failed to get photo reader: %v", err))
		return
	}
	store, err := storage.New(h.config.Storage.OriginalsPath, h.config.Storage.CachePath)
	if err != nil {
		h.failJob(job, fmt.Sprintf("storage init failed: %v", err))
		return
	}

	uids, err := listPhotoUIDsForBuild(ctx, reader, photoUID, job.Options.Limit)
	if err != nil {
		if ctx.Err() != nil {
			h.cancelJob(job)
		} else {
			h.failJob(job, fmt.Sprintf("failed to list photos: %v", err))
		}
		return
	}

	job.mu.Lock()
	job.TotalPhotos = len(uids)
	job.mu.Unlock()
	job.SendEvent(JobEvent{Type: "photos_counted", Data: map[string]int{"total": len(uids)}})

	if len(uids) == 0 {
		h.completeBuildThumbsJob(job, &BuildThumbsJobResult{})
		return
	}

	result := h.runBuildThumbsWorkerPool(ctx, store, reader, job, uids)
	if ctx.Err() != nil {
		h.cancelJob(job)
		return
	}
	h.completeBuildThumbsJob(job, result)
}

// listPhotoUIDsForBuild returns the photo UIDs the build-thumbs job
// should process. A non-empty photoUID short-circuits to a single-element
// slice (verifying the row exists); otherwise the photos table is paged
// through in stable order until limit (0 = unlimited) is reached.
func listPhotoUIDsForBuild(
	ctx context.Context, reader database.PhotoReader,
	photoUID string, limit int,
) ([]string, error) {
	if photoUID != "" {
		if _, err := reader.GetPhoto(ctx, photoUID); err != nil {
			return nil, fmt.Errorf("photo %q: %w", photoUID, err)
		}
		return []string{photoUID}, nil
	}
	return paginateAllPhotoUIDs(ctx, reader, limit)
}

// paginateAllPhotoUIDs pages through the photos table in stable order,
// appending UIDs until limit (0 = unlimited) is reached or the table
// runs out of rows. The context error is wrapped so callers can
// distinguish cancellation from a repository error.
func paginateAllPhotoUIDs(
	ctx context.Context, reader database.PhotoReader, limit int,
) ([]string, error) {
	var uids []string
	offset := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("paginate photos: %w", err)
		}
		page, _, err := reader.ListPhotos(ctx, database.PhotoFilter{
			SortBy: "newest",
			Limit:  buildThumbsServicePageSize,
			Offset: offset,
		})
		if err != nil {
			return nil, fmt.Errorf("list photos: %w", err)
		}
		if len(page) == 0 {
			return uids, nil
		}
		uids, done := appendUIDs(uids, page, limit)
		if done {
			return uids, nil
		}
		offset += len(page)
	}
}

// appendUIDs appends every page entry's UID to uids, stopping early when
// limit (>0) is reached. The bool return reports whether the limit was
// hit so the caller can break out of the pagination loop.
func appendUIDs(uids []string, page []database.Photo, limit int) ([]string, bool) {
	for _, p := range page {
		uids = append(uids, p.UID)
		if limit > 0 && len(uids) >= limit {
			return uids, true
		}
	}
	return uids, false
}

// buildThumbsCounters is the worker-pool's atomic state. Done is the
// processed-photo counter that drives progress events; the others are
// summed up in the final summary.
type buildThumbsCounters struct {
	generated atomic.Int64
	skipped   atomic.Int64
	failed    atomic.Int64
	done      atomic.Int64
}

// runBuildThumbsWorkerPool runs the concurrent thumbnail backfill worker
// pool and returns the aggregated summary. Each completed photo bumps the
// progress counter and emits a `progress` event with the current UID.
func (h *ProcessHandler) runBuildThumbsWorkerPool(
	ctx context.Context, store *storage.Storage,
	reader database.PhotoReader, job *ProcessJob, uids []string,
) *BuildThumbsJobResult {
	counters := &buildThumbsCounters{}
	concurrency := job.Options.Concurrency
	if concurrency <= 0 {
		concurrency = constants.DefaultConcurrency
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, uid := range uids {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go h.runBuildThumbsWorker(ctx, store, reader, job, uid, sem, counters, &wg)
	}
	wg.Wait()

	return &BuildThumbsJobResult{
		Generated: counters.generated.Load(),
		Skipped:   counters.skipped.Load(),
		Failed:    counters.failed.Load(),
	}
}

// runBuildThumbsWorker is the per-photo worker spawned from
// runBuildThumbsWorkerPool. It blocks on the semaphore, runs the build,
// updates counters + progress, and exits — context cancellation aborts
// the slot acquisition cleanly.
func (h *ProcessHandler) runBuildThumbsWorker(
	ctx context.Context, store *storage.Storage,
	reader database.PhotoReader, job *ProcessJob,
	photoUID string, sem chan struct{},
	counters *buildThumbsCounters, wg *sync.WaitGroup,
) {
	defer wg.Done()
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-sem }()
	if ctx.Err() != nil {
		return
	}

	outcome := buildOnePhotoThumbs(ctx, reader, store, photoUID, job.Options.Sizes, job.Options.OnlyMissing)
	recordBuildThumbsOutcome(counters, photoUID, outcome)

	count := counters.done.Add(1)
	h.sendBuildThumbsProgress(job, int(count), photoUID)
}

// recordBuildThumbsOutcome translates a single-photo outcome into the
// matching atomic counter increment and logs any error so the operator
// can investigate without leaking the panic into the worker pool.
func recordBuildThumbsOutcome(counters *buildThumbsCounters, photoUID string, outcome thumbServiceOutcome) {
	switch outcome.status {
	case thumbServiceGenerated:
		counters.generated.Add(int64(outcome.wrote))
	case thumbServiceSkipped:
		counters.skipped.Add(1)
	case thumbServiceError:
		counters.failed.Add(1)
		if outcome.err != nil {
			log.Printf("build-thumbs: %s: %v", photoUID, outcome.err)
		}
	}
}

// thumbServiceStatus is the per-photo outcome of buildOnePhotoThumbs.
type thumbServiceStatus int

const (
	thumbServiceGenerated thumbServiceStatus = iota
	thumbServiceSkipped
	thumbServiceError
)

// thumbServiceOutcome captures the per-photo status, the number of thumbs
// actually written, and any error to log. Errors are not fatal — they
// only bump the failed counter.
type thumbServiceOutcome struct {
	status thumbServiceStatus
	wrote  int
	err    error
}

// buildOnePhotoThumbs resolves the photo's original, decodes it via
// imgconvert.EnsureDecodable, and writes the requested thumbnail subset
// via thumb.GenerateSizes. When onlyMissing is false, the existing thumbs
// for the requested sizes are deleted first so the regen actually rewrites
// the files.
func buildOnePhotoThumbs(
	ctx context.Context, reader database.PhotoReader,
	store *storage.Storage, photoUID string,
	sizes []string, onlyMissing bool,
) thumbServiceOutcome {
	photo, err := reader.GetPhoto(ctx, photoUID)
	if err != nil {
		return thumbServiceOutcome{status: thumbServiceError, err: fmt.Errorf("get photo: %w", err)}
	}
	absPath, err := store.AbsOriginal(photo.FilePath)
	if err != nil {
		return thumbServiceOutcome{status: thumbServiceError, err: fmt.Errorf("resolve original: %w", err)}
	}
	if _, statErr := os.Stat(absPath); statErr != nil {
		return thumbServiceOutcome{status: thumbServiceError, err: fmt.Errorf("stat original: %w", statErr)}
	}

	if !onlyMissing {
		for _, name := range sizes {
			rel, relErr := storage.ThumbRelPath(photo.FileHash, name)
			if relErr != nil {
				continue
			}
			_ = store.DeleteThumb(rel)
		}
	}

	missing := countMissingServiceThumbs(store, photo.FileHash, sizes)
	if missing == 0 {
		return thumbServiceOutcome{status: thumbServiceSkipped}
	}

	decodable, cleanup, err := imgconvert.EnsureDecodable(ctx, absPath)
	if err != nil {
		return thumbServiceOutcome{status: thumbServiceError, err: fmt.Errorf("ensure decodable: %w", err)}
	}
	defer cleanup()

	src := thumb.Source{Path: decodable, Orientation: photo.FileOrientation}
	if _, err := thumb.GenerateSizes(src, sizes, store, photo.FileHash); err != nil {
		return thumbServiceOutcome{status: thumbServiceError, err: fmt.Errorf("generate thumbs: %w", err)}
	}
	return thumbServiceOutcome{status: thumbServiceGenerated, wrote: missing}
}

// countMissingServiceThumbs returns the number of requested sizes whose
// thumbnail file is not yet present in the cache. Mirrors the CLI helper
// of the same name; kept independent so the two packages do not have to
// import each other.
func countMissingServiceThumbs(store *storage.Storage, fileHash string, sizes []string) int {
	missing := 0
	for _, name := range sizes {
		rel, err := storage.ThumbRelPath(fileHash, name)
		if err != nil {
			continue
		}
		if !store.ThumbExists(rel) {
			missing++
		}
	}
	return missing
}

// sendBuildThumbsProgress emits a `progress` SSE event after a photo's
// thumbnails are (un)processed. The payload matches the spec:
// `{ done, total, current_photo_uid }`.
func (h *ProcessHandler) sendBuildThumbsProgress(job *ProcessJob, done int, currentUID string) {
	job.mu.Lock()
	job.ProcessedPhotos = done
	total := job.TotalPhotos
	job.mu.Unlock()
	job.SendEvent(JobEvent{
		Type: "progress",
		Data: map[string]any{
			"done":              done,
			"total":             total,
			"current_photo_uid": currentUID,
		},
	})
}

// completeBuildThumbsJob transitions a build-thumbs job to "completed"
// and emits the final `summary` SSE event holding the generated /
// skipped / failed counts.
func (h *ProcessHandler) completeBuildThumbsJob(job *ProcessJob, result *BuildThumbsJobResult) {
	now := time.Now()
	job.mu.Lock()
	job.Status = JobStatusCompleted
	job.CompletedAt = &now
	job.BuildResult = result
	job.mu.Unlock()

	recordJobCompleted(JobKindProcess)
	job.SendEvent(JobEvent{Type: "summary", Data: result})
	job.SendEvent(JobEvent{Type: "completed", Data: result})
}
