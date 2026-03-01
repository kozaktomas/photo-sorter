package database

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/coder/hnsw"
)

// HNSWIndexMetadata stores metadata for validating cached HNSW indexes.
type HNSWIndexMetadata struct {
	FaceCount int64     `json:"face_count"`
	MaxFaceID int64     `json:"max_face_id"`
	BuildTime time.Time `json:"build_time"`
	Version   int       `json:"version"` // For future compatibility
}

const hnswMetadataVersion = 1

// HNSWIndex wraps the HNSW graph for face embedding search.
type HNSWIndex struct {
	graph      *hnsw.Graph[int64]
	savedGraph *hnsw.SavedGraph[int64] // For persistence
	idToFace   map[int64]*StoredFace   // Maps HNSW node ID to face
	mu         sync.RWMutex
	path       string // Path to save/load index
}

// NewHNSWIndex creates a new empty HNSW index.
func NewHNSWIndex() *HNSWIndex {
	return &HNSWIndex{
		idToFace: make(map[int64]*StoredFace),
	}
}

// BuildFromFaces builds the index from a slice of faces.
func (h *HNSWIndex) BuildFromFaces(faces []StoredFace) error { //nolint:dupl
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(faces) == 0 {
		h.graph = nil
		h.savedGraph = nil
		h.idToFace = make(map[int64]*StoredFace)
		return nil
	}

	// Create new graph with cosine distance.
	g := hnsw.NewGraph[int64]()
	g.M = HNSWMaxNeighbors
	g.Ml = 1.0 / float64(HNSWMaxNeighbors) // Standard HNSW formula
	g.Distance = hnsw.CosineDistance

	h.idToFace = make(map[int64]*StoredFace, len(faces))

	// Add all faces to the graph.
	for i := range faces {
		face := &faces[i]
		if len(face.Embedding) == 0 {
			continue
		}

		g.Add(hnsw.MakeNode(face.ID, face.Embedding))
		h.idToFace[face.ID] = face
	}

	h.graph = g
	return nil
}

// Search finds the k nearest neighbors to the query embedding.
// Returns face IDs and their distances.
func (h *HNSWIndex) Search(query []float32, k int) ([]int64, []float64, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.graph == nil && h.savedGraph == nil {
		return nil, nil, errors.New("index not initialized")
	}

	var neighbors []hnsw.Node[int64]
	if h.savedGraph != nil {
		neighbors = h.savedGraph.Search(query, k)
	} else {
		neighbors = h.graph.Search(query, k)
	}

	ids := make([]int64, len(neighbors))
	distances := make([]float64, len(neighbors))

	for i, n := range neighbors {
		ids[i] = n.Key
		// Compute actual cosine distance using the embedding from the node directly.
		// This avoids needing the idToFace map for distance computation.
		if len(n.Value) > 0 {
			distances[i] = float64(CosineDistance(query, n.Value))
		}
	}

	return ids, distances, nil
}

// GetFace returns the face for a given ID.
func (h *HNSWIndex) GetFace(id int64) *StoredFace {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.idToFace[id]
}

// Add adds a single face to the index.
func (h *HNSWIndex) Add(face *StoredFace) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(face.Embedding) == 0 {
		return nil
	}

	if h.graph == nil {
		// Create new graph.
		h.graph = hnsw.NewGraph[int64]()
		h.graph.M = HNSWMaxNeighbors
		h.graph.Ml = 1.0 / float64(HNSWMaxNeighbors)
		h.graph.Distance = hnsw.CosineDistance
	}

	h.graph.Add(hnsw.MakeNode(face.ID, face.Embedding))
	h.idToFace[face.ID] = face

	return nil
}

// UpdateFaceMetadata updates metadata fields of a face in the idToFace map by database ID.
// Returns true if the face was found and updated, false if not found.
func (h *HNSWIndex) UpdateFaceMetadata(id int64, markerUID, subjectUID, subjectName string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	face, ok := h.idToFace[id]
	if !ok {
		return false
	}
	face.MarkerUID = markerUID
	face.SubjectUID = subjectUID
	face.SubjectName = subjectName
	return true
}

// Delete removes a face from the index (marks as deleted).
func (h *HNSWIndex) Delete(id int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.idToFace, id)
	// Note: HNSW doesn't support true deletion, but removing from idToFace.
	// effectively removes it from search results since we filter by lookup.
}

// SetPath sets the path for saving/loading the index.
func (h *HNSWIndex) SetPath(path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.path = path
}

// Save persists the index to disk.
func (h *HNSWIndex) Save() error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.path == "" {
		return nil // No path set
	}

	if h.graph == nil {
		// Remove existing file if index is empty (best-effort cleanup).
		_ = os.Remove(h.path)
		return nil
	}

	// Write to file.
	f, err := os.Create(h.path)
	if err != nil {
		return fmt.Errorf("failed to create HNSW index file: %w", err)
	}
	defer f.Close()

	if err := h.graph.Export(f); err != nil {
		return fmt.Errorf("exporting HNSW graph: %w", err)
	}
	return nil
}

// Load loads the index from disk.
func (h *HNSWIndex) Load(path string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.path = path

	// Check if file exists.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil // No index file, will build from faces
	}

	saved, err := hnsw.LoadSavedGraph[int64](path)
	if err != nil {
		return fmt.Errorf("failed to load HNSW index: %w", err)
	}

	h.savedGraph = saved
	return nil
}

// Count returns the number of indexed faces.
func (h *HNSWIndex) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.idToFace)
}

