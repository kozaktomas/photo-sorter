package database

// Page-size caps for the vector export feeds (GET /embeddings, GET /faces).
//
// Payload size is the binding constraint here, not row count. A 768-dim CLIP
// embedding serialises to roughly 9 KB as a JSON number array (≈4 KB as a
// base64 float32 array); a 512-dim face vector to roughly 6 KB (≈2.7 KB).
// The caps below therefore bound a single response at a few MB even in the
// fat JSON encoding — enough that a 20k-photo export completes in a couple
// of hundred round trips, while never asking the server to materialise the
// whole 112k-row face table (≈700 MB of JSON) in one go.
//
// Both feeds clamp rather than reject an oversized limit, matching
// ClampPhotoLimit: a client that asks for more simply gets the cap, and the
// keyset cursor in the response tells it where to continue from.
const (
	// DefaultEmbeddingExportLimit is the page size the embedding feed applies
	// when no limit is requested.
	DefaultEmbeddingExportLimit = 100

	// MaxEmbeddingExportLimit caps the embedding feed's page size
	// (≈4.5 MB of JSON, ≈2 MB of base64 per page).
	MaxEmbeddingExportLimit = 500

	// DefaultFaceExportLimit is the page size the face feed applies when no
	// limit is requested.
	DefaultFaceExportLimit = 200

	// MaxFaceExportLimit caps the face feed's page size (≈6 MB of JSON,
	// ≈2.7 MB of base64 per page).
	MaxFaceExportLimit = 1000
)

// ClampEmbeddingExportLimit returns the page size the embedding feed will
// actually apply for a requested limit.
//
// Like ClampPhotoLimit, this is shared between the repository and the handler
// on purpose: the handler needs the *effective* limit to decide whether a page
// came back full (and therefore whether to mint a next cursor). If the two
// disagreed, a full page would read as a short one and the export would stop
// early, silently missing vectors.
func ClampEmbeddingExportLimit(limit int) int {
	return clampExportLimit(limit, DefaultEmbeddingExportLimit, MaxEmbeddingExportLimit)
}

// ClampFaceExportLimit returns the page size the face feed will actually apply
// for a requested limit. See ClampEmbeddingExportLimit for why it is shared.
func ClampFaceExportLimit(limit int) int {
	return clampExportLimit(limit, DefaultFaceExportLimit, MaxFaceExportLimit)
}

// clampExportLimit applies the default for a non-positive limit and the cap
// for an oversized one.
func clampExportLimit(limit, defaultLimit, maxLimit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}
