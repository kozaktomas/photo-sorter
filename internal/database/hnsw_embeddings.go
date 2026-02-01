package database

import (
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/coder/hnsw"
)

// HNSWEmbeddingIndex wraps the HNSW graph for image embedding search
// Unlike HNSWIndex (faces), this uses PhotoUID (string) as keys
type HNSWEmbeddingIndex struct {
	graph       *hnsw.Graph[string]
	savedGraph  *hnsw.SavedGraph[string] // For persistence
	idToEmb     map[string]*StoredEmbedding
	mu          sync.RWMutex
	path        string // Path to save/load index
}

// NewHNSWEmbeddingIndex creates a new empty HNSW embedding index
func NewHNSWEmbeddingIndex() *HNSWEmbeddingIndex {
	return &HNSWEmbeddingIndex{
		idToEmb: make(map[string]*StoredEmbedding),
	}
}

// BuildFromEmbeddings builds the index from a slice of embeddings
func (h *HNSWEmbeddingIndex) BuildFromEmbeddings(embeddings []StoredEmbedding) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(embeddings) == 0 {
		h.graph = nil
		h.savedGraph = nil
		h.idToEmb = make(map[string]*StoredEmbedding)
		return nil
	}

	// Create new graph with cosine distance
	g := hnsw.NewGraph[string]()
	g.M = HNSWMaxNeighbors
	g.Ml = 1.0 / float64(HNSWMaxNeighbors) // Standard HNSW formula
	g.Distance = hnsw.CosineDistance

	h.idToEmb = make(map[string]*StoredEmbedding, len(embeddings))

	// Add all embeddings to the graph
	for i := range embeddings {
		emb := &embeddings[i]
		if len(emb.Embedding) == 0 {
			continue
		}

		g.Add(hnsw.MakeNode(emb.PhotoUID, emb.Embedding))
		h.idToEmb[emb.PhotoUID] = emb
	}

	h.graph = g
	return nil
}

// Search finds the k nearest neighbors to the query embedding
// Returns photo UIDs and their distances
func (h *HNSWEmbeddingIndex) Search(query []float32, k int) ([]string, []float64, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.graph == nil && h.savedGraph == nil {
		return nil, nil, fmt.Errorf("index not initialized")
	}

	var neighbors []hnsw.Node[string]
	if h.savedGraph != nil {
		neighbors = h.savedGraph.Search(query, k)
	} else {
		neighbors = h.graph.Search(query, k)
	}

	ids := make([]string, len(neighbors))
	distances := make([]float64, len(neighbors))

	for i, n := range neighbors {
		ids[i] = n.Key
		// Compute actual cosine distance for the result
		if emb, ok := h.idToEmb[n.Key]; ok && len(emb.Embedding) > 0 {
			distances[i] = float64(CosineDistance(query, emb.Embedding))
		}
	}

	return ids, distances, nil
}

// GetEmbedding returns the embedding for a given photo UID
func (h *HNSWEmbeddingIndex) GetEmbedding(photoUID string) *StoredEmbedding {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.idToEmb[photoUID]
}

// SetPath sets the path for saving/loading the index
func (h *HNSWEmbeddingIndex) SetPath(path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.path = path
}

// Save persists the index to disk
func (h *HNSWEmbeddingIndex) Save() error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.path == "" {
		return nil // No path set
	}

	if h.graph == nil {
		// Remove existing file if index is empty
		os.Remove(h.path)
		return nil
	}

	// Write to file
	f, err := os.Create(h.path)
	if err != nil {
		return fmt.Errorf("failed to create HNSW embedding index file: %w", err)
	}
	defer f.Close()

	return h.graph.Export(f)
}

// Load loads the index from disk
func (h *HNSWEmbeddingIndex) Load(path string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.path = path

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("index file not found: %s", path)
	}

	saved, err := hnsw.LoadSavedGraph[string](path)
	if err != nil {
		return fmt.Errorf("failed to load HNSW embedding index: %w", err)
	}

	h.savedGraph = saved
	return nil
}

// Count returns the number of indexed embeddings
func (h *HNSWEmbeddingIndex) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.idToEmb)
}

// IsEmpty returns true if the index has no graph data loaded.
// Note: idToEmb is populated separately by RebuildFromEmbeddings after loading.
func (h *HNSWEmbeddingIndex) IsEmpty() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.graph == nil && h.savedGraph == nil
}

// RebuildFromEmbeddings rebuilds the idToEmb map from embeddings
// Called after loading index from disk
func (h *HNSWEmbeddingIndex) RebuildFromEmbeddings(embeddings []StoredEmbedding) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.idToEmb = make(map[string]*StoredEmbedding, len(embeddings))
	for i := range embeddings {
		h.idToEmb[embeddings[i].PhotoUID] = &embeddings[i]
	}
}