// IsEmpty returns true if the index has no graph data loaded.
// Note: idToFace is populated separately by RebuildFromFaces after loading.
func (h *HNSWIndex) IsEmpty() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.graph == nil && h.savedGraph == nil
}

// RebuildFromFaces rebuilds the idToFace map from faces.
// Called after loading index from disk.
func (h *HNSWIndex) RebuildFromFaces(faces []StoredFace) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.idToFace = make(map[int64]*StoredFace, len(faces))
	for i := range faces {
		h.idToFace[faces[i].ID] = &faces[i]
	}
}

// SaveWithMetadata persists the index to disk along with metadata for staleness detection.
func (h *HNSWIndex) SaveWithMetadata(path string, metadata HNSWIndexMetadata) error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.graph == nil {
		// Remove existing files if index is empty (best-effort cleanup).
		_ = os.Remove(path)
		_ = os.Remove(path + ".meta")
		return nil
	}

	// Write graph to file.
	f, err := os.Create(path) //nolint:gosec // path is from trusted config
	if err != nil {
		return fmt.Errorf("failed to create HNSW index file: %w", err)
	}
	defer f.Close()

	if err := h.graph.Export(f); err != nil {
		return fmt.Errorf("failed to export HNSW graph: %w", err)
	}

	// Write metadata to separate file.
	metadata.Version = hnswMetadataVersion
	metaData, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(path+".meta", metaData, 0600); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	return nil
}

// LoadHNSWMetadata loads metadata from a separate .meta file.
func LoadHNSWMetadata(path string) (HNSWIndexMetadata, error) {
	var metadata HNSWIndexMetadata

	metaPath := path + ".meta"
	data, err := os.ReadFile(metaPath) //nolint:gosec // path is from trusted config
	if err != nil {
		return metadata, fmt.Errorf("failed to read metadata file: %w", err)
	}

	if err := json.Unmarshal(data, &metadata); err != nil {
		return metadata, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return metadata, nil
}

// SaveFaceMetadata saves face metadata to a .faces file for fast loading at startup.
func SaveFaceMetadata(path string, faces []StoredFace) error {
	facesPath := path + ".faces"

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(faces); err != nil {
		return fmt.Errorf("failed to encode faces: %w", err)
	}

	if err := os.WriteFile(facesPath, buf.Bytes(), 0600); err != nil {
		return fmt.Errorf("failed to write faces file: %w", err)
	}

	return nil
}

// LoadFaceMetadata loads face metadata from a .faces file.
func LoadFaceMetadata(path string) ([]StoredFace, error) {
	facesPath := path + ".faces"

	data, err := os.ReadFile(facesPath) //nolint:gosec // path is from trusted config
	if err != nil {
		return nil, fmt.Errorf("failed to read faces file: %w", err)
	}

	var faces []StoredFace
	dec := gob.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&faces); err != nil {
		return nil, fmt.Errorf("failed to decode faces: %w", err)
	}

	return faces, nil
}

// LoadWithFaceMetadata loads both the HNSW graph and face metadata from disk.
func (h *HNSWIndex) LoadWithFaceMetadata(path string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.path = path

	// Load HNSW graph.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("HNSW index file not found: %s", path)
	}

	saved, err := hnsw.LoadSavedGraph[int64](path)
	if err != nil {
		return fmt.Errorf("failed to load HNSW index: %w", err)
	}

	// Load face metadata.
	faces, err := LoadFaceMetadata(path)
	if err != nil {
		return fmt.Errorf("failed to load face metadata: %w", err)
	}

	h.savedGraph = saved
	h.idToFace = make(map[int64]*StoredFace, len(faces))
	for i := range faces {
		h.idToFace[faces[i].ID] = &faces[i]
	}

	return nil
}

// exportFaceGraph exports the HNSW graph to the given file path.
func (h *HNSWIndex) exportFaceGraph(path string) error {
	f, err := os.Create(path) //nolint:gosec // path is from trusted config
	if err != nil {
		return fmt.Errorf("failed to create HNSW index file: %w", err)
	}
	if h.savedGraph != nil {
		if err := h.savedGraph.Export(f); err != nil {
			_ = f.Close()
			return fmt.Errorf("failed to export HNSW graph from savedGraph: %w", err)
		}
	} else {
		if err := h.graph.Export(f); err != nil {
			_ = f.Close()
			return fmt.Errorf("failed to export HNSW graph: %w", err)
		}
	}
	_ = f.Close()
	fmt.Printf("Face index: wrote graph to %s\n", path)
	return nil
}

// SaveWithFaceMetadata persists the index and face metadata to disk.
func (h *HNSWIndex) SaveWithFaceMetadata(path string, metadata HNSWIndexMetadata) error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.graph == nil && h.savedGraph == nil {
		fmt.Printf("Face index save: no graph loaded, removing files\n")
		_ = os.Remove(path)
		_ = os.Remove(path + ".meta")
		_ = os.Remove(path + ".faces")
		return nil
	}

	if err := h.exportFaceGraph(path); err != nil {
		return err
	}

	metadata.Version = hnswMetadataVersion
	metaData, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}
	metaPath := path + ".meta"
	if err := os.WriteFile(metaPath, metaData, 0600); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}
	fmt.Printf("Face index: wrote metadata to %s (%d bytes)\n", metaPath, len(metaData))

	faces := make([]StoredFace, 0, len(h.idToFace))
	for _, face := range h.idToFace {
		faces = append(faces, *face)
	}
	if err := SaveFaceMetadata(path, faces); err != nil {
		return fmt.Errorf("failed to save face metadata: %w", err)
	}
	fmt.Printf("Face index: wrote faces to %s.faces (%d faces)\n", path, len(faces))

	return nil
}
