// Package facematch provides face matching utilities shared between CLI and web handlers.
// This package extracts common face matching logic to eliminate code duplication.
package facematch

// MatchAction represents what action is needed for a matched face
type MatchAction string

const (
	ActionCreateMarker   MatchAction = "create_marker"   // No marker exists, need to create one
	ActionAssignPerson   MatchAction = "assign_person"   // Marker exists but no person assigned
	ActionAlreadyDone    MatchAction = "already_done"    // Marker exists with person already assigned
	ActionUnassignPerson MatchAction = "unassign_person" // Remove person from marker
)
