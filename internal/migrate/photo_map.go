package migrate

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"time"
)

// PhotoMapJSON is the on-disk schema produced by `migrate-from-photoprism
// --emit-photo-map`. It is consumed by `migrate-remap-references` and is
// safe for an operator to inspect or hand-edit.
//
// Now that the migrator preserves PhotoPrism UIDs verbatim, the
// PhotoUIDMap field is an identity mapping in the happy path. The file is
// still emitted so:
//   - operators can verify the migration preserved every UID by running
//     `migrate-remap-references --dry-run` (which short-circuits on an
//     identity map and exits 0);
//   - operators who ran an older buggy version can take a hand-edited
//     file (mapping old generated UIDs to the new PhotoPrism UIDs) and
//     remap their existing native rows.
//
// FileUIDMap maps PhotoPrism file_uid → native photo UID (the same value
// the migrator's in-memory fileMap holds). It is informational; the
// remap command currently does not act on it because the native schema
// has no file_uid columns to update.
type PhotoMapJSON struct {
	Version     int               `json:"version"`
	GeneratedAt time.Time         `json:"generated_at"`
	Source      string            `json:"source"`
	PhotoUIDMap map[string]string `json:"photo_uid_map"`
	FileUIDMap  map[string]string `json:"file_uid_map"`
}

// photoMapJSONVersion is the schema version embedded in the emitted
// JSON. Bump when adding required fields so consumers can fail fast on
// unsupported input.
const photoMapJSONVersion = 1

// writePhotoMap serialises the migrator's in-memory mappings to a JSON
// file at path. The file is written atomically (write to <path>.tmp,
// rename) so a crashed migrator never leaves a partially-written map.
func writePhotoMap(path string, photoMap, fileMap map[string]string) error {
	if path == "" {
		return nil
	}
	payload := PhotoMapJSON{
		Version:     photoMapJSONVersion,
		GeneratedAt: time.Now().UTC(),
		Source:      "photoprism",
		PhotoUIDMap: copyStringMap(photoMap),
		FileUIDMap:  copyStringMap(fileMap),
	}
	buf, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal photo map: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return fmt.Errorf("write photo map %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename photo map %s -> %s: %w", tmp, path, err)
	}
	return nil
}

// copyStringMap returns a shallow copy so the on-disk payload cannot be
// mutated by the migrator after the emit.
func copyStringMap(src map[string]string) map[string]string {
	out := make(map[string]string, len(src))
	maps.Copy(out, src)
	return out
}

// LoadPhotoMap reads a photo-map JSON written by writePhotoMap. Used by
// the migrate-remap-references command. Returns a non-nil pointer even
// when individual maps inside the file are empty so callers can rely on
// the field shape.
func LoadPhotoMap(path string) (*PhotoMapJSON, error) {
	buf, err := os.ReadFile(path) //#nosec G304 -- operator-supplied path
	if err != nil {
		return nil, fmt.Errorf("read photo map %s: %w", path, err)
	}
	var p PhotoMapJSON
	if err := json.Unmarshal(buf, &p); err != nil {
		return nil, fmt.Errorf("parse photo map %s: %w", path, err)
	}
	if p.Version != photoMapJSONVersion {
		return nil, fmt.Errorf(
			"photo map %s: unsupported version %d (expected %d)",
			path, p.Version, photoMapJSONVersion,
		)
	}
	if p.PhotoUIDMap == nil {
		p.PhotoUIDMap = map[string]string{}
	}
	if p.FileUIDMap == nil {
		p.FileUIDMap = map[string]string{}
	}
	return &p, nil
}
