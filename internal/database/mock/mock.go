// Package mock provides mock implementations of database interfaces for testing.
package mock

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/facematch"
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
		for i := range faces {
			if faces[i].SubjectName == subjectName {
				results = append(results, faces[i])
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
		for i := range faces {
			results = append(results, faces[i])
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
		for i := range faces {
			results = append(results, faces[i])
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

// GetPhotoUIDsWithSubjectName returns photo UIDs from the given list that have a face assigned to the subject
func (m *MockFaceReader) GetPhotoUIDsWithSubjectName(ctx context.Context, photoUIDs []string, subjectName string) (map[string]bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	uidSet := make(map[string]struct{}, len(photoUIDs))
	for _, uid := range photoUIDs {
		uidSet[uid] = struct{}{}
	}

	normalizedInput := strings.ToLower(facematch.NormalizePersonName(subjectName))
	result := make(map[string]bool)
	for photoUID, faces := range m.faces {
		if _, ok := uidSet[photoUID]; !ok {
			continue
		}
		for _, face := range faces {
			if strings.ToLower(facematch.NormalizePersonName(face.SubjectName)) == normalizedInput {
				result[photoUID] = true
				break
			}
		}
	}
	return result, nil
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


// MockBookWriter is a mock implementation of database.BookWriter
type MockBookWriter struct {
	mu            sync.RWMutex
	books         map[string]*database.PhotoBook
	sections      map[string]*database.BookSection
	sectionPhotos map[string][]database.SectionPhoto // keyed by sectionID
	pages         map[string]*database.BookPage
	pageSlots     map[string][]database.PageSlot // keyed by pageID
	memberships   map[string][]database.PhotoBookMembership // keyed by photoUID

	bookCounter    int
	sectionCounter int
	pageCounter    int

	// Error injection
	ListBooksError             error
	GetBookError               error
	CreateBookError            error
	UpdateBookError            error
	DeleteBookError            error
	GetSectionsError           error
	CreateSectionError         error
	UpdateSectionError         error
	DeleteSectionError         error
	ReorderSectionsError       error
	GetSectionPhotosError      error
	CountSectionPhotosError    error
	AddSectionPhotosError      error
	RemoveSectionPhotosError   error
	UpdateSectionPhotoError    error
	GetPagesError              error
	GetPageError               error
	CreatePageError            error
	UpdatePageError            error
	DeletePageError            error
	ReorderPagesError          error
	GetPageSlotsError          error
	AssignSlotError            error
	ClearSlotError             error
	SwapSlotsError               error
	UpdateSlotCropError          error
	GetPhotoBookMembershipsError error
}

// NewMockBookWriter creates a new mock book writer
func NewMockBookWriter() *MockBookWriter {
	return &MockBookWriter{
		books:         make(map[string]*database.PhotoBook),
		sections:      make(map[string]*database.BookSection),
		sectionPhotos: make(map[string][]database.SectionPhoto),
		pages:         make(map[string]*database.BookPage),
		pageSlots:     make(map[string][]database.PageSlot),
		memberships:   make(map[string][]database.PhotoBookMembership),
	}
}

// AddBook adds a book to the mock store
func (m *MockBookWriter) AddBook(book database.PhotoBook) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.books[book.ID] = &book
}

// AddSection adds a section to the mock store
func (m *MockBookWriter) AddSection(section database.BookSection) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sections[section.ID] = &section
}

// AddPage adds a page to the mock store
func (m *MockBookWriter) AddPage(page database.BookPage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pages[page.ID] = &page
}

// SetPageSlots sets slots for a page
func (m *MockBookWriter) SetPageSlots(pageID string, slots []database.PageSlot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pageSlots[pageID] = slots
}

// SetSectionPhotos sets photos for a section
func (m *MockBookWriter) SetSectionPhotos(sectionID string, photos []database.SectionPhoto) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sectionPhotos[sectionID] = photos
}

// SetMemberships sets book memberships for a photo
func (m *MockBookWriter) SetMemberships(photoUID string, memberships []database.PhotoBookMembership) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memberships[photoUID] = memberships
}

func (m *MockBookWriter) ListBooks(ctx context.Context) ([]database.PhotoBook, error) {
	if m.ListBooksError != nil {
		return nil, m.ListBooksError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []database.PhotoBook
	for _, b := range m.books {
		result = append(result, *b)
	}
	return result, nil
}

func (m *MockBookWriter) ListBooksWithCounts(ctx context.Context) ([]database.PhotoBookWithCounts, error) {
	if m.ListBooksError != nil {
		return nil, m.ListBooksError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []database.PhotoBookWithCounts
	for _, b := range m.books {
		bwc := database.PhotoBookWithCounts{PhotoBook: *b}
		for _, s := range m.sections {
			if s.BookID == b.ID {
				bwc.SectionCount++
				bwc.PhotoCount += len(m.sectionPhotos[s.ID])
			}
		}
		for _, p := range m.pages {
			if p.BookID == b.ID {
				bwc.PageCount++
			}
		}
		result = append(result, bwc)
	}
	return result, nil
}

func (m *MockBookWriter) GetBook(ctx context.Context, id string) (*database.PhotoBook, error) {
	if m.GetBookError != nil {
		return nil, m.GetBookError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.books[id]
	if !ok {
		return nil, nil
	}
	return b, nil
}

func (m *MockBookWriter) CreateBook(ctx context.Context, book *database.PhotoBook) error {
	if m.CreateBookError != nil {
		return m.CreateBookError
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bookCounter++
	book.ID = fmt.Sprintf("book-%d", m.bookCounter)
	m.books[book.ID] = book
	return nil
}

func (m *MockBookWriter) UpdateBook(ctx context.Context, book *database.PhotoBook) error {
	if m.UpdateBookError != nil {
		return m.UpdateBookError
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.books[book.ID] = book
	return nil
}

func (m *MockBookWriter) DeleteBook(ctx context.Context, id string) error {
	if m.DeleteBookError != nil {
		return m.DeleteBookError
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.books, id)
	return nil
}

func (m *MockBookWriter) GetSections(ctx context.Context, bookID string) ([]database.BookSection, error) {
	if m.GetSectionsError != nil {
		return nil, m.GetSectionsError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []database.BookSection
	for _, s := range m.sections {
		if s.BookID == bookID {
			sec := *s
			// Compute PhotoCount from sectionPhotos
			sec.PhotoCount = len(m.sectionPhotos[sec.ID])
			result = append(result, sec)
		}
	}
	return result, nil
}

func (m *MockBookWriter) CreateSection(ctx context.Context, section *database.BookSection) error {
	if m.CreateSectionError != nil {
		return m.CreateSectionError
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sectionCounter++
	section.ID = fmt.Sprintf("section-%d", m.sectionCounter)
	m.sections[section.ID] = section
	return nil
}

func (m *MockBookWriter) UpdateSection(ctx context.Context, section *database.BookSection) error {
	if m.UpdateSectionError != nil {
		return m.UpdateSectionError
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.sections[section.ID]; ok {
		existing.Title = section.Title
	}
	return nil
}

func (m *MockBookWriter) DeleteSection(ctx context.Context, id string) error {
	if m.DeleteSectionError != nil {
		return m.DeleteSectionError
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sections, id)
	delete(m.sectionPhotos, id)
	return nil
}

func (m *MockBookWriter) ReorderSections(ctx context.Context, bookID string, sectionIDs []string) error {
	if m.ReorderSectionsError != nil {
		return m.ReorderSectionsError
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, id := range sectionIDs {
		if s, ok := m.sections[id]; ok {
			s.SortOrder = i
		}
	}
	return nil
}

func (m *MockBookWriter) GetSectionPhotos(ctx context.Context, sectionID string) ([]database.SectionPhoto, error) {
	if m.GetSectionPhotosError != nil {
		return nil, m.GetSectionPhotosError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sectionPhotos[sectionID], nil
}

func (m *MockBookWriter) CountSectionPhotos(ctx context.Context, sectionID string) (int, error) {
	if m.CountSectionPhotosError != nil {
		return 0, m.CountSectionPhotosError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sectionPhotos[sectionID]), nil
}

func (m *MockBookWriter) AddSectionPhotos(ctx context.Context, sectionID string, photoUIDs []string) error {
	if m.AddSectionPhotosError != nil {
		return m.AddSectionPhotosError
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, uid := range photoUIDs {
		m.sectionPhotos[sectionID] = append(m.sectionPhotos[sectionID], database.SectionPhoto{
			SectionID: sectionID,
			PhotoUID:  uid,
		})
	}
	return nil
}

func (m *MockBookWriter) RemoveSectionPhotos(ctx context.Context, sectionID string, photoUIDs []string) error {
	if m.RemoveSectionPhotosError != nil {
		return m.RemoveSectionPhotosError
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	uidSet := make(map[string]struct{}, len(photoUIDs))
	for _, uid := range photoUIDs {
		uidSet[uid] = struct{}{}
	}
	var remaining []database.SectionPhoto
	for _, p := range m.sectionPhotos[sectionID] {
		if _, remove := uidSet[p.PhotoUID]; !remove {
			remaining = append(remaining, p)
		}
	}
	m.sectionPhotos[sectionID] = remaining
	return nil
}

func (m *MockBookWriter) UpdateSectionPhoto(ctx context.Context, sectionID string, photoUID string, description string, note string) error {
	if m.UpdateSectionPhotoError != nil {
		return m.UpdateSectionPhotoError
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	photos := m.sectionPhotos[sectionID]
	for i := range photos {
		if photos[i].PhotoUID == photoUID {
			photos[i].Description = description
			photos[i].Note = note
			break
		}
	}
	m.sectionPhotos[sectionID] = photos
	return nil
}

func (m *MockBookWriter) GetPages(ctx context.Context, bookID string) ([]database.BookPage, error) {
	if m.GetPagesError != nil {
		return nil, m.GetPagesError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []database.BookPage
	for _, p := range m.pages {
		if p.BookID == bookID {
			page := *p
			page.Slots = m.pageSlots[page.ID]
			result = append(result, page)
		}
	}
	return result, nil
}

func (m *MockBookWriter) GetPage(ctx context.Context, pageID string) (*database.BookPage, error) {
	if m.GetPageError != nil {
		return nil, m.GetPageError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.pages[pageID]
	if !ok {
		return nil, nil
	}
	page := *p
	page.Slots = m.pageSlots[page.ID]
	return &page, nil
}

func (m *MockBookWriter) CreatePage(ctx context.Context, page *database.BookPage) error {
	if m.CreatePageError != nil {
		return m.CreatePageError
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pageCounter++
	page.ID = fmt.Sprintf("page-%d", m.pageCounter)
	m.pages[page.ID] = page
	return nil
}

func (m *MockBookWriter) UpdatePage(ctx context.Context, page *database.BookPage) error {
	if m.UpdatePageError != nil {
		return m.UpdatePageError
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pages[page.ID] = page
	return nil
}

func (m *MockBookWriter) DeletePage(ctx context.Context, id string) error {
	if m.DeletePageError != nil {
		return m.DeletePageError
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pages, id)
	delete(m.pageSlots, id)
	return nil
}

func (m *MockBookWriter) ReorderPages(ctx context.Context, bookID string, pageIDs []string) error {
	if m.ReorderPagesError != nil {
		return m.ReorderPagesError
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, id := range pageIDs {
		if p, ok := m.pages[id]; ok {
			p.SortOrder = i
		}
	}
	return nil
}

func (m *MockBookWriter) GetPageSlots(ctx context.Context, pageID string) ([]database.PageSlot, error) {
	if m.GetPageSlotsError != nil {
		return nil, m.GetPageSlotsError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pageSlots[pageID], nil
}

func (m *MockBookWriter) AssignSlot(ctx context.Context, pageID string, slotIndex int, photoUID string) error {
	if m.AssignSlotError != nil {
		return m.AssignSlotError
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	slots := m.pageSlots[pageID]
	for i := range slots {
		if slots[i].SlotIndex == slotIndex {
			slots[i].PhotoUID = photoUID
			slots[i].TextContent = ""
			slots[i].CropX = 0.5
			slots[i].CropY = 0.5
			slots[i].CropScale = 1.0
			m.pageSlots[pageID] = slots
			return nil
		}
	}
	m.pageSlots[pageID] = append(slots, database.PageSlot{SlotIndex: slotIndex, PhotoUID: photoUID, CropX: 0.5, CropY: 0.5, CropScale: 1.0})
	return nil
}

func (m *MockBookWriter) AssignTextSlot(ctx context.Context, pageID string, slotIndex int, textContent string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	slots := m.pageSlots[pageID]
	for i := range slots {
		if slots[i].SlotIndex == slotIndex {
			slots[i].PhotoUID = ""
			slots[i].TextContent = textContent
			m.pageSlots[pageID] = slots
			return nil
		}
	}
	m.pageSlots[pageID] = append(slots, database.PageSlot{SlotIndex: slotIndex, TextContent: textContent})
	return nil
}

func (m *MockBookWriter) ClearSlot(ctx context.Context, pageID string, slotIndex int) error {
	if m.ClearSlotError != nil {
		return m.ClearSlotError
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	slots := m.pageSlots[pageID]
	for i := range slots {
		if slots[i].SlotIndex == slotIndex {
			slots[i].PhotoUID = ""
			m.pageSlots[pageID] = slots
			return nil
		}
	}
	return nil
}

func (m *MockBookWriter) SwapSlots(ctx context.Context, pageID string, slotA int, slotB int) error {
	if m.SwapSlotsError != nil {
		return m.SwapSlotsError
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	slots := m.pageSlots[pageID]
	var idxA, idxB = -1, -1
	for i := range slots {
		if slots[i].SlotIndex == slotA {
			idxA = i
		}
		if slots[i].SlotIndex == slotB {
			idxB = i
		}
	}
	if idxA >= 0 && idxB >= 0 {
		slots[idxA].PhotoUID, slots[idxB].PhotoUID = slots[idxB].PhotoUID, slots[idxA].PhotoUID
		slots[idxA].TextContent, slots[idxB].TextContent = slots[idxB].TextContent, slots[idxA].TextContent
		slots[idxA].CropX, slots[idxB].CropX = slots[idxB].CropX, slots[idxA].CropX
		slots[idxA].CropY, slots[idxB].CropY = slots[idxB].CropY, slots[idxA].CropY
		slots[idxA].CropScale, slots[idxB].CropScale = slots[idxB].CropScale, slots[idxA].CropScale
		m.pageSlots[pageID] = slots
	}
	return nil
}

func (m *MockBookWriter) UpdateSlotCrop(ctx context.Context, pageID string, slotIndex int, cropX, cropY, cropScale float64) error {
	if m.UpdateSlotCropError != nil {
		return m.UpdateSlotCropError
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	slots := m.pageSlots[pageID]
	for i := range slots {
		if slots[i].SlotIndex == slotIndex {
			slots[i].CropX = cropX
			slots[i].CropY = cropY
			slots[i].CropScale = cropScale
			m.pageSlots[pageID] = slots
			return nil
		}
	}
	return nil
}

func (m *MockBookWriter) GetPhotoBookMemberships(ctx context.Context, photoUID string) ([]database.PhotoBookMembership, error) {
	if m.GetPhotoBookMembershipsError != nil {
		return nil, m.GetPhotoBookMembershipsError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.memberships[photoUID], nil
}

// Verify interface compliance
var _ database.EmbeddingReader = (*MockEmbeddingReader)(nil)
var _ database.EmbeddingWriter = (*MockEmbeddingWriter)(nil)
var _ database.FaceReader = (*MockFaceReader)(nil)
var _ database.FaceWriter = (*MockFaceWriter)(nil)
var _ database.BookWriter = (*MockBookWriter)(nil)
