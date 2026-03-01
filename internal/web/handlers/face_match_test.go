package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/mock"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// createFacesHandlerWithMocks creates a FacesHandler with mock database dependencies.
func createFacesHandlerWithMocks(faceReader database.FaceReader, faceWriter database.FaceWriter) *FacesHandler {
	return &FacesHandler{
		config:         testConfig(),
		sessionManager: nil,
		faceReader:     faceReader,
		faceWriter:     faceWriter,
	}
}

func TestFacesHandler_Match_NoFaceReader(t *testing.T) {
	handler := createFacesHandlerWithMocks(nil, nil)

	body := bytes.NewBufferString(`{"person_name": "john-doe"}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/match", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.Match(recorder, req)

	assertStatusCode(t, recorder, http.StatusServiceUnavailable)
	assertJSONError(t, recorder, "face data not available")
}

func TestFacesHandler_Match_InvalidJSON(t *testing.T) {
	mockReader := mock.NewMockFaceReader()
	handler := createFacesHandlerWithMocks(mockReader, nil)

	body := bytes.NewBufferString(`{invalid json}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/match", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.Match(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestFacesHandler_Match_MissingPersonName(t *testing.T) {
	mockReader := mock.NewMockFaceReader()
	handler := createFacesHandlerWithMocks(mockReader, nil)

	body := bytes.NewBufferString(`{"threshold": 0.5}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/match", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.Match(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "person_name is required")
}

func TestFacesHandler_Match_EmptyPersonName(t *testing.T) {
	mockReader := mock.NewMockFaceReader()
	handler := createFacesHandlerWithMocks(mockReader, nil)

	body := bytes.NewBufferString(`{"person_name": ""}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/match", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.Match(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "person_name is required")
}

func TestFacesHandler_Match_NoPhotoPrismClient(t *testing.T) {
	mockReader := mock.NewMockFaceReader()
	handler := createFacesHandlerWithMocks(mockReader, nil)

	body := bytes.NewBufferString(`{"person_name": "john-doe"}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/match", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.Match(recorder, req)

	// Should fail because no PhotoPrism client in context.
	assertStatusCode(t, recorder, http.StatusInternalServerError)
}

func TestFacesHandler_Match_NoFacesForPerson(t *testing.T) {
	mockReader := mock.NewMockFaceReader()
	handler := createFacesHandlerWithMocks(mockReader, nil)

	// Set up mock server for PhotoPrism.
	server := setupMockPhotoPrismServer(t, nil)
	defer server.Close()

	pp := createPhotoPrismClient(t, server)

	body := bytes.NewBufferString(`{"person_name": "john-doe"}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/match", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.Match(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var response MatchResponse
	parseJSONResponse(t, recorder, &response)

	if response.Person != "john-doe" {
		t.Errorf("expected person 'john-doe', got '%s'", response.Person)
	}

	if response.SourceFaces != 0 {
		t.Errorf("expected source_faces=0, got %d", response.SourceFaces)
	}

	if len(response.Matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(response.Matches))
	}
}

func TestFacesHandler_Match_WithThresholdAndLimit(t *testing.T) {
	mockReader := mock.NewMockFaceReader()
	// Add some faces for the person.
	mockReader.AddFaces("photo1", []database.StoredFace{
		{
			ID:          1,
			PhotoUID:    "photo1",
			FaceIndex:   0,
			Embedding:   make([]float32, 512),
			BBox:        []float64{100, 100, 200, 200},
			DetScore:    0.95,
			SubjectName: "john-doe",
			SubjectUID:  "subj123",
			MarkerUID:   "marker123",
			PhotoWidth:  1000,
			PhotoHeight: 800,
			Orientation: 1,
			FileUID:     "file123",
		},
	})

	handler := createFacesHandlerWithMocks(mockReader, nil)

	// Set up mock server for PhotoPrism.
	server := setupMockPhotoPrismServer(t, nil)
	defer server.Close()

	pp := createPhotoPrismClient(t, server)

	body := bytes.NewBufferString(`{"person_name": "john-doe", "threshold": 0.3, "limit": 10}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/match", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.Match(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var response MatchResponse
	parseJSONResponse(t, recorder, &response)

	if response.Person != "john-doe" {
		t.Errorf("expected person 'john-doe', got '%s'", response.Person)
	}

	if response.SourceFaces != 1 {
		t.Errorf("expected source_faces=1, got %d", response.SourceFaces)
	}

	if response.SourcePhotos != 1 {
		t.Errorf("expected source_photos=1, got %d", response.SourcePhotos)
	}
}

func TestFacesHandler_Match_DefaultThreshold(t *testing.T) {
	mockReader := mock.NewMockFaceReader()
	handler := createFacesHandlerWithMocks(mockReader, nil)

	server := setupMockPhotoPrismServer(t, nil)
	defer server.Close()

	pp := createPhotoPrismClient(t, server)

	// Request without threshold - should use default 0.5.
	body := bytes.NewBufferString(`{"person_name": "john-doe"}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/match", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.Match(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
}

func TestFacesHandler_Match_GetFacesBySubjectError(t *testing.T) {
	mockReader := mock.NewMockFaceReader()
	mockReader.GetFacesBySubjectError = http.ErrAbortHandler

	handler := createFacesHandlerWithMocks(mockReader, nil)

	server := setupMockPhotoPrismServer(t, nil)
	defer server.Close()

	pp := createPhotoPrismClient(t, server)

	body := bytes.NewBufferString(`{"person_name": "john-doe"}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/match", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.Match(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to get faces for person")
}

func TestMatchRequest_Validation(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		expectStatus int
		expectError  string
	}{
		{
			name:         "valid request",
			body:         `{"person_name": "john-doe", "threshold": 0.5, "limit": 10}`,
			expectStatus: http.StatusOK,
		},
		{
			name:         "missing person_name",
			body:         `{"threshold": 0.5}`,
			expectStatus: http.StatusBadRequest,
			expectError:  "person_name is required",
		},
		{
			name:         "empty person_name",
			body:         `{"person_name": "", "threshold": 0.5}`,
			expectStatus: http.StatusBadRequest,
			expectError:  "person_name is required",
		},
		{
			name:         "zero threshold uses default",
			body:         `{"person_name": "john-doe", "threshold": 0}`,
			expectStatus: http.StatusOK,
		},
		{
			name:         "negative threshold uses default",
			body:         `{"person_name": "john-doe", "threshold": -1}`,
			expectStatus: http.StatusOK,
		},
		{
			name:         "zero limit means no limit",
			body:         `{"person_name": "john-doe", "limit": 0}`,
			expectStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockReader := mock.NewMockFaceReader()
			handler := createFacesHandlerWithMocks(mockReader, nil)

			server := setupMockPhotoPrismServer(t, nil)
			defer server.Close()

			pp := createPhotoPrismClient(t, server)

			body := bytes.NewBufferString(tc.body)
			req := httptest.NewRequest("POST", "/api/v1/faces/match", body)
			req.Header.Set("Content-Type", "application/json")
			ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
			req = req.WithContext(ctx)

			recorder := httptest.NewRecorder()

			handler.Match(recorder, req)

			assertStatusCode(t, recorder, tc.expectStatus)

			if tc.expectError != "" {
				assertJSONError(t, recorder, tc.expectError)
			}
		})
	}
}

func TestBuildMatchSourceData_FacesWithoutEmbedding(t *testing.T) {
	// Photo1 has an embedding, photo2 does NOT — both should be in sourcePhotoSet.
	allFaces := []database.StoredFace{
		{
			PhotoUID:    "photo1",
			FaceIndex:   0,
			Embedding:   make([]float32, 512),
			SubjectName: "John",
		},
		{
			PhotoUID:    "photo2",
			FaceIndex:   0,
			Embedding:   nil, // no embedding
			SubjectName: "John",
		},
	}

	sourceFaces, sourcePhotoSet := buildMatchSourceData(allFaces)

	// Both photos must be in sourcePhotoSet.
	if !sourcePhotoSet["photo1"] {
		t.Error("photo1 should be in sourcePhotoSet")
	}
	if !sourcePhotoSet["photo2"] {
		t.Error("photo2 should be in sourcePhotoSet (even without embedding)")
	}

	// Only photo1 should have a source face (it has an embedding).
	if len(sourceFaces) != 1 {
		t.Fatalf("expected 1 source face, got %d", len(sourceFaces))
	}
	if sourceFaces[0].PhotoUID != "photo1" {
		t.Errorf("expected source face from photo1, got %s", sourceFaces[0].PhotoUID)
	}
}

func TestMarkAlreadyAssignedPhotos(t *testing.T) {
	mockReader := mock.NewMockFaceReader()
	// Add a face on "candidate-photo" that is assigned to "John".
	mockReader.AddFaces("candidate-photo", []database.StoredFace{
		{
			PhotoUID:    "candidate-photo",
			FaceIndex:   0,
			Embedding:   make([]float32, 512),
			BBox:        []float64{10, 10, 50, 50},
			SubjectName: "John",
			SubjectUID:  "subj-john",
			MarkerUID:   "marker-1",
		},
	})
	// Add a face on "new-photo" with no assignment.
	mockReader.AddFaces("new-photo", []database.StoredFace{
		{
			PhotoUID:  "new-photo",
			FaceIndex: 0,
			Embedding: make([]float32, 512),
			BBox:      []float64{10, 10, 50, 50},
		},
	})

	matchMap := map[string]*matchCandidate{
		"candidate-photo": {
			PhotoUID:   "candidate-photo",
			Distance:   0.2,
			FaceIndex:  0,
			BBox:       []float64{10, 10, 50, 50},
			MatchCount: 1,
			// Stale HNSW data: no SubjectName/SubjectUID set.
		},
		"new-photo": {
			PhotoUID:   "new-photo",
			Distance:   0.3,
			FaceIndex:  0,
			BBox:       []float64{10, 10, 50, 50},
			MatchCount: 1,
		},
	}

	markAlreadyAssignedPhotos(context.Background(), mockReader, matchMap, "John")

	// candidate-photo should now have subject data set (already assigned).
	cp := matchMap["candidate-photo"]
	if cp.SubjectName == "" {
		t.Error("candidate-photo SubjectName should be set after markAlreadyAssignedPhotos")
	}
	if cp.SubjectUID == "" {
		t.Error("candidate-photo SubjectUID should be set after markAlreadyAssignedPhotos")
	}
	if cp.MarkerUID == "" {
		t.Error("candidate-photo MarkerUID should be set after markAlreadyAssignedPhotos")
	}

	// determineMatchAction should return AlreadyDone for candidate-photo.
	action, _, _ := determineMatchAction(cp)
	if action != ActionAlreadyDone {
		t.Errorf("expected ActionAlreadyDone for candidate-photo, got %s", action)
	}

	// new-photo should be unchanged (no assignment in DB).
	np := matchMap["new-photo"]
	if np.SubjectName != "" {
		t.Errorf("new-photo SubjectName should be empty, got %q", np.SubjectName)
	}

	// determineMatchAction should return CreateMarker for new-photo.
	action2, _, _ := determineMatchAction(np)
	if action2 != ActionCreateMarker {
		t.Errorf("expected ActionCreateMarker for new-photo, got %s", action2)
	}
}
