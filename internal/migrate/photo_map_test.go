package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestPhotoMapJSON_RoundTrip writes a photo map to disk and reads it
// back, confirming the version, source, and both maps survive.
func TestPhotoMapJSON_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "photo-map.json")

	photoMap := map[string]string{"p001": "p001", "p002": "p002"}
	fileMap := map[string]string{"f001": "p001"}
	if err := writePhotoMap(path, photoMap, fileMap); err != nil {
		t.Fatalf("write: %v", err)
	}

	loaded, err := LoadPhotoMap(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Version != photoMapJSONVersion {
		t.Errorf("Version = %d, want %d", loaded.Version, photoMapJSONVersion)
	}
	if loaded.Source != "photoprism" {
		t.Errorf("Source = %q, want %q", loaded.Source, "photoprism")
	}
	if got := loaded.PhotoUIDMap["p001"]; got != "p001" {
		t.Errorf("PhotoUIDMap[p001] = %q, want p001", got)
	}
	if got := loaded.FileUIDMap["f001"]; got != "p001" {
		t.Errorf("FileUIDMap[f001] = %q, want p001", got)
	}
	if !loaded.IdentityMap() {
		t.Errorf("IdentityMap() = false, want true (all keys equal values)")
	}
}

// TestPhotoMapJSON_NonIdentity confirms IdentityMap returns false when
// any key maps to a different value.
func TestPhotoMapJSON_NonIdentity(t *testing.T) {
	p := &PhotoMapJSON{
		PhotoUIDMap: map[string]string{"old-1": "new-1", "p002": "p002"},
	}
	if p.IdentityMap() {
		t.Errorf("IdentityMap() = true, want false (one pair differs)")
	}
}

// TestPhotoMapJSON_EmptyIsIdentity treats an empty (or nil) map as
// identity so the remap command can short-circuit on a fresh deploy.
func TestPhotoMapJSON_EmptyIsIdentity(t *testing.T) {
	var p *PhotoMapJSON
	if !p.IdentityMap() {
		t.Errorf("nil map: IdentityMap() = false, want true")
	}
	p = &PhotoMapJSON{}
	if !p.IdentityMap() {
		t.Errorf("empty map: IdentityMap() = false, want true")
	}
}

// TestLoadPhotoMap_VersionMismatch rejects payloads with a different
// schema version so the operator notices when the format changes under
// them.
func TestLoadPhotoMap_VersionMismatch(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "photo-map.json")
	buf, _ := json.Marshal(map[string]any{
		"version":       99,
		"generated_at":  "2025-01-01T00:00:00Z",
		"source":        "photoprism",
		"photo_uid_map": map[string]string{},
		"file_uid_map":  map[string]string{},
	})
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := LoadPhotoMap(path); err == nil {
		t.Errorf("expected error on version mismatch, got nil")
	}
}

// TestBuildValuesClause produces a deterministic clause with ::text
// casts and the corresponding flat args slice.
func TestBuildValuesClause(t *testing.T) {
	clause, args := buildValuesClause([][2]string{
		{"old-1", "new-1"},
		{"old-2", "new-2"},
	})
	want := "($1::text, $2::text), ($3::text, $4::text)"
	if clause != want {
		t.Errorf("clause = %q, want %q", clause, want)
	}
	if len(args) != 4 || args[0] != "old-1" || args[3] != "new-2" {
		t.Errorf("args = %v, want [old-1 new-1 old-2 new-2]", args)
	}
}

// TestNonIdentityPairs drops empty entries and identity entries so the
// remap pass doesn't waste a round-trip on no-op rows.
func TestNonIdentityPairs(t *testing.T) {
	pairs := nonIdentityPairs(map[string]string{
		"same":  "same",
		"old-1": "new-1",
		"":      "x",
		"y":     "",
	})
	if len(pairs) != 1 || pairs[0][0] != "old-1" || pairs[0][1] != "new-1" {
		t.Errorf("nonIdentityPairs = %v, want [[old-1 new-1]]", pairs)
	}
}
