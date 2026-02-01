package database

// Face filter constants - minimum size for faces to be shown in UI
const (
	// MinFaceWidthPx is the absolute minimum face width in pixels
	MinFaceWidthPx = 35

	// MinFaceWidthRel is the minimum face width relative to photo width (1%)
	MinFaceWidthRel = 0.01
)

// HNSW index parameters for 512-dim face embeddings
const (
	// HNSWMaxNeighbors (M) is the maximum number of neighbors per node.
	// Higher values improve recall but increase memory and build time.
	HNSWMaxNeighbors = 16

	// HNSWEfSearch is the search candidate pool size.
	// Higher values improve recall but slow down search.
	HNSWEfSearch = 100

	// HNSWEfConstruction is used during index building.
	// Higher values improve index quality but slow down construction.
	HNSWEfConstruction = 200

	// HNSWSearchMultiplier is the factor to request more candidates from HNSW
	// to ensure we have enough after distance filtering.
	HNSWSearchMultiplier = 3
)
