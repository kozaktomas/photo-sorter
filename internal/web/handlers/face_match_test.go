package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/mock"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// createFacesHandlerWithMocks creates a FacesHandler with mock database dependencies
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

	// Should fail because no PhotoPrism client in context
	assertStatusCode(t, recorder, http.StatusInternalServerError)
}

func TestFacesHandler_Match_NoFacesForPerson(t *testing.T) {
	mockReader := mock.NewMockFaceReader()
	handler := createFacesHandlerWithMocks(mockReader, nil)

	// Set up mock server for PhotoPrism
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
	// Add some faces for the person
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

	// Set up mock server for PhotoPrism
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

	// Request without threshold - should use default 0.5
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
		name           string
		body           string
		expectStatus   int
		expectError    string
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
