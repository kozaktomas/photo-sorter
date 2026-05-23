package handlers

import (
	"context"
	"sync"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/ai"
	"github.com/kozaktomas/photo-sorter/internal/constants"
)

// JobStatus represents the status of an async job.
type JobStatus string

// JobStatus constants define the lifecycle states of an async job.
const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
)

// SortJob represents an async sort job.
type SortJob struct {
	EventBroadcaster

	ID              string         `json:"id"`
	AlbumUID        string         `json:"album_uid"`
	AlbumTitle      string         `json:"album_title"`
	Status          JobStatus      `json:"status"`
	Progress        int            `json:"progress"`
	TotalPhotos     int            `json:"total_photos"`
	ProcessedPhotos int            `json:"processed_photos"`
	Error           string         `json:"error,omitempty"`
	StartedAt       time.Time      `json:"started_at"`
	CompletedAt     *time.Time     `json:"completed_at,omitempty"`
	Options         SortJobOptions `json:"options"`
	Result          *SortJobResult `json:"result,omitempty"`

	events chan JobEvent
}

// GetStatus returns the current job status (implements SSEJob).
func (j *SortJob) GetStatus() JobStatus {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.Status
}

// Cancel cancels the sort job.
func (j *SortJob) Cancel() {
	j.EventBroadcaster.Cancel()
	j.mu.Lock()
	j.Status = JobStatusCancelled
	j.mu.Unlock()
}

// SortJobOptions represents sort job options.
type SortJobOptions struct {
	DryRun          bool   `json:"dry_run"`
	Limit           int    `json:"limit"`
	IndividualDates bool   `json:"individual_dates"`
	BatchMode       bool   `json:"batch_mode"`
	Provider        string `json:"provider"`
	ForceDate       bool   `json:"force_date"`
	Concurrency     int    `json:"concurrency"`
}

// SortJobResult represents the result of a sort job.
type SortJobResult struct {
	ProcessedCount int                 `json:"processed_count"`
	SortedCount    int                 `json:"sorted_count"`
	AlbumDate      string              `json:"album_date,omitempty"`
	DateReasoning  string              `json:"date_reasoning,omitempty"`
	Errors         []string            `json:"errors,omitempty"`
	Suggestions    []ai.SortSuggestion `json:"suggestions,omitempty"`
	Usage          *UsageInfo          `json:"usage,omitempty"`
}

// UsageInfo represents API usage information.
type UsageInfo struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	TotalCost    float64 `json:"total_cost"`
}

// JobEvent represents an event from a job.
type JobEvent struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// EventBroadcaster provides listener management and event broadcasting for
// async jobs. Embed this in job structs to get AddListener, RemoveListener,
// SendEvent, and a job-scoped cancellation context.
//
// Lifecycle: the HTTP handler that creates a job calls InitContext BEFORE
// spawning the worker goroutine, so DELETE /jobs/{id} arriving between
// CreateJob and the goroutine actually starting is still observable by the
// worker via ctx cancellation. The worker reads the context via Context()
// (or its passed-in ctx) and calls Release on exit so the parent context's
// goroutine does not leak.
type EventBroadcaster struct {
	ctx       context.Context //nolint:containedctx // job-scoped cancellation
	cancel    context.CancelFunc
	listeners []chan JobEvent
	mu        sync.RWMutex
}

// InitContext initialises the job's cancellation context. Idempotent: a
// second call returns the previously-stored context. Always call this
// BEFORE launching the worker goroutine so that a Cancel() arriving while
// the goroutine is still scheduling will actually short-circuit the work.
//
// Returns the child context the worker should pass into downstream calls
// (DB, HTTP, subprocess). The cancel func is held inside the broadcaster
// and invoked by Cancel() or Release().
func (b *EventBroadcaster) InitContext(parent context.Context) context.Context {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ctx != nil {
		return b.ctx
	}
	if parent == nil {
		parent = context.Background()
	}
	b.ctx, b.cancel = context.WithCancel(parent)
	return b.ctx
}

// Context returns the job context, or context.Background if InitContext was
// never called (the latter is a programmer error but is tolerated so callers
// constructed in tests without going through CreateJob still work).
func (b *EventBroadcaster) Context() context.Context {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.ctx == nil {
		return context.Background()
	}
	return b.ctx
}

// Release cancels the job context without emitting a "cancelled" SSE event.
// Use it as a `defer` in the worker so the parent's WithCancel goroutine
// terminates when the worker returns normally. Safe to call multiple times.
func (b *EventBroadcaster) Release() {
	b.mu.Lock()
	cancel := b.cancel
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// AddListener adds an event listener.
func (b *EventBroadcaster) AddListener() chan JobEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan JobEvent, constants.EventChannelBuffer)
	b.listeners = append(b.listeners, ch)
	return ch
}

// RemoveListener removes an event listener.
func (b *EventBroadcaster) RemoveListener(ch chan JobEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, listener := range b.listeners {
		if listener == ch {
			b.listeners = append(b.listeners[:i], b.listeners[i+1:]...)
			close(ch)
			return
		}
	}
}

// SendEvent sends an event to all listeners.
func (b *EventBroadcaster) SendEvent(event JobEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, listener := range b.listeners {
		select {
		case listener <- event:
		default:
			// Listener buffer full, skip.
		}
	}
}

// Cancel cancels the job via context and sends a cancelled event. Safe to
// call multiple times and safe to call before InitContext (no-ops the
// cancel). The cancel/listeners reads happen under b.mu so concurrent
// InitContext from the worker goroutine cannot race with Cancel from the
// HTTP handler.
func (b *EventBroadcaster) Cancel() {
	b.mu.Lock()
	cancel := b.cancel
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	b.SendEvent(JobEvent{Type: "cancelled", Message: "Job cancelled by user"})
}

// SSEJob is the interface required by streamSSEEvents to stream job events via SSE.
type SSEJob interface {
	AddListener() chan JobEvent
	RemoveListener(ch chan JobEvent)
	GetStatus() JobStatus
}

// JobManager manages async jobs.
type JobManager struct {
	jobs map[string]*SortJob
	mu   sync.RWMutex
}

// NewJobManager creates a new job manager.
func NewJobManager() *JobManager {
	return &JobManager{
		jobs: make(map[string]*SortJob),
	}
}

// CreateJob creates a new sort job. The job's cancellation context is
// initialised before the job is returned so a DELETE arriving between
// CreateJob and the worker goroutine actually starting is honoured.
func (m *JobManager) CreateJob(id, albumUID, albumTitle string, options SortJobOptions) *SortJob {
	job := &SortJob{
		ID:         id,
		AlbumUID:   albumUID,
		AlbumTitle: albumTitle,
		Status:     JobStatusPending,
		StartedAt:  time.Now(),
		Options:    options,
		events:     make(chan JobEvent, 100),
	}
	job.InitContext(context.Background())

	m.mu.Lock()
	m.jobs[id] = job
	m.mu.Unlock()

	return job
}

// GetJob retrieves a job by ID.
func (m *JobManager) GetJob(id string) *SortJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.jobs[id]
}

// DeleteJob removes a job.
func (m *JobManager) DeleteJob(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.jobs, id)
}

// ListJobs returns all jobs.
func (m *JobManager) ListJobs() []*SortJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	jobs := make([]*SortJob, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}
