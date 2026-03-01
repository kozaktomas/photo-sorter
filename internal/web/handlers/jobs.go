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

// EventBroadcaster provides listener management and event broadcasting for async jobs.
// Embed this in job structs to get AddListener, RemoveListener, and SendEvent methods.
type EventBroadcaster struct {
	cancel    context.CancelFunc
	listeners []chan JobEvent
	mu        sync.RWMutex
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

// Cancel cancels the job via context and sends a cancelled event.
func (b *EventBroadcaster) Cancel() {
	if b.cancel != nil {
		b.cancel()
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

// CreateJob creates a new sort job.
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
