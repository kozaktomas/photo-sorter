package database

import "testing"

func TestPageSlotStateHelpers(t *testing.T) {
	tests := []struct {
		name  string
		slot  PageSlot
		empty bool
		text  bool
		caps  bool
		conts bool
	}{
		{
			name:  "empty slot",
			slot:  PageSlot{SlotIndex: 0},
			empty: true,
		},
		{
			name: "photo slot",
			slot: PageSlot{SlotIndex: 0, PhotoUID: "p1"},
		},
		{
			name: "text slot",
			slot: PageSlot{SlotIndex: 0, TextContent: "hello"},
			text: true,
		},
		{
			name: "captions slot",
			slot: PageSlot{SlotIndex: 0, IsCaptionsSlot: true},
			caps: true,
		},
		{
			name:  "contents slot",
			slot:  PageSlot{SlotIndex: 0, IsContentsSlot: true},
			conts: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.slot.IsEmpty(); got != tc.empty {
				t.Errorf("IsEmpty() = %v, want %v", got, tc.empty)
			}
			if got := tc.slot.IsTextSlot(); got != tc.text {
				t.Errorf("IsTextSlot() = %v, want %v", got, tc.text)
			}
			if got := tc.slot.IsCaptions(); got != tc.caps {
				t.Errorf("IsCaptions() = %v, want %v", got, tc.caps)
			}
			if got := tc.slot.IsContents(); got != tc.conts {
				t.Errorf("IsContents() = %v, want %v", got, tc.conts)
			}
		})
	}
}
