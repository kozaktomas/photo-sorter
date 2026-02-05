// Package mock provides mock implementations of database interfaces for testing.
package mock

import (
	"context"
	"sync"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// MockEmbeddingReader is a mock implementation of database.EmbeddingReader
type MockEmbeddingReader struct {
	mu         sync.RWMutex
	embeddings map[string]*database.StoredEmbedding

	// Error injection
	GetError                error
	HasError                error
	CountError              error
	FindSimilarError        error
	FindSimilarWDError      error
	GetUniquePhotoUIDsError error
}

// NewMockEmbeddingReader creates a new mock embedding reader
func NewMockEmbeddingReader() *MockEmbeddingReader {
	return &MockEmbeddingReader{
		embeddings: make(map[string]*database.StoredEmbedding),
	}
}

// AddEmbedding adds an embedding to the mock store
func (m *MockEmbeddingReader) AddEmbedding(emb database.StoredEmbedding) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.embeddings[emb.PhotoUID] = &emb
}

// Get retrieves an embedding by photo UID
func (m *MockEmbeddingReader) Get(ctx context.Context, photoUID string) (*database.StoredEmbedding, error) {
	if m.GetError != nil {
		return nil, m.GetError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.embeddings[photoUID], nil
}

// Has checks if an embedding exists
func (m *MockEmbeddingReader) Has(ctx context.Context, photoUID string) (bool, error) {
	if m.HasError != nil {
		return false, m.HasError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.embeddings[photoUID]
	return ok, nil
}

// Count returns the total number of embeddings
func (m *MockEmbeddingReader) Count(ctx context.Context) (int, error) {
	if m.CountError != nil {
		return 0, m.CountError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.embeddings), nil
}

// CountByUIDs returns the number of embeddings whose photo_uid is in the given list
func (m *MockEmbeddingReader) CountByUIDs(ctx context.Context, uids []string) (int, error) {
	if m.CountError != nil {
		return 0, m.CountError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	uidSet := make(map[string]struct{}, len(uids))
	for _, uid := range uids {
		uidSet[uid] = struct{}{}
	}
	count := 0
	for uid := range m.embeddings {
		if _, ok := uidSet[uid]; ok {
			count++
		}
	}
	return count, nil
}

// FindSimilar finds similar embeddings
func (m *MockEmbeddingReader) FindSimilar(ctx context.Context, embedding []float32, limit int) ([]database.StoredEmbedding, error) {
	if m.FindSimilarError != nil {
		return nil, m.FindSimilarError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []database.StoredEmbedding
	for _, emb := range m.embeddings {
		results = append(results, *emb)
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

// FindSimilarWithDistance finds similar embeddings with distances
func (m *MockEmbeddingReader) FindSimilarWithDistance(ctx context.Context, embedding []float32, limit int, maxDistance float64) ([]database.StoredEmbedding, []float64, error) {
	if m.FindSimilarWDError != nil {
		return nil, nil, m.FindSimilarWDError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []database.StoredEmbedding
	var distances []float64
	for _, emb := range m.embeddings {
		results = append(results, *emb)
		distances = append(distances, 0.1) // Mock distance
		if len(results) >= limit {
			break
		}
	}
	return results, distances, nil
}

// GetUniquePhotoUIDs returns all unique photo UIDs that have embeddings
func (m *MockEmbeddingReader) GetUniquePhotoUIDs(ctx context.Context) ([]string, error) {
	if m.GetUniquePhotoUIDsError != nil {
		return nil, m.GetUniquePhotoUIDsError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	var uids []string
	for uid := range m.embeddings {
		uids = append(uids, uid)
	}
	return uids, nil
}

// MockFaceReader is a mock implementation of database.FaceReader
type MockFaceReader struct {
	mu    sync.RWMutex
	faces map[string][]database.StoredFace // keyed by PhotoUID

	// Error injection
	GetFacesError          error
	GetFacesBySubjectError error
	HasFacesError          error
	IsFacesProcessedError  error
	CountError             error
	CountPhotosError       error
	FindSimilarError       error
	FindSimilarWDError     error
	GetUniquePhotoUIDsError error
}

// NewMockFaceReader creates a new mock face reader
func NewMockFaceReader() *MockFaceReader {
	return &MockFaceReader{
		faces: make(map[string][]database.StoredFace),
	}
}

// AddFaces adds faces for a photo
func (m *MockFaceReader) AddFaces(photoUID string, faces []database.StoredFace) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.faces[photoUID] = faces
}

// GetFaces retrieves all faces for a photo
func (m *MockFaceReader) GetFaces(ctx context.Context, photoUID string) ([]database.StoredFace, error) {
	if m.GetFacesError != nil {
		return nil, m.GetFacesError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.faces[photoUID], nil
}

// GetFacesBySubjectName retrieves all faces for a subject
func (m *MockFaceReader) GetFacesBySubjectName(ctx context.Context, subjectName string) ([]database.StoredFace, error) {
	if m.GetFacesBySubjectError != nil {
		return nil, m.GetFacesBySubjectError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []database.StoredFace
	for _, faces := range m.faces {
		for _, face := range faces {
			if face.SubjectName == subjectName {
				results = append(results, face)
			}
		}
	}
	return results, nil
}

// HasFaces checks if faces exist for a photo
func (m *MockFaceReader) HasFaces(ctx context.Context, photoUID string) (bool, error) {
	if m.HasFacesError != nil {
		return false, m.HasFacesError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	faces, ok := m.faces[photoUID]
	return ok && len(faces) > 0, nil
}

// IsFacesProcessed checks if face detection has been run
func (m *MockFaceReader) IsFacesProcessed(ctx context.Context, photoUID string) (bool, error) {
	if m.IsFacesProcessedError != nil {
		return false, m.IsFacesProcessedError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.faces[photoUID]
	return ok, nil
}

// Count returns the total number of faces
func (m *MockFaceReader) Count(ctx context.Context) (int, error) {
	if m.CountError != nil {
		return 0, m.CountError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, faces := range m.faces {
		count += len(faces)
	}
	return count, nil
}

// CountByUIDs returns the number of faces whose photo_uid is in the given list
func (m *MockFaceReader) CountByUIDs(ctx context.Context, uids []string) (int, error) {
	if m.CountError != nil {
		return 0, m.CountError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	uidSet := make(map[string]struct{}, len(uids))
	for _, uid := range uids {
		uidSet[uid] = struct{}{}
	}
	count := 0
	for uid, faces := range m.faces {
		if _, ok := uidSet[uid]; ok {
			count += len(faces)
		}
	}
	return count, nil
}

// CountPhotos returns the number of distinct photos with faces
func (m *MockFaceReader) CountPhotos(ctx context.Context) (int, error) {
	if m.CountPhotosError != nil {
		return 0, m.CountPhotosError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.faces), nil
}

// CountPhotosByUIDs returns the number of distinct photos with faces whose photo_uid is in the given list
func (m *MockFaceReader) CountPhotosByUIDs(ctx context.Context, uids []string) (int, error) {
	if m.CountPhotosError != nil {
		return 0, m.CountPhotosError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	uidSet := make(map[string]struct{}, len(uids))
	for _, uid := range uids {
		uidSet[uid] = struct{}{}
	}
	count := 0
	for uid := range m.faces {
		if _, ok := uidSet[uid]; ok {
			count++
		}
	}
	return count, nil
}

// FindSimilar finds similar faces
func (m *MockFaceReader) FindSimilar(ctx context.Context, embedding []float32, limit int) ([]database.StoredFace, error) {
	if m.FindSimilarError != nil {
		return nil, m.FindSimilarError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []database.StoredFace
	for _, faces := range m.faces {
		for _, face := range faces {
			results = append(results, face)
			if len(results) >= limit {
				return results, nil
			}
		}
	}
	return results, nil
}

// FindSimilarWithDistance finds similar faces with distances
func (m *MockFaceReader) FindSimilarWithDistance(ctx context.Context, embedding []float32, limit int, maxDistance float64) ([]database.StoredFace, []float64, error) {
	if m.FindSimilarWDError != nil {
		return nil, nil, m.FindSimilarWDError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []database.StoredFace
	var distances []float64
	for _, faces := range m.faces {
		for _, face := range faces {
			results = append(results, face)
			distances = append(distances, 0.1) // Mock distance
			if len(results) >= limit {
				return results, distances, nil
			}
		}
	}
	return results, distances, nil
}

// GetUniquePhotoUIDs returns all unique photo UIDs
func (m *MockFaceReader) GetUniquePhotoUIDs(ctx context.Context) ([]string, error) {
	if m.GetUniquePhotoUIDsError != nil {
		return nil, m.GetUniquePhotoUIDsError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	var uids []string
	for uid := range m.faces {
		uids = append(uids, uid)
	}
	return uids, nil
}

// GetFacesWithMarkerUID returns all faces that have a non-empty marker_uid
func (m *MockFaceReader) GetFacesWithMarkerUID(ctx context.Context) ([]database.StoredFace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []database.StoredFace
	for _, faces := range m.faces {
		for _, face := range faces {
			if face.MarkerUID != "" {
				result = append(result, face)
			}
		}
	}
	return result, nil
}

// MockFaceWriter is a mock implementation of database.FaceWriter
type MockFaceWriter struct {
	*MockFaceReader

	// Track calls
	SaveFacesCalls         []SaveFacesCall
	MarkProcessedCalls     []MarkProcessedCall
	UpdateMarkerCalls      []UpdateMarkerCall
	UpdatePhotoInfoCalls   []UpdatePhotoInfoCall
	DeleteFacesCalls       []string

	// Error injection
	SaveFacesError         error
	MarkProcessedError     error
	UpdateMarkerError      error
	UpdatePhotoInfoError   error
	DeleteFacesError       error
}

// SaveFacesCall tracks a SaveFaces call
type SaveFacesCall struct {
	PhotoUID string
	Faces    []database.StoredFace
}

// MarkProcessedCall tracks a MarkFacesProcessed call
type MarkProcessedCall struct {
	PhotoUID  string
	FaceCount int
}

// UpdateMarkerCall tracks an UpdateFaceMarker call
type UpdateMarkerCall struct {
	PhotoUID    string
	FaceIndex   int
	MarkerUID   string
	SubjectUID  string
	SubjectName string
}

// UpdatePhotoInfoCall tracks an UpdateFacePhotoInfo call
type UpdatePhotoInfoCall struct {
	PhotoUID    string
	Width       int
	Height      int
	Orientation int
	FileUID     string
}

// NewMockFaceWriter creates a new mock face writer
func NewMockFaceWriter() *MockFaceWriter {
	return &MockFaceWriter{
		MockFaceReader: NewMockFaceReader(),
	}
}

// SaveFaces stores faces for a photo
func (m *MockFaceWriter) SaveFaces(ctx context.Context, photoUID string, faces []database.StoredFace) error {
	if m.SaveFacesError != nil {
		return m.SaveFacesError
	}
	m.SaveFacesCalls = append(m.SaveFacesCalls, SaveFacesCall{PhotoUID: photoUID, Faces: faces})
	m.AddFaces(photoUID, faces)
	return nil
}

// MarkFacesProcessed marks a photo as processed
func (m *MockFaceWriter) MarkFacesProcessed(ctx context.Context, photoUID string, faceCount int) error {
	if m.MarkProcessedError != nil {
		return m.MarkProcessedError
	}
	m.MarkProcessedCalls = append(m.MarkProcessedCalls, MarkProcessedCall{PhotoUID: photoUID, FaceCount: faceCount})
	return nil
}

// UpdateFaceMarker updates marker data for a face
func (m *MockFaceWriter) UpdateFaceMarker(ctx context.Context, photoUID string, faceIndex int, markerUID, subjectUID, subjectName string) error {
	if m.UpdateMarkerError != nil {
		return m.UpdateMarkerError
	}
	m.UpdateMarkerCalls = append(m.UpdateMarkerCalls, UpdateMarkerCall{
		PhotoUID:    photoUID,
		FaceIndex:   faceIndex,
		MarkerUID:   markerUID,
		SubjectUID:  subjectUID,
		SubjectName: subjectName,
	})
	return nil
}

// UpdateFacePhotoInfo updates photo info for faces
func (m *MockFaceWriter) UpdateFacePhotoInfo(ctx context.Context, photoUID string, width, height, orientation int, fileUID string) error {
	if m.UpdatePhotoInfoError != nil {
		return m.UpdatePhotoInfoError
	}
	m.UpdatePhotoInfoCalls = append(m.UpdatePhotoInfoCalls, UpdatePhotoInfoCall{
		PhotoUID:    photoUID,
		Width:       width,
		Height:      height,
		Orientation: orientation,
		FileUID:     fileUID,
	})
	return nil
}

// DeleteFacesByPhoto removes all faces for a photo
func (m *MockFaceWriter) DeleteFacesByPhoto(ctx context.Context, photoUID string) ([]int64, error) {
	if m.DeleteFacesError != nil {
		return nil, m.DeleteFacesError
	}
	m.DeleteFacesCalls = append(m.DeleteFacesCalls, photoUID)
	m.mu.Lock()
	defer m.mu.Unlock()
	faces := m.faces[photoUID]
	var ids []int64
	for _, f := range faces {
		ids = append(ids, f.ID)
	}
	delete(m.faces, photoUID)
	return ids, nil
}

// MockEmbeddingWriter is a mock implementation of database.EmbeddingWriter
type MockEmbeddingWriter struct {
	*MockEmbeddingReader

	// Track calls
	DeleteEmbeddingCalls []string

	// Error injection
	DeleteEmbeddingError error
}

// NewMockEmbeddingWriter creates a new mock embedding writer
func NewMockEmbeddingWriter() *MockEmbeddingWriter {
	return &MockEmbeddingWriter{
		MockEmbeddingReader: NewMockEmbeddingReader(),
	}
}

// DeleteEmbedding removes an embedding for a photo
func (m *MockEmbeddingWriter) DeleteEmbedding(ctx context.Context, photoUID string) error {
	if m.DeleteEmbeddingError != nil {
		return m.DeleteEmbeddingError
	}
	m.DeleteEmbeddingCalls = append(m.DeleteEmbeddingCalls, photoUID)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.embeddings, photoUID)
	return nil
}


// Verify interface compliance
var _ database.EmbeddingReader = (*MockEmbeddingReader)(nil)
var _ database.EmbeddingWriter = (*MockEmbeddingWriter)(nil)
var _ database.FaceReader = (*MockFaceReader)(nil)
var _ database.FaceWriter = (*MockFaceWriter)(nil)