// SearchWithDistance finds the k nearest neighbors with distance filtering
// Returns photo UIDs and their distances, filtered by maxDistance
func (h *HNSWEmbeddingIndex) SearchWithDistance(query []float32, k int, maxDistance float64) ([]string, []float64, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.graph == nil && h.savedGraph == nil {
		return nil, nil, fmt.Errorf("index not initialized")
	}

	// Search with more candidates for better recall after filtering
	searchK := k * HNSWSearchMultiplier
	if searchK < 100 {
		searchK = 100
	}

	var neighbors []hnsw.Node[string]
	if h.savedGraph != nil {
		neighbors = h.savedGraph.Search(query, searchK)
	} else {
		neighbors = h.graph.Search(query, searchK)
	}

	ids := make([]string, 0, k)
	distances := make([]float64, 0, k)

	for _, n := range neighbors {
		// Compute actual cosine distance for the result
		emb, ok := h.idToEmb[n.Key]
		if !ok || len(emb.Embedding) == 0 {
			continue
		}
		dist := float64(CosineDistance(query, emb.Embedding))
		if dist >= maxDistance {
			continue
		}
		ids = append(ids, n.Key)
		distances = append(distances, dist)
		if len(ids) >= k {
			break
		}
	}

	return ids, distances, nil
}

// HNSWEmbeddingIndexMetadata stores metadata for freshness checking
type HNSWEmbeddingIndexMetadata struct {
	EmbeddingCount int64 `json:"embedding_count"`
}

// LoadHNSWEmbeddingMetadata loads just the metadata file for staleness checking
func LoadHNSWEmbeddingMetadata(basePath string) (*HNSWEmbeddingIndexMetadata, error) {
	metaPath := basePath + ".meta"
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, err
	}
	var meta HNSWEmbeddingIndexMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// SaveWithEmbeddingMetadata saves the index and embedding metadata to disk
func (h *HNSWEmbeddingIndex) SaveWithEmbeddingMetadata(basePath string, metadata HNSWEmbeddingIndexMetadata) error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.graph == nil && h.savedGraph == nil {
		// Remove existing files if index is empty
		fmt.Printf("Embedding index save: no graph loaded, removing files\n")
		os.Remove(basePath)
		os.Remove(basePath + ".meta")
		os.Remove(basePath + ".embeddings")
		return nil
	}

	// Save HNSW graph - use savedGraph if available (loaded from disk), otherwise use graph (built fresh)
	f, err := os.Create(basePath)
	if err != nil {
		return fmt.Errorf("failed to create HNSW embedding index file: %w", err)
	}
	if h.savedGraph != nil {
		// SavedGraph embeds *Graph, so we can call Export on it
		if err := h.savedGraph.Export(f); err != nil {
			f.Close()
			return fmt.Errorf("failed to export HNSW graph from savedGraph: %w", err)
		}
	} else {
		if err := h.graph.Export(f); err != nil {
			f.Close()
			return fmt.Errorf("failed to export HNSW graph: %w", err)
		}
	}
	f.Close()

	// Save metadata
	metaPath := basePath + ".meta"
	metaData, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	// Save embedding data (for fast startup)
	embPath := basePath + ".embeddings"
	embFile, err := os.Create(embPath)
	if err != nil {
		return fmt.Errorf("failed to create embeddings file: %w", err)
	}
	defer embFile.Close()

	// Convert map to slice for gob encoding
	embeddings := make([]StoredEmbedding, 0, len(h.idToEmb))
	for _, emb := range h.idToEmb {
		embeddings = append(embeddings, *emb)
	}

	encoder := gob.NewEncoder(embFile)
	if err := encoder.Encode(embeddings); err != nil {
		return fmt.Errorf("failed to encode embeddings: %w", err)
	}

	return nil
}

// LoadWithEmbeddingMetadata loads the index and embedding metadata from disk
func (h *HNSWEmbeddingIndex) LoadWithEmbeddingMetadata(basePath string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.path = basePath

	// Load HNSW graph
	saved, err := hnsw.LoadSavedGraph[string](basePath)
	if err != nil {
		return fmt.Errorf("failed to load HNSW embedding index: %w", err)
	}
	h.savedGraph = saved

	// Try to load embedding metadata
	embPath := basePath + ".embeddings"
	embFile, err := os.Open(embPath)
	if err != nil {
		return fmt.Errorf("failed to open embeddings file: %w", err)
	}
	defer embFile.Close()

	var embeddings []StoredEmbedding
	decoder := gob.NewDecoder(embFile)
	if err := decoder.Decode(&embeddings); err != nil {
		return fmt.Errorf("failed to decode embeddings: %w", err)
	}

	// Rebuild idToEmb map
	h.idToEmb = make(map[string]*StoredEmbedding, len(embeddings))
	for i := range embeddings {
		h.idToEmb[embeddings[i].PhotoUID] = &embeddings[i]
	}

	return nil
}
