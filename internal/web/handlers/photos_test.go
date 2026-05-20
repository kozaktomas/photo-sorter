package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/mock"
	"github.com/kozaktomas/photo-sorter/internal/storage"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// fakePhotoReader is an in-memory database.PhotoReader for handler tests.
// Filtering supports the subset of PhotoFilter exercised by the spec:
// search (ILIKE-ish), taken range, bbox, archived flag, and limit/offset.
// Sort respects the "newest" / "oldest" / "name" keys.
type fakePhotoReader struct {
	photos map[string]*database.Photo
	files  map[string][]database.PhotoFile
	err    error
}

func newFakePhotoReader() *fakePhotoReader {
	return &fakePhotoReader{
		photos: map[string]*database.Photo{},
		files:  map[string][]database.PhotoFile{},
	}
}

func (f *fakePhotoReader) add(p *database.Photo) {
	cp := *p
	f.photos[p.UID] = &cp
}

func (f *fakePhotoReader) addFile(file database.PhotoFile) {
	f.files[file.PhotoUID] = append(f.files[file.PhotoUID], file)
}

func (f *fakePhotoReader) GetPhoto(_ context.Context, uid string) (*database.Photo, error) {
	if f.err != nil {
		return nil, f.err
	}
	p, ok := f.photos[uid]
	if !ok {
		return nil, database.ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (f *fakePhotoReader) GetPhotoByHash(_ context.Context, hash string) (*database.Photo, error) {
	if f.err != nil {
		return nil, f.err
	}
	for _, p := range f.photos {
		if p.FileHash == hash {
			cp := *p
			return &cp, nil
		}
	}
	return nil, database.ErrNotFound
}

func (f *fakePhotoReader) ListPhotos(
	_ context.Context, filter database.PhotoFilter,
) ([]database.Photo, int, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	var out []database.Photo
	for _, p := range f.photos {
		if !fakePhotoMatches(p, filter) {
			continue
		}
		out = append(out, *p)
	}
	sortFakePhotos(out, filter.SortBy)
	total := len(out)
	limit := filter.Limit
	if limit == 0 {
		limit = 50
	}
	start := min(filter.Offset, total)
	end := min(start+limit, total)
	return out[start:end], total, nil
}

func (f *fakePhotoReader) ListPhotoFiles(
	_ context.Context, photoUID string,
) ([]database.PhotoFile, error) {
	if f.err != nil {
		return nil, f.err
	}
	files := f.files[photoUID]
	out := make([]database.PhotoFile, len(files))
	copy(out, files)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsPrimary != out[j].IsPrimary {
			return out[i].IsPrimary
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func fakePhotoMatches(p *database.Photo, filter database.PhotoFilter) bool {
	// archived: nil/false -> exclude archived; true -> only archived.
	if filter.Archived == nil || !*filter.Archived {
		if p.ArchivedAt != nil {
			return false
		}
	} else if p.ArchivedAt == nil {
		return false
	}
	if filter.Favorite != nil && p.Favorite != *filter.Favorite {
		return false
	}
	if filter.Private != nil && p.Private != *filter.Private {
		return false
	}
	if filter.TakenFrom != nil && (p.TakenAt == nil || p.TakenAt.Before(*filter.TakenFrom)) {
		return false
	}
	if filter.TakenTo != nil && (p.TakenAt == nil || p.TakenAt.After(*filter.TakenTo)) {
		return false
	}
	if filter.BBox != nil {
		if p.Lat == nil || p.Lng == nil {
			return false
		}
		if *p.Lat < filter.BBox.MinLat || *p.Lat > filter.BBox.MaxLat {
			return false
		}
		if *p.Lng < filter.BBox.MinLng || *p.Lng > filter.BBox.MaxLng {
			return false
		}
	}
	if s := strings.ToLower(filter.Search); s != "" {
		hay := strings.ToLower(p.Title + " " + p.Description + " " + p.FileName)
		if !strings.Contains(hay, s) {
			return false
		}
	}
	return true
}

func sortFakePhotos(photos []database.Photo, sortBy string) {
	keyOf := func(p database.Photo) time.Time {
		if p.TakenAt == nil {
			return time.Time{}
		}
		return *p.TakenAt
	}
	switch sortBy {
	case "oldest":
		sort.SliceStable(photos, func(i, j int) bool { return keyOf(photos[i]).Before(keyOf(photos[j])) })
	case "name":
		sort.SliceStable(photos, func(i, j int) bool { return photos[i].FileName < photos[j].FileName })
	default:
		sort.SliceStable(photos, func(i, j int) bool { return keyOf(photos[i]).After(keyOf(photos[j])) })
	}
}

// createPhotosHandlerForTest creates a PhotosHandler for testing without
// any native backends. Use it for endpoints that don't touch the native
// reader/store (Update, BatchAddLabels, FindSimilar, etc.).
func createPhotosHandlerForTest(cfg *config.Config) *PhotosHandler {
	return &PhotosHandler{
		config:          cfg,
		sessionManager:  nil,
		embeddingReader: nil,
	}
}

// createPhotosHandlerWithEmbeddings creates a PhotosHandler with a mock embedding reader.
func createPhotosHandlerWithEmbeddings(cfg *config.Config, reader database.EmbeddingReader) *PhotosHandler {
	return &PhotosHandler{
		config:          cfg,
		sessionManager:  nil,
		embeddingReader: reader,
	}
}

// createPhotosHandlerNative wires a PhotosHandler with the given native
// reader and storage so the GET endpoints can be exercised end-to-end.
func createPhotosHandlerNative(
	cfg *config.Config, reader database.PhotoReader, store *storage.Storage,
) *PhotosHandler {
	return &PhotosHandler{
		config: cfg,
		reader: reader,
		store:  store,
	}
}

// newTestStorage builds a Storage rooted under t.TempDir().
func newTestStorage(t *testing.T) *storage.Storage {
	t.Helper()
	root := t.TempDir()
	s, err := storage.New(filepath.Join(root, "originals"), filepath.Join(root, "cache"))
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return s
}

// writeThumbFile writes data to <cacheRoot>/thumb/<rel> where rel is derived
// from storage.ThumbRelPath(hash, size). Returns the absolute path.
func writeThumbFile(t *testing.T, s *storage.Storage, hash, size string, data []byte) string {
	t.Helper()
	rel, err := storage.ThumbRelPath(hash, size)
	if err != nil {
		t.Fatalf("ThumbRelPath: %v", err)
	}
	abs, err := s.AbsThumb(rel)
	if err != nil {
		t.Fatalf("AbsThumb: %v", err)
	}
	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		t.Fatalf("MkdirAll: %v", mkErr)
	}
	if writeErr := os.WriteFile(abs, data, 0o644); writeErr != nil {
		t.Fatalf("WriteFile: %v", writeErr)
	}
	return abs
}

// writeOriginalFile writes data to <originalsRoot>/<rel>. Returns the abs path.
func writeOriginalFile(t *testing.T, s *storage.Storage, rel string, data []byte) string {
	t.Helper()
	abs, err := s.AbsOriginal(rel)
	if err != nil {
		t.Fatalf("AbsOriginal: %v", err)
	}
	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		t.Fatalf("MkdirAll: %v", mkErr)
	}
	if writeErr := os.WriteFile(abs, data, 0o644); writeErr != nil {
		t.Fatalf("WriteFile: %v", writeErr)
	}
	return abs
}

// samplePhoto builds a minimal database.Photo for tests. Callers tweak the
// fields they care about before adding it to the reader.
func samplePhoto(uid, hash, title string, taken time.Time) *database.Photo {
	t := taken
	return &database.Photo{
		UID:        uid,
		FileHash:   hash,
		FilePath:   "2024/06/" + uid + ".jpg",
		FileName:   uid + ".jpg",
		FileSize:   1024,
		FileMime:   "image/jpeg",
		FileWidth:  1920,
		FileHeight: 1080,
		TakenAt:    &t,
		Title:      title,
	}
}

func TestPhotosHandler_List_Success(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("photo1", "hash1ffffff", "alpha", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)))
	reader.add(samplePhoto("photo2", "hash2ffffff", "bravo", time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC)))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	assertContentType(t, rec, "application/json")

	var resp PhotoListResponse
	parseJSONResponse(t, rec, &resp)
	if resp.Total != 2 {
		t.Errorf("Total = %d, want 2", resp.Total)
	}
	if len(resp.Photos) != 2 {
		t.Fatalf("Photos length = %d, want 2", len(resp.Photos))
	}
	// Default sort is newest first.
	if resp.Photos[0].UID != "photo2" {
		t.Errorf("expected photo2 first, got %s", resp.Photos[0].UID)
	}
}

