package mock_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/mock"
)

const testPageID = "page-1"

func TestAssignContentsSlot_ClearsOtherContent(t *testing.T) {
	m := mock.NewMockBookWriter()
	ctx := context.Background()

	if err := m.AssignSlot(ctx, testPageID, 0, "photo-abc"); err != nil {
		t.Fatalf("AssignSlot: %v", err)
	}
	if err := m.AssignContentsSlot(ctx, testPageID, 0); err != nil {
		t.Fatalf("AssignContentsSlot: %v", err)
	}
	slots, err := m.GetPageSlots(ctx, testPageID)
	if err != nil {
		t.Fatalf("GetPageSlots: %v", err)
	}
	if len(slots) != 1 {
		t.Fatalf("expected 1 slot, got %d", len(slots))
	}
	s := slots[0]
	if !s.IsContentsSlot {
		t.Errorf("IsContentsSlot = false, want true")
	}
	if s.PhotoUID != "" || s.TextContent != "" || s.IsCaptionsSlot {
		t.Errorf("non-contents fields should be cleared, got %+v", s)
	}
}

func TestAssignContentsSlot_OnePerPage(t *testing.T) {
	m := mock.NewMockBookWriter()
	ctx := context.Background()

	if err := m.AssignContentsSlot(ctx, testPageID, 0); err != nil {
		t.Fatalf("first AssignContentsSlot: %v", err)
	}
	err := m.AssignContentsSlot(ctx, testPageID, 1)
	if !errors.Is(err, database.ErrContentsSlotExists) {
		t.Fatalf("second contents slot on different index: got %v, want ErrContentsSlotExists", err)
	}
}

func TestAssignContentsSlot_SameIndexIsIdempotent(t *testing.T) {
	m := mock.NewMockBookWriter()
	ctx := context.Background()

	if err := m.AssignContentsSlot(ctx, testPageID, 2); err != nil {
		t.Fatalf("first AssignContentsSlot: %v", err)
	}
	if err := m.AssignContentsSlot(ctx, testPageID, 2); err != nil {
		t.Fatalf("re-assigning same index must not error: %v", err)
	}
}

func TestAssignSlot_ClearsContentsFlag(t *testing.T) {
	m := mock.NewMockBookWriter()
	ctx := context.Background()

	if err := m.AssignContentsSlot(ctx, testPageID, 0); err != nil {
		t.Fatalf("AssignContentsSlot: %v", err)
	}
	if err := m.AssignSlot(ctx, testPageID, 0, "photo-1"); err != nil {
		t.Fatalf("AssignSlot: %v", err)
	}
	slots, err := m.GetPageSlots(ctx, testPageID)
	if err != nil {
		t.Fatalf("GetPageSlots: %v", err)
	}
	if len(slots) != 1 || slots[0].IsContentsSlot {
		t.Fatalf("contents flag should be cleared after photo assignment, got %+v", slots)
	}
}
