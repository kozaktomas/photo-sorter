package handlers

import (
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/photopipe"
)

// TestEmitNearDuplicates_DispatchesEventToListener verifies that the
// "near_duplicates" SSE event is broadcast to any active listeners with
// the supplied payload intact. This is the backend half of the upload
// flow's near-duplicate UI surface; the frontend `DropZone.tsx` consumes
// the event but renders are not exercised here.
func TestEmitNearDuplicates_DispatchesEventToListener(t *testing.T) {
	t.Parallel()

	job := &UploadJob{ID: "test-job", Status: JobStatusRunning}
	ch := job.AddListener()
	t.Cleanup(func() { job.RemoveListener(ch) })

	takenAt := time.Date(2024, 5, 2, 12, 0, 0, 0, time.UTC)
	ev := NearDuplicatesEvent{
		FileName: "IMG_0001.jpg",
		PhotoUID: "pnew000000000000",
		Matches: []photopipe.DuplicateMatch{
			{
				PhotoUID:       "pexisting000000",
				FileName:       "IMG_0001.jpg",
				TakenAt:        &takenAt,
				ScorePHash:     2,
				ScoreEmbedding: 0.97,
			},
		},
	}

	go EmitNearDuplicates(job, ev)

	select {
	case got := <-ch:
		if got.Type != "near_duplicates" {
			t.Fatalf("event type = %q, want near_duplicates", got.Type)
		}
		payload, ok := got.Data.(NearDuplicatesEvent)
		if !ok {
			t.Fatalf("event data type = %T, want NearDuplicatesEvent", got.Data)
		}
		if payload.FileName != ev.FileName {
			t.Errorf("filename = %q, want %q", payload.FileName, ev.FileName)
		}
		if payload.PhotoUID != ev.PhotoUID {
			t.Errorf("photo_uid = %q, want %q", payload.PhotoUID, ev.PhotoUID)
		}
		if len(payload.Matches) != 1 {
			t.Fatalf("len(matches) = %d, want 1", len(payload.Matches))
		}
		if payload.Matches[0].ScorePHash != 2 {
			t.Errorf("matches[0].ScorePHash = %d, want 2", payload.Matches[0].ScorePHash)
		}
		if payload.Matches[0].ScoreEmbedding != 0.97 {
			t.Errorf("matches[0].ScoreEmbedding = %v, want 0.97", payload.Matches[0].ScoreEmbedding)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for near_duplicates event")
	}
}

// TestEmitNearDuplicates_SkipsEmptyMatches verifies that the helper is a
// no-op when the matches slice is empty — the frontend should never
// receive an event for a clean upload because the UI uses event presence
// alone to decide whether to render the warning bar.
func TestEmitNearDuplicates_SkipsEmptyMatches(t *testing.T) {
	t.Parallel()

	job := &UploadJob{ID: "test-job-empty", Status: JobStatusRunning}
	ch := job.AddListener()
	t.Cleanup(func() { job.RemoveListener(ch) })

	EmitNearDuplicates(job, NearDuplicatesEvent{
		FileName: "no-matches.jpg",
		Matches:  nil,
	})

	select {
	case got := <-ch:
		t.Fatalf("unexpected event received: %+v", got)
	case <-time.After(50 * time.Millisecond):
		// Pass — no event was sent for the empty match set.
	}
}
