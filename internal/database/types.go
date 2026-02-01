package database

import (
	"time"
)

// StoredEmbedding represents an embedding stored in the database
type StoredEmbedding struct {
	PhotoUID   string
	Embedding  []float32
	Model      string
	Pretrained string
	Dim        int
	CreatedAt  time.Time
}

// StoredFace represents a face embedding stored in the database
type StoredFace struct {
	ID        int64
	PhotoUID  string
	FaceIndex int
	Embedding []float32
	BBox      []float64 // [x1, y1, x2, y2] in raw pixel coordinates
	DetScore  float64
	Model     string
	Dim       int
	CreatedAt time.Time

	// Cached PhotoPrism data (populated during processing, v3+)
	MarkerUID   string // Matching PhotoPrism marker UID (empty if no marker matched)
	SubjectUID  string // Subject UID from marker (empty if unassigned)
	SubjectName string // Person name from marker (empty if unassigned)
	PhotoWidth  int    // Primary file width in pixels
	PhotoHeight int    // Primary file height in pixels
	Orientation int    // EXIF orientation (1-8)
	FileUID     string // Primary file UID
}

// FaceProcessedRecord represents a record of a photo that has been processed for face detection
type FaceProcessedRecord struct {
	PhotoUID  string
	FaceCount int
	CreatedAt time.Time
}

// ExportData contains all embeddings and faces data for export/storage
type ExportData struct {
	Version        int
	ExportedAt     time.Time
	Embeddings     []StoredEmbedding
	Faces          []StoredFace
	FacesProcessed []FaceProcessedRecord // Photos processed for face detection (v2+)
}

const currentExportVersion = 3
