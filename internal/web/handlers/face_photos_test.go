package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/mock"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

func TestFacesHandler_GetPhotoFaces_Success(t *testing.T) {
	// Setup mock PhotoPrism server
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos/photo123": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// Include markers in Files - GetPhotoMarkers extracts from Files[].Markers
			json.NewEncoder(w).Encode(map[string]interface{}{
				"UID":   "photo123",
				"Title": "Test Photo",
				"Files": []map[string]interface{}{
					{
						"UID":         "file123",
						"Primary":     true,
						"Width":       1920,
						"Height":      1080,
						"Orientation": 1,
						"Markers": []map[string]interface{}{
							{
								"UID":     "marker1",
								"Type":    "face",
								"Name":    "john-doe",
								"SubjUID": "subj1",
								"X":       0.1,
								"Y":       0.1,
								"W":       0.1,
								"H":       0.15,
							},
						},
					},
				},
			})
		},
		"/api/v1/subjects": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"UID":        "subj1",
					"Name":       "john-doe",
					"PhotoCount": 10,
				},
			})
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)

	// Setup mock face reader
	mockReader := mock.NewMockFaceReader()
	mockReader.AddFaces("photo123", []database.StoredFace{
		{
			PhotoUID:  "photo123",
			FaceIndex: 0,
			Embedding: make([]float32, 512),
			BBox:      []float64{192, 108, 384, 270}, // Roughly matches marker position
			DetScore:  0.95,
		},
	})

	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret")
	handler := &FacesHandler{
		config:         cfg,
		sessionManager: sm,
		faceReader:     mockReader,
	}

	req := requestWithPhotoPrism(t, "GET", "/api/v1/photos/photo123/faces", pp)
	req = requestWithChiParams(req, map[string]string{"uid": "photo123"})
	recorder := httptest.NewRecorder()

	handler.GetPhotoFaces(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var response PhotoFacesResponse
	parseJSONResponse(t, recorder, &response)

	if response.PhotoUID != "photo123" {
		t.Errorf("expected photo_uid 'photo123', got '%s'", response.PhotoUID)
	}

	if response.FileUID != "file123" {
		t.Errorf("expected file_uid 'file123', got '%s'", response.FileUID)
	}

	if response.Width != 1920 {
		t.Errorf("expected width 1920, got %d", response.Width)
	}

	if response.Height != 1080 {
		t.Errorf("expected height 1080, got %d", response.Height)
	}

	if len(response.Faces) == 0 {
		t.Error("expected at least one face")
	}
}

func TestFacesHandler_GetPhotoFaces_MissingPhotoUID(t *testing.T) {
	mockReader := mock.NewMockFaceReader()
	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret")
	handler := &FacesHandler{
		config:         cfg,
		sessionManager: sm,
		faceReader:     mockReader,
	}

	req := httptest.NewRequest("GET", "/api/v1/photos//faces", nil)
	req = requestWithChiParams(req, map[string]string{})
	recorder := httptest.NewRecorder()

	handler.GetPhotoFaces(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "photo_uid is required")
}

func TestFacesHandler_GetPhotoFaces_NoFaceReader(t *testing.T) {
	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret")
	handler := &FacesHandler{
		config:         cfg,
		sessionManager: sm,
		faceReader:     nil, // No face reader configured
	}

	req := httptest.NewRequest("GET", "/api/v1/photos/photo123/faces", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "photo123"})
	recorder := httptest.NewRecorder()

	handler.GetPhotoFaces(recorder, req)

	assertStatusCode(t, recorder, http.StatusServiceUnavailable)
	assertJSONError(t, recorder, "face data not available")
}

func TestFacesHandler_GetPhotoFaces_NoPhotoPrismClient(t *testing.T) {
	mockReader := mock.NewMockFaceReader()
	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret")
	handler := &FacesHandler{
		config:         cfg,
		sessionManager: sm,
		faceReader:     mockReader,
	}

	// Request without PhotoPrism client in context
	req := httptest.NewRequest("GET", "/api/v1/photos/photo123/faces", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "photo123"})
	recorder := httptest.NewRecorder()

	handler.GetPhotoFaces(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
}