func TestPhotosHandler_List_FilterByDateRange(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("older", "aaaaaa1234", "older", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)))
	reader.add(samplePhoto("middle", "bbbbbb1234", "middle", time.Date(2022, 6, 1, 0, 0, 0, 0, time.UTC)))
	reader.add(samplePhoto("newer", "cccccc1234", "newer", time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC)))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	url := "/api/v1/photos?taken_from=2021-01-01T00:00:00Z&taken_to=2023-01-01T00:00:00Z"
	req := httptest.NewRequestWithContext(context.Background(), "GET", url, nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var resp PhotoListResponse
	parseJSONResponse(t, rec, &resp)
	if resp.Total != 1 {
		t.Fatalf("Total = %d, want 1", resp.Total)
	}
	if resp.Photos[0].UID != "middle" {
		t.Errorf("expected middle, got %s", resp.Photos[0].UID)
	}
}

func TestPhotosHandler_List_FilterBySearch(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("p1", "111111aaaa", "alpine sunrise", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)))
	p2 := samplePhoto("p2", "222222aaaa", "beach day", time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC))
	reader.add(p2)
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos?q=alpine", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var resp PhotoListResponse
	parseJSONResponse(t, rec, &resp)
	if resp.Total != 1 || resp.Photos[0].UID != "p1" {
		t.Errorf("search filter wrong: total=%d photos=%+v", resp.Total, resp.Photos)
	}
}

