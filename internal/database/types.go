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

// StoredEraEmbedding represents a CLIP text embedding centroid for a photo era
type StoredEraEmbedding struct {
	EraSlug            string
	EraName            string
	RepresentativeDate string // "YYYY-MM-DD"
	PromptCount        int
	Embedding          []float32
	Model              string
	Pretrained         string
	Dim                int
	CreatedAt          time.Time
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

// PhotoBook represents a photo book project
type PhotoBook struct {
	ID          string
	Title       string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// BookSection represents an ordered group within a book
type BookSection struct {
	ID         string
	BookID     string
	Title      string
	SortOrder  int
	PhotoCount int // computed, not stored
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SectionPhoto represents a photo in a section's prepick pool
type SectionPhoto struct {
	ID          int64
	SectionID   string
	PhotoUID    string
	Description string
	Note        string
	AddedAt     time.Time
}

// BookPage represents a page with a specific format
type BookPage struct {
	ID          string
	BookID      string
	SectionID   string // optional, may be empty
	Format      string
	Description string
	SortOrder   int
	Slots       []PageSlot // populated on read
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// PageSlot represents a photo assignment to a position on a page
type PageSlot struct {
	SlotIndex int
	PhotoUID  string // empty = unoccupied
}

// PhotoBookMembership represents a book+section that contains a photo
type PhotoBookMembership struct {
	BookID      string
	BookTitle   string
	SectionID   string
	SectionTitle string
}

// PageFormatSlotCount returns the number of slots for a given page format
func PageFormatSlotCount(format string) int {
	switch format {
	case "4_landscape":
		return 4
	case "2l_1p":
		return 3
	case "1p_2l":
		return 3
	case "2_portrait":
		return 2
	case "1_fullscreen":
		return 1
	default:
		return 0
	}
}
