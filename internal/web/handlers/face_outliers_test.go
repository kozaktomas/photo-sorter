package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/mock"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// errMockError is a reusable mock error for testing
var errMockError = errors.New("mock error")

func TestFacesHandler_FindOutliers_Success(t *testing.T) {
	server := setupMockPhotoPrismServer(t, nil)
	defer server.Close()

	pp := createPhotoPrismClient(t, server)

	mockReader := mock.NewMockFaceReader()
	// Add faces for "john-doe"
	embedding1 := make([]float32, 512)
	for i := range embedding1 {
		embedding1[i] = 0.1
	}
	embedding2 := make([]float32, 512)
	for i := range embedding2 {
		embedding2[i] = 0.1
	}
	// Outlier with different embedding
	outlierEmbedding := make([]float32, 512)
	for i := range outlierEmbedding {
		outlierEmbedding[i] = 0.9 // Very different
	}

	mockReader.AddFaces("photo1", []database.StoredFace{
		{
			PhotoUID:    "photo1",
			FaceIndex:   0,
			Embedding:   embedding1,
			BBox:        []float64{100, 100, 200, 200},
			DetScore:    0.95,
			SubjectName: "john-doe",
			SubjectUID:  "subj1",
			MarkerUID:   "marker1",
			PhotoWidth:  1920,
			PhotoHeight: 1080,
			Orientation: 1,
			FileUID:     "file1",
		},
	})
	mockReader.AddFaces("photo2", []database.StoredFace{
		{
			PhotoUID:    "photo2",
			FaceIndex:   0,
			Embedding:   embedding2,
			BBox:        []float64{100, 100, 200, 200},
			DetScore:    0.92,
			SubjectName: "john-doe",
			SubjectUID:  "subj1",
			MarkerUID:   "marker2",
			PhotoWidth:  1920,
			PhotoHeight: 1080,
			Orientation: 1,
			FileUID:     "file2",
		},
	})
	mockReader.AddFaces("photo3", []database.StoredFace{
		{
			PhotoUID:    "photo3",
			FaceIndex:   0,
			Embedding:   outlierEmbedding,
			BBox:        []float64{100, 100, 200, 200},
			DetScore:    0.88,
			SubjectName: "john-doe",
			SubjectUID:  "subj1",
			MarkerUID:   "marker3",
			PhotoWidth:  1920,
			PhotoHeight: 1080,
			Orientation: 1,
			FileUID:     "file3",
		},
	})

	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := &FacesHandler{
		config:         cfg,
		sessionManager: sm,
		faceReader:     mockReader,
	}

	body := bytes.NewBufferString(`{"person_name": "john-doe", "threshold": 0.0, "limit": 10}`)
	req := requestWithPhotoPrism(t, "POST", "/api/v1/faces/outliers", pp)
	req.Body = io.NopCloser(body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.FindOutliers(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var response OutlierResponse
	parseJSONResponse(t, recorder, &response)

	if response.Person != "john-doe" {
		t.Errorf("expected person 'john-doe', got '%s'", response.Person)
	}

	if response.TotalFaces != 3 {
		t.Errorf("expected total_faces 3, got %d", response.TotalFaces)
	}

	// Should have outliers sorted by distance (most suspicious first)
	if len(response.Outliers) == 0 {
		t.Error("expected at least one outlier")
	}
}

func TestFacesHandler_FindOutliers_MissingPersonName(t *testing.T) {
	mockReader := mock.NewMockFaceReader()
	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := &FacesHandler{
		config:         cfg,
		sessionManager: sm,
		faceReader:     mockReader,
	}

	body := bytes.NewBufferString(`{"person_name": "", "threshold": 0.1}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/outliers", body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.FindOutliers(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "person_name is required")
}

func TestFacesHandler_FindOutliers_InvalidJSON(t *testing.T) {
	mockReader := mock.NewMockFaceReader()
	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := &FacesHandler{
		config:         cfg,
		sessionManager: sm,
		faceReader:     mockReader,
	}

	body := bytes.NewBufferString(`{invalid json}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/outliers", body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.FindOutliers(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestFacesHandler_FindOutliers_NoFaceReader(t *testing.T) {
	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := &FacesHandler{
		config:         cfg,
		sessionManager: sm,
		faceReader:     nil, // No face reader
	}

	body := bytes.NewBufferString(`{"person_name": "john-doe"}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/outliers", body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.FindOutliers(recorder, req)

	assertStatusCode(t, recorder, http.StatusServiceUnavailable)
	assertJSONError(t, recorder, "face data not available")
}

func TestFacesHandler_FindOutliers_PersonNotFound(t *testing.T) {
	server := setupMockPhotoPrismServer(t, nil)
	defer server.Close()

	pp := createPhotoPrismClient(t, server)

	mockReader := mock.NewMockFaceReader()
	// No faces for "unknown-person"

	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := &FacesHandler{
		config:         cfg,
		sessionManager: sm,
		faceReader:     mockReader,
	}

	body := bytes.NewBufferString(`{"person_name": "unknown-person"}`)
	req := requestWithPhotoPrism(t, "POST", "/api/v1/faces/outliers", pp)
	req.Body = io.NopCloser(body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.FindOutliers(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var response OutlierResponse
	parseJSONResponse(t, recorder, &response)

	if response.Person != "unknown-person" {
		t.Errorf("expected person 'unknown-person', got '%s'", response.Person)
	}

	if response.TotalFaces != 0 {
		t.Errorf("expected total_faces 0, got %d", response.TotalFaces)
	}

	if len(response.Outliers) != 0 {
		t.Errorf("expected 0 outliers, got %d", len(response.Outliers))
	}
}

func TestFacesHandler_FindOutliers_DatabaseError(t *testing.T) {
	server := setupMockPhotoPrismServer(t, nil)
	defer server.Close()

	pp := createPhotoPrismClient(t, server)

	mockReader := mock.NewMockFaceReader()
	mockReader.GetFacesBySubjectError = errMockError

	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := &FacesHandler{
		config:         cfg,
		sessionManager: sm,
		faceReader:     mockReader,
	}

	body := bytes.NewBufferString(`{"person_name": "john-doe"}`)
	req := requestWithPhotoPrism(t, "POST", "/api/v1/faces/outliers", pp)
	req.Body = io.NopCloser(body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.FindOutliers(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to get faces for person")
}

func TestFacesHandler_FindOutliers_WithThreshold(t *testing.T) {
	server := setupMockPhotoPrismServer(t, nil)
	defer server.Close()

	pp := createPhotoPrismClient(t, server)

	mockReader := mock.NewMockFaceReader()
	// Add similar embeddings
	embedding := make([]float32, 512)
	for i := range embedding {
		embedding[i] = 0.5
	}

	mockReader.AddFaces("photo1", []database.StoredFace{
		{
			PhotoUID:    "photo1",
			FaceIndex:   0,
			Embedding:   embedding,
			BBox:        []float64{100, 100, 200, 200},
			SubjectName: "john-doe",
			SubjectUID:  "subj1",
			PhotoWidth:  1920,
			PhotoHeight: 1080,
		},
	})
	mockReader.AddFaces("photo2", []database.StoredFace{
		{
			PhotoUID:    "photo2",
			FaceIndex:   0,
			Embedding:   embedding, // Same embedding
			BBox:        []float64{100, 100, 200, 200},
			SubjectName: "john-doe",
			SubjectUID:  "subj1",
			PhotoWidth:  1920,
			PhotoHeight: 1080,
		},
	})

	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := &FacesHandler{
		config:         cfg,
		sessionManager: sm,
		faceReader:     mockReader,
	}

	// High threshold should filter out all faces (they're identical)
	body := bytes.NewBufferString(`{"person_name": "john-doe", "threshold": 0.5}`)
	req := requestWithPhotoPrism(t, "POST", "/api/v1/faces/outliers", pp)
	req.Body = io.NopCloser(body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.FindOutliers(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var response OutlierResponse
	parseJSONResponse(t, recorder, &response)

	// With identical embeddings and high threshold, no outliers should be found
	if len(response.Outliers) != 0 {
		t.Errorf("expected 0 outliers with high threshold, got %d", len(response.Outliers))
	}
}

func TestFacesHandler_FindOutliers_WithLimit(t *testing.T) {
	server := setupMockPhotoPrismServer(t, nil)
	defer server.Close()

	pp := createPhotoPrismClient(t, server)

	mockReader := mock.NewMockFaceReader()

	// Add multiple faces with different embeddings
	for i := 0; i < 10; i++ {
		embedding := make([]float32, 512)
		for j := range embedding {
			embedding[j] = float32(i) * 0.1
		}
		mockReader.AddFaces("photo"+string(rune('0'+i)), []database.StoredFace{
			{
				PhotoUID:    "photo" + string(rune('0'+i)),
				FaceIndex:   0,
				Embedding:   embedding,
				BBox:        []float64{100, 100, 200, 200},
				SubjectName: "john-doe",
				SubjectUID:  "subj1",
				PhotoWidth:  1920,
				PhotoHeight: 1080,
			},
		})
	}

	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := &FacesHandler{
		config:         cfg,
		sessionManager: sm,
		faceReader:     mockReader,
	}

	body := bytes.NewBufferString(`{"person_name": "john-doe", "threshold": 0.0, "limit": 3}`)
	req := requestWithPhotoPrism(t, "POST", "/api/v1/faces/outliers", pp)
	req.Body = io.NopCloser(body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.FindOutliers(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var response OutlierResponse
	parseJSONResponse(t, recorder, &response)

	if len(response.Outliers) > 3 {
		t.Errorf("expected at most 3 outliers (limit), got %d", len(response.Outliers))
	}
}

func TestFacesHandler_FindOutliers_MissingEmbeddings(t *testing.T) {
	server := setupMockPhotoPrismServer(t, nil)
	defer server.Close()

	pp := createPhotoPrismClient(t, server)

	mockReader := mock.NewMockFaceReader()

	// Add face with valid embedding
	embedding := make([]float32, 512)
	for i := range embedding {
		embedding[i] = 0.5
	}
	mockReader.AddFaces("photo1", []database.StoredFace{
		{
			PhotoUID:    "photo1",
			FaceIndex:   0,
			Embedding:   embedding,
			BBox:        []float64{100, 100, 200, 200},
			SubjectName: "john-doe",
			SubjectUID:  "subj1",
			PhotoWidth:  1920,
			PhotoHeight: 1080,
			MarkerUID:   "marker1",
		},
	})

	// Add face with empty embedding (missing)
	mockReader.AddFaces("photo2", []database.StoredFace{
		{
			PhotoUID:    "photo2",
			FaceIndex:   0,
			Embedding:   []float32{}, // Empty embedding
			BBox:        []float64{100, 100, 200, 200},
			SubjectName: "john-doe",
			SubjectUID:  "subj1",
			MarkerUID:   "marker2",
		},
	})

	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := &FacesHandler{
		config:         cfg,
		sessionManager: sm,
		faceReader:     mockReader,
	}

	body := bytes.NewBufferString(`{"person_name": "john-doe"}`)
	req := requestWithPhotoPrism(t, "POST", "/api/v1/faces/outliers", pp)
	req.Body = io.NopCloser(body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.FindOutliers(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var response OutlierResponse
	parseJSONResponse(t, recorder, &response)

	if response.TotalFaces != 1 {
		t.Errorf("expected total_faces 1 (excluding missing), got %d", response.TotalFaces)
	}

	if len(response.MissingEmbeddings) != 1 {
		t.Errorf("expected 1 missing embedding, got %d", len(response.MissingEmbeddings))
	}

	if len(response.MissingEmbeddings) > 0 {
		if response.MissingEmbeddings[0].DistFromCentroid != -1 {
			t.Errorf("expected dist_from_centroid -1 for missing, got %f", response.MissingEmbeddings[0].DistFromCentroid)
		}
	}
}

func TestOutlierRequest_JSON(t *testing.T) {
	jsonData := `{"person_name": "jane-doe", "threshold": 0.2, "limit": 50}`

	var req OutlierRequest
	if err := json.Unmarshal([]byte(jsonData), &req); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if req.PersonName != "jane-doe" {
		t.Errorf("expected person_name 'jane-doe', got '%s'", req.PersonName)
	}

	if req.Threshold != 0.2 {
		t.Errorf("expected threshold 0.2, got %f", req.Threshold)
	}

	if req.Limit != 50 {
		t.Errorf("expected limit 50, got %d", req.Limit)
	}
}

func TestOutlierResult_JSON(t *testing.T) {
	result := OutlierResult{
		PhotoUID:         "photo123",
		DistFromCentroid: 0.35,
		FaceIndex:        0,
		BBoxRel:          []float64{0.1, 0.1, 0.1, 0.15},
		FileUID:          "file123",
		MarkerUID:        "marker123",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed OutlierResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed.PhotoUID != "photo123" {
		t.Errorf("expected photo_uid 'photo123', got '%s'", parsed.PhotoUID)
	}

	if parsed.DistFromCentroid != 0.35 {
		t.Errorf("expected dist_from_centroid 0.35, got %f", parsed.DistFromCentroid)
	}

	if parsed.FaceIndex != 0 {
		t.Errorf("expected face_index 0, got %d", parsed.FaceIndex)
	}
}

func TestOutlierResponse_JSON(t *testing.T) {
	response := OutlierResponse{
		Person:      "john-doe",
		TotalFaces:  100,
		AvgDistance: 0.08,
		Outliers: []OutlierResult{
			{PhotoUID: "photo1", DistFromCentroid: 0.5},
		},
		MissingEmbeddings: []OutlierResult{
			{PhotoUID: "photo2", DistFromCentroid: -1},
		},
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed OutlierResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed.Person != "john-doe" {
		t.Errorf("expected person 'john-doe', got '%s'", parsed.Person)
	}

	if parsed.TotalFaces != 100 {
		t.Errorf("expected total_faces 100, got %d", parsed.TotalFaces)
	}

	if parsed.AvgDistance != 0.08 {
		t.Errorf("expected avg_distance 0.08, got %f", parsed.AvgDistance)
	}

	if len(parsed.Outliers) != 1 {
		t.Errorf("expected 1 outlier, got %d", len(parsed.Outliers))
	}

	if len(parsed.MissingEmbeddings) != 1 {
		t.Errorf("expected 1 missing embedding, got %d", len(parsed.MissingEmbeddings))
	}
}