func TestPhotosHandler_List_FilterByBBox(t *testing.T) {
	reader := newFakePhotoReader()
	lat1, lng1 := 50.0, 14.0 // Prague-ish
	lat2, lng2 := 48.0, 17.0 // Bratislava-ish
	in := samplePhoto("in", "iiiiii1111", "in", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	in.Lat, in.Lng = &lat1, &lng1
	out := samplePhoto("out", "ooooooo111", "out", time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC))
	out.Lat, out.Lng = &lat2, &lng2
	reader.add(in)
	reader.add(out)
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	url := "/api/v1/photos?min_lat=49&min_lng=13&max_lat=51&max_lng=15"
	req := httptest.NewRequestWithContext(context.Background(), "GET", url, nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var resp PhotoListResponse
	parseJSONResponse(t, rec, &resp)
	if resp.Total != 1 || resp.Photos[0].UID != "in" {
		t.Errorf("bbox filter wrong: total=%d photos=%+v", resp.Total, resp.Photos)
	}
}

func TestPhotosHandler_List_BBoxRequiresAllCorners(t *testing.T) {
	reader := newFakePhotoReader()
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos?min_lat=49&max_lat=51", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
}

func TestPhotosHandler_List_NoReader(t *testing.T) {
	h := createPhotosHandlerForTest(testConfig())
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	assertStatusCode(t, rec, http.StatusServiceUnavailable)
}

func TestPhotosHandler_Get_Success(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("photo123", "abcdef1234", "Hello", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/photo123", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "photo123"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var p PhotoResponse
	parseJSONResponse(t, rec, &p)
	if p.UID != "photo123" || p.Hash != "abcdef1234" {
		t.Errorf("photo mismatch: %+v", p)
	}
}

func TestPhotosHandler_Get_MissingUID(t *testing.T) {
	h := createPhotosHandlerNative(testConfig(), newFakePhotoReader(), newTestStorage(t))
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/", nil)
	req = requestWithChiParams(req, map[string]string{})
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "missing photo UID")
}

func TestPhotosHandler_Get_NotFound(t *testing.T) {
	h := createPhotosHandlerNative(testConfig(), newFakePhotoReader(), newTestStorage(t))
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/missing", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "missing"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	assertStatusCode(t, rec, http.StatusNotFound)
	assertJSONError(t, rec, "photo not found")
}

func TestPhotosHandler_Get_ArchivedHiddenByDefault(t *testing.T) {
	reader := newFakePhotoReader()
	p := samplePhoto("arch", "abcabc1234", "arch", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	now := time.Now()
	p.ArchivedAt = &now
	reader.add(p)
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/arch", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "arch"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	assertStatusCode(t, rec, http.StatusNotFound)
}

func TestPhotosHandler_Get_ArchivedWithFlag(t *testing.T) {
	reader := newFakePhotoReader()
	p := samplePhoto("arch", "abcabc1234", "arch", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	now := time.Now()
	p.ArchivedAt = &now
	reader.add(p)
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/arch?include_archived=true", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "arch"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	assertStatusCode(t, rec, http.StatusOK)
	var p2 PhotoResponse
	parseJSONResponse(t, rec, &p2)
	if p2.UID != "arch" {
		t.Errorf("expected arch, got %s", p2.UID)
	}
}

func TestPhotosHandler_Update_Success(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos/photo123": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "PUT" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"UID":         "photo123",
				"Title":       "Updated Title",
				"Description": "New description",
				"Type":        "image",
			})
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"title": "Updated Title", "description": "New description"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/photos/photo123", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)
	req = requestWithChiParams(req, map[string]string{"uid": "photo123"})

	recorder := httptest.NewRecorder()

	handler.Update(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var photo PhotoResponse
	parseJSONResponse(t, recorder, &photo)

	if photo.Title != "Updated Title" {
		t.Errorf("expected title 'Updated Title', got '%s'", photo.Title)
	}
}

func TestPhotosHandler_Update_MissingUID(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"title": "Updated"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/photos/", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{})

	recorder := httptest.NewRecorder()

	handler.Update(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "missing photo UID")
}

func TestPhotosHandler_Update_InvalidJSON(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{invalid json}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/photos/photo123", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "photo123"})

	recorder := httptest.NewRecorder()

	handler.Update(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestPhotosHandler_Thumbnail_Success(t *testing.T) {
	hash := "abcdef1234"
	store := newTestStorage(t)
	writeThumbFile(t, store, hash, "fit_1280", []byte("jpegbytes"))

	reader := newFakePhotoReader()
	reader.add(samplePhoto("photo123", hash, "p", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)))
	h := createPhotosHandlerNative(testConfig(), reader, store)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/photo123/thumb/fit_1280", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "photo123", "size": "fit_1280"})
	rec := httptest.NewRecorder()
	h.Thumbnail(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	if rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q, want immutable", cc)
	}
	if etag := rec.Header().Get("ETag"); etag != `"sha:`+hash+`:fit_1280"` {
		t.Errorf("ETag = %q", etag)
	}
	if rec.Body.String() != "jpegbytes" {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestPhotosHandler_Thumbnail_InvalidSize(t *testing.T) {
	h := createPhotosHandlerNative(testConfig(), newFakePhotoReader(), newTestStorage(t))
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/photo123/thumb/invalid_size", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "photo123", "size": "invalid_size"})
	rec := httptest.NewRecorder()
	h.Thumbnail(rec, req)
	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "invalid size")
}

func TestPhotosHandler_Thumbnail_MissingParams(t *testing.T) {
	h := createPhotosHandlerNative(testConfig(), newFakePhotoReader(), newTestStorage(t))
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos//thumb/", nil)
	req = requestWithChiParams(req, map[string]string{})
	rec := httptest.NewRecorder()
	h.Thumbnail(rec, req)
	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "missing photo UID or size")
}

func TestPhotosHandler_Thumbnail_PhotoNotFound(t *testing.T) {
	h := createPhotosHandlerNative(testConfig(), newFakePhotoReader(), newTestStorage(t))
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/missing/thumb/fit_1280", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "missing", "size": "fit_1280"})
	rec := httptest.NewRecorder()
	h.Thumbnail(rec, req)
	assertStatusCode(t, rec, http.StatusNotFound)
	assertJSONError(t, rec, "photo not found")
}

func TestPhotosHandler_Thumbnail_FileMissing(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("photo123", "ffffff1234", "p", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/photo123/thumb/fit_1280", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "photo123", "size": "fit_1280"})
	rec := httptest.NewRecorder()
	h.Thumbnail(rec, req)
	assertStatusCode(t, rec, http.StatusNotFound)
	assertJSONError(t, rec, "thumbnail not found")
}

func TestPhotosHandler_Download_FullResponse(t *testing.T) {
	store := newTestStorage(t)
	body := []byte("hello world this is the file content")
	relPath := "2024/06/photo.jpg"
	writeOriginalFile(t, store, relPath, body)

	reader := newFakePhotoReader()
	p := samplePhoto("photoDL", "downloadhash123", "p", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC))
	p.FilePath = relPath
	p.FileName = "photo.jpg"
	p.FileMime = "image/jpeg"
	reader.add(p)
	reader.addFile(database.PhotoFile{
		ID: 1, PhotoUID: "photoDL", FilePath: relPath, FileMime: "image/jpeg", IsPrimary: true,
	})
	h := createPhotosHandlerNative(testConfig(), reader, store)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/photoDL/download", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "photoDL"})
	rec := httptest.NewRecorder()
	h.Download(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	if rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, `filename="photo.jpg"`) {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if !bytes.Equal(rec.Body.Bytes(), body) {
		t.Errorf("body = %q want %q", rec.Body.String(), body)
	}
}

func TestPhotosHandler_Download_RangeResponse(t *testing.T) {
	store := newTestStorage(t)
	body := []byte("abcdefghijklmnopqrstuvwxyz0123456789")
	relPath := "2024/07/range.jpg"
	writeOriginalFile(t, store, relPath, body)

	reader := newFakePhotoReader()
	p := samplePhoto("rangeP", "rangehash456789", "p", time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC))
	p.FilePath = relPath
	p.FileName = "range.jpg"
	p.FileMime = "image/jpeg"
	reader.add(p)
	reader.addFile(database.PhotoFile{
		ID: 1, PhotoUID: "rangeP", FilePath: relPath, FileMime: "image/jpeg", IsPrimary: true,
	})
	h := createPhotosHandlerNative(testConfig(), reader, store)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/rangeP/download", nil)
	req.Header.Set("Range", "bytes=0-9")
	req = requestWithChiParams(req, map[string]string{"uid": "rangeP"})
	rec := httptest.NewRecorder()
	h.Download(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	if got, want := rec.Body.String(), string(body[:10]); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	if cr := rec.Header().Get("Content-Range"); !strings.HasPrefix(cr, "bytes 0-9/") {
		t.Errorf("Content-Range = %q", cr)
	}
}

func TestPhotosHandler_Download_NotFound(t *testing.T) {
	h := createPhotosHandlerNative(testConfig(), newFakePhotoReader(), newTestStorage(t))
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/missing/download", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "missing"})
	rec := httptest.NewRecorder()
	h.Download(rec, req)
	assertStatusCode(t, rec, http.StatusNotFound)
}

func TestPhotosHandler_Download_MissingUID(t *testing.T) {
	h := createPhotosHandlerNative(testConfig(), newFakePhotoReader(), newTestStorage(t))
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos//download", nil)
	req = requestWithChiParams(req, map[string]string{})
	rec := httptest.NewRecorder()
	h.Download(rec, req)
	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "missing photo UID")
}

func TestPhotosHandler_BatchAddLabels_Success(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos/photo1/label": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"UID": "photo1"}`))
		},
		"/api/v1/photos/photo2/label": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"UID": "photo2"}`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"photo_uids": ["photo1", "photo2"], "label": "vacation"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos/batch/labels", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.BatchAddLabels(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var response BatchAddLabelsResponse
	parseJSONResponse(t, recorder, &response)

	if response.Updated != 2 {
		t.Errorf("expected updated=2, got %d", response.Updated)
	}
}

func TestPhotosHandler_BatchAddLabels_MissingPhotoUIDs(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"label": "vacation"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos/batch/labels", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.BatchAddLabels(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "photo_uids is required")
}

func TestPhotosHandler_BatchAddLabels_MissingLabel(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"photo_uids": ["photo1"]}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos/batch/labels", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.BatchAddLabels(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "label is required")
}

func TestPhotosHandler_BatchAddLabels_InvalidJSON(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos/batch/labels", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.BatchAddLabels(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestPhotosHandler_FindSimilar_NoEmbedding(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"photo_uid": "photo123"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos/similar", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.FindSimilar(recorder, req)

	assertStatusCode(t, recorder, http.StatusServiceUnavailable)
	assertJSONError(t, recorder, "embeddings not available")
}

func TestPhotosHandler_FindSimilar_MissingPhotoUID(t *testing.T) {
	mockReader := mock.NewMockEmbeddingReader()
	handler := createPhotosHandlerWithEmbeddings(testConfig(), mockReader)

	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos/similar", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.FindSimilar(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "photo_uid is required")
}

func TestPhotosHandler_FindSimilar_PhotoNotFound(t *testing.T) {
	mockReader := mock.NewMockEmbeddingReader()
	handler := createPhotosHandlerWithEmbeddings(testConfig(), mockReader)

	body := bytes.NewBufferString(`{"photo_uid": "nonexistent"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos/similar", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.FindSimilar(recorder, req)

	assertStatusCode(t, recorder, http.StatusNotFound)
	assertJSONError(t, recorder, "no embedding found for this photo. Run 'photo info --embedding' first")
}

func TestPhotosHandler_FindSimilar_Success(t *testing.T) {
	mockReader := mock.NewMockEmbeddingReader()
	// Add source and similar embeddings.
	mockReader.AddEmbedding(database.StoredEmbedding{
		PhotoUID:  "photo1",
		Embedding: make([]float32, 768),
	})
	mockReader.AddEmbedding(database.StoredEmbedding{
		PhotoUID:  "photo2",
		Embedding: make([]float32, 768),
	})

	handler := createPhotosHandlerWithEmbeddings(testConfig(), mockReader)

	body := bytes.NewBufferString(`{"photo_uid": "photo1", "limit": 10, "threshold": 0.5}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos/similar", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.FindSimilar(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var response SimilarResponse
	parseJSONResponse(t, recorder, &response)

	if response.SourcePhotoUID != "photo1" {
		t.Errorf("expected source_photo_uid 'photo1', got '%s'", response.SourcePhotoUID)
	}
}

func TestPhotosHandler_FindSimilar_InvalidJSON(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos/similar", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.FindSimilar(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestPhotosHandler_SearchByText_EmptyText(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"text": ""}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos/search-by-text", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.SearchByText(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "text is required")
}

func TestPhotosHandler_SearchByText_WhitespaceText(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"text": "   "}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos/search-by-text", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.SearchByText(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "text is required")
}

func TestPhotosHandler_SearchByText_NoEmbeddings(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"text": "sunset beach"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos/search-by-text", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.SearchByText(recorder, req)

	assertStatusCode(t, recorder, http.StatusServiceUnavailable)
	assertJSONError(t, recorder, "embeddings not available")
}

func TestPhotosHandler_SearchByText_InvalidJSON(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos/search-by-text", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.SearchByText(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestPhotosHandler_FindSimilarToCollection_InvalidJSON(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos/similar-to-collection", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.FindSimilarToCollection(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestPhotosHandler_FindSimilarToCollection_MissingSourceType(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"source_id": "vacation"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos/similar-to-collection", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.FindSimilarToCollection(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "source_type is required")
}

func TestPhotosHandler_FindSimilarToCollection_MissingSourceID(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"source_type": "label"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos/similar-to-collection", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.FindSimilarToCollection(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "source_id is required")
}

func TestPhotosHandler_FindSimilarToCollection_InvalidSourceType(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"source_type": "invalid", "source_id": "test"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos/similar-to-collection", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.FindSimilarToCollection(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "source_type must be 'label' or 'album'")
}