func TestFacesHandler_GetPhotoFaces_DatabaseError(t *testing.T) {
	server := setupMockPhotoPrismServer(t, nil)
	defer server.Close()

	pp := createPhotoPrismClient(t, server)

	mockReader := mock.NewMockFaceReader()
	mockReader.GetFacesError = errMockError

	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret")
	handler := &FacesHandler{
		config:         cfg,
		sessionManager: sm,
		faceReader:     mockReader,
	}

	req := requestWithPhotoPrism(t, "GET", "/api/v1/photos/photo123/faces", pp)
	req = requestWithChiParams(req, map[string]string{"uid": "photo123"})
	recorder := httptest.NewRecorder()

	handler.GetPhotoFaces(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to get faces from database")
}

func TestFacesHandler_GetPhotoFaces_PhotoDetailsError(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos/photo123": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error": "not found"}`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)

	mockReader := mock.NewMockFaceReader()
	mockReader.AddFaces("photo123", []database.StoredFace{
		{
			PhotoUID:  "photo123",
			FaceIndex: 0,
			Embedding: make([]float32, 512),
			BBox:      []float64{100, 100, 200, 200},
			DetScore:  0.9,
		},
	})

	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret")
	handler := &FacesHandler{
		config:         cfg,
		sessionManager: sm,
		faceReader:     mockReader,
	}

	req := requestWithPhotoPrism(t, "GET", "/api/v1/photos/photo123/faces", pp)
	req = requestWithChiParams(req, map[string]string{"uid": "photo123"})
	recorder := httptest.NewRecorder()

	handler.GetPhotoFaces(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to get photo details")
}

func TestFacesHandler_GetPhotoFaces_NoFacesInPhoto(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos/photo123": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"UID":   "photo123",
				"Title": "Test Photo",
				"Files": []map[string]interface{}{
					{
						"UID":         "file123",
						"Primary":     true,
						"Width":       1920,
						"Height":      1080,
						"Orientation": 1,
					},
				},
			})
		},
		"/api/v1/photos/photo123/markers": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
		},
		"/api/v1/subjects": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)

	mockReader := mock.NewMockFaceReader()
	// No faces added

	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret")
	handler := &FacesHandler{
		config:         cfg,
		sessionManager: sm,
		faceReader:     mockReader,
	}

	req := requestWithPhotoPrism(t, "GET", "/api/v1/photos/photo123/faces", pp)
	req = requestWithChiParams(req, map[string]string{"uid": "photo123"})
	recorder := httptest.NewRecorder()

	handler.GetPhotoFaces(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var response PhotoFacesResponse
	parseJSONResponse(t, recorder, &response)

	if len(response.Faces) != 0 {
		t.Errorf("expected 0 faces, got %d", len(response.Faces))
	}

	if response.EmbeddingsCount != 0 {
		t.Errorf("expected embeddings_count 0, got %d", response.EmbeddingsCount)
	}

	if response.MarkersCount != 0 {
		t.Errorf("expected markers_count 0, got %d", response.MarkersCount)
	}
}

func TestFacesHandler_GetPhotoFaces_WithThresholdAndLimit(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos/photo123": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"UID":   "photo123",
				"Title": "Test Photo",
				"Files": []map[string]interface{}{
					{
						"UID":         "file123",
						"Primary":     true,
						"Width":       1920,
						"Height":      1080,
						"Orientation": 1,
					},
				},
			})
		},
		"/api/v1/photos/photo123/markers": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
		},
		"/api/v1/subjects": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)

	mockReader := mock.NewMockFaceReader()
	mockReader.AddFaces("photo123", []database.StoredFace{
		{
			PhotoUID:  "photo123",
			FaceIndex: 0,
			Embedding: make([]float32, 512),
			BBox:      []float64{100, 100, 200, 200},
			DetScore:  0.9,
		},
	})

	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret")
	handler := &FacesHandler{
		config:         cfg,
		sessionManager: sm,
		faceReader:     mockReader,
	}

	// Test with custom threshold and limit parameters
	req := requestWithPhotoPrism(t, "GET", "/api/v1/photos/photo123/faces?threshold=0.3&limit=5", pp)
	req = requestWithChiParams(req, map[string]string{"uid": "photo123"})
	recorder := httptest.NewRecorder()

	handler.GetPhotoFaces(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
}

func TestFacesHandler_GetPhotoFaces_UnmatchedMarkers(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos/photo123": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// Include markers in Files - GetPhotoMarkers extracts from Files[].Markers
			json.NewEncoder(w).Encode(map[string]interface{}{
				"UID":   "photo123",
				"Title": "Test Photo",
				"Files": []map[string]interface{}{
					{
						"UID":         "file123",
						"Primary":     true,
						"Width":       1920,
						"Height":      1080,
						"Orientation": 1,
						"Markers": []map[string]interface{}{
							{
								"UID":     "marker1",
								"Type":    "face",
								"Name":    "",
								"SubjUID": "",
								"X":       0.5,
								"Y":       0.5,
								"W":       0.1,
								"H":       0.15,
							},
						},
					},
				},
			})
		},
		"/api/v1/subjects": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)

	mockReader := mock.NewMockFaceReader()
	// No database faces, so marker should appear as unmatched

	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret")
	handler := &FacesHandler{
		config:         cfg,
		sessionManager: sm,
		faceReader:     mockReader,
	}

	req := requestWithPhotoPrism(t, "GET", "/api/v1/photos/photo123/faces", pp)
	req = requestWithChiParams(req, map[string]string{"uid": "photo123"})
	recorder := httptest.NewRecorder()

	handler.GetPhotoFaces(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var response PhotoFacesResponse
	parseJSONResponse(t, recorder, &response)

	// The markers count should show the PhotoPrism marker
	if response.MarkersCount != 1 {
		t.Errorf("expected markers_count 1, got %d", response.MarkersCount)
	}

	// Should have the unmatched marker with negative face_index
	if len(response.Faces) != 1 {
		t.Logf("Response: %+v", response)
		t.Errorf("expected 1 face (unmatched marker), got %d", len(response.Faces))
	}

	if len(response.Faces) > 0 {
		if response.Faces[0].FaceIndex >= 0 {
			t.Errorf("expected negative face_index for unmatched marker, got %d", response.Faces[0].FaceIndex)
		}
		if response.Faces[0].Action != ActionAssignPerson {
			t.Errorf("expected action 'assign_person' for unmatched marker, got '%s'", response.Faces[0].Action)
		}
	}
}

func TestPhotoFace_JSON(t *testing.T) {
	face := PhotoFace{
		FaceIndex:  0,
		BBox:       []float64{100, 100, 200, 200},
		BBoxRel:    []float64{0.1, 0.1, 0.1, 0.1},
		DetScore:   0.95,
		MarkerUID:  "marker1",
		MarkerName: "john-doe",
		Action:     ActionAlreadyDone,
		Suggestions: []FaceSuggestion{
			{
				PersonName: "jane-doe",
				PersonUID:  "subj2",
				Distance:   0.15,
				Confidence: 0.85,
				PhotoCount: 20,
			},
		},
	}

	data, err := json.Marshal(face)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed PhotoFace
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed.FaceIndex != 0 {
		t.Errorf("expected face_index 0, got %d", parsed.FaceIndex)
	}

	if parsed.DetScore != 0.95 {
		t.Errorf("expected det_score 0.95, got %f", parsed.DetScore)
	}

	if parsed.Action != ActionAlreadyDone {
		t.Errorf("expected action 'already_done', got '%s'", parsed.Action)
	}
}

func TestFaceSuggestion_JSON(t *testing.T) {
	suggestion := FaceSuggestion{
		PersonName: "john-doe",
		PersonUID:  "subj1",
		Distance:   0.12,
		Confidence: 0.88,
		PhotoCount: 50,
	}

	data, err := json.Marshal(suggestion)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed FaceSuggestion
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed.PersonName != "john-doe" {
		t.Errorf("expected person_name 'john-doe', got '%s'", parsed.PersonName)
	}

	if parsed.Distance != 0.12 {
		t.Errorf("expected distance 0.12, got %f", parsed.Distance)
	}

	if parsed.Confidence != 0.88 {
		t.Errorf("expected confidence 0.88, got %f", parsed.Confidence)
	}
}

func TestPhotoFacesResponse_JSON(t *testing.T) {
	response := PhotoFacesResponse{
		PhotoUID:        "photo123",
		FileUID:         "file123",
		Width:           1920,
		Height:          1080,
		Orientation:     1,
		EmbeddingsCount: 2,
		MarkersCount:    3,
		Faces:           []PhotoFace{},
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed PhotoFacesResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed.PhotoUID != "photo123" {
		t.Errorf("expected photo_uid 'photo123', got '%s'", parsed.PhotoUID)
	}

	if parsed.EmbeddingsCount != 2 {
		t.Errorf("expected embeddings_count 2, got %d", parsed.EmbeddingsCount)
	}

	if parsed.MarkersCount != 3 {
		t.Errorf("expected markers_count 3, got %d", parsed.MarkersCount)
	}
}
