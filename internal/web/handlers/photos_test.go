package handlers

import (
	"bytes"
	"context"
	"errors"
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
	"github.com/kozaktomas/photo-sorter/internal/trash"
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

// ListArchivedBefore returns UIDs of archived photos whose ArchivedAt is
// strictly before cutoff. The fake walks the in-memory map so test cases can
// stage archived photos with explicit timestamps and assert the helper's
// selection logic.
func (f *fakePhotoReader) ListArchivedBefore(
	_ context.Context, cutoff time.Time,
) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	var uids []string
	for uid, p := range f.photos {
		if p.ArchivedAt != nil && p.ArchivedAt.Before(cutoff) {
			uids = append(uids, uid)
		}
	}
	sort.Strings(uids)
	return uids, nil
}

// CreatePhoto inserts a photo. Stub for PhotoWriter interface compliance.
func (f *fakePhotoReader) CreatePhoto(_ context.Context, p *database.Photo) error {
	if f.err != nil {
		return f.err
	}
	cp := *p
	now := time.Now()
	cp.CreatedAt = now
	cp.UpdatedAt = now
	f.photos[p.UID] = &cp
	p.CreatedAt = now
	p.UpdatedAt = now
	return nil
}

// UpdatePhoto overwrites the stored photo. Returns database.ErrNotFound
// when no photo with the supplied UID exists.
func (f *fakePhotoReader) UpdatePhoto(_ context.Context, p *database.Photo) error {
	if f.err != nil {
		return f.err
	}
	if _, ok := f.photos[p.UID]; !ok {
		return database.ErrNotFound
	}
	cp := *p
	cp.UpdatedAt = time.Now()
	f.photos[p.UID] = &cp
	p.UpdatedAt = cp.UpdatedAt
	return nil
}

// DeletePhoto removes the photo row. Returns database.ErrNotFound when no
// photo with the supplied UID exists.
func (f *fakePhotoReader) DeletePhoto(_ context.Context, uid string) error {
	if f.err != nil {
		return f.err
	}
	if _, ok := f.photos[uid]; !ok {
		return database.ErrNotFound
	}
	delete(f.photos, uid)
	delete(f.files, uid)
	return nil
}

// ArchivePhoto sets archived_at on the stored photo. Returns
// database.ErrNotFound when no photo with the supplied UID exists.
func (f *fakePhotoReader) ArchivePhoto(_ context.Context, uid string) error {
	if f.err != nil {
		return f.err
	}
	p, ok := f.photos[uid]
	if !ok {
		return database.ErrNotFound
	}
	now := time.Now()
	p.ArchivedAt = &now
	p.UpdatedAt = now
	return nil
}

// RestorePhoto clears archived_at. Returns database.ErrNotFound when no
// photo with the supplied UID exists.
func (f *fakePhotoReader) RestorePhoto(_ context.Context, uid string) error {
	if f.err != nil {
		return f.err
	}
	p, ok := f.photos[uid]
	if !ok {
		return database.ErrNotFound
	}
	p.ArchivedAt = nil
	p.UpdatedAt = time.Now()
	return nil
}

// AddPhotoFile appends a file row. Stub for PhotoWriter interface compliance.
func (f *fakePhotoReader) AddPhotoFile(_ context.Context, file *database.PhotoFile) error {
	if f.err != nil {
		return f.err
	}
	file.CreatedAt = time.Now()
	f.files[file.PhotoUID] = append(f.files[file.PhotoUID], *file)
	return nil
}

// DeletePhotoFile drops a file row by (photo_uid, file_path).
func (f *fakePhotoReader) DeletePhotoFile(_ context.Context, photoUID, filePath string) error {
	if f.err != nil {
		return f.err
	}
	list := f.files[photoUID]
	for i, file := range list {
		if file.FilePath == filePath {
			f.files[photoUID] = append(list[:i], list[i+1:]...)
			return nil
		}
	}
	return database.ErrNotFound
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
// repo and storage so the native endpoints can be exercised end-to-end.
func createPhotosHandlerNative(
	cfg *config.Config, repo database.PhotoWriter, store *storage.Storage,
) *PhotosHandler {
	return &PhotosHandler{
		config: cfg,
		repo:   repo,
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

// newUpdateRequest builds an authenticated PUT request for the Update
// handler. The session is intentionally empty-role so requireWriteRole
// admits the call.
func newUpdateRequest(t *testing.T, uid, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(
		context.Background(), "PUT", "/api/v1/photos/"+uid, bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")
	return requestWithChiParams(req, map[string]string{"uid": uid})
}

func TestPhotosHandler_Update_PartialUpdate(t *testing.T) {
	reader := newFakePhotoReader()
	original := samplePhoto("photo123", "abc123456789", "Old Title", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC))
	original.Description = "Old description"
	original.Notes = "Old notes"
	fav := true
	original.Favorite = fav
	reader.add(original)
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := newUpdateRequest(t, "photo123", `{"title": "New Title", "favorite": false}`)
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	assertContentType(t, rec, "application/json")
	var p PhotoResponse
	parseJSONResponse(t, rec, &p)
	if p.Title != "New Title" {
		t.Errorf("Title = %q, want %q", p.Title, "New Title")
	}
	if p.Description != "Old description" {
		t.Errorf("Description = %q, want unchanged", p.Description)
	}
	if p.Favorite {
		t.Errorf("Favorite = true, want false")
	}
	stored, _ := reader.GetPhoto(context.Background(), "photo123")
	if stored.Notes != "Old notes" {
		t.Errorf("Notes = %q, want unchanged 'Old notes'", stored.Notes)
	}
}

func TestPhotosHandler_Update_TakenAtAndLatLng(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("photo123", "abc123456789", "p", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	body := `{"taken_at": "1995-08-12T10:00:00Z", "lat": 50.08, "lng": 14.43}`
	req := newUpdateRequest(t, "photo123", body)
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	stored, _ := reader.GetPhoto(context.Background(), "photo123")
	if stored.TakenAt == nil || stored.TakenAt.Year() != 1995 {
		t.Errorf("TakenAt = %v, want 1995", stored.TakenAt)
	}
	if stored.Lat == nil || *stored.Lat != 50.08 {
		t.Errorf("Lat = %v, want 50.08", stored.Lat)
	}
	if stored.Lng == nil || *stored.Lng != 14.43 {
		t.Errorf("Lng = %v, want 14.43", stored.Lng)
	}
}

func TestPhotosHandler_Update_MissingUID(t *testing.T) {
	h := createPhotosHandlerNative(testConfig(), newFakePhotoReader(), newTestStorage(t))

	body := bytes.NewBufferString(`{"title": "Updated"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/photos/", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{})

	rec := httptest.NewRecorder()
	h.Update(rec, req)
	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "missing photo UID")
}

func TestPhotosHandler_Update_InvalidJSON(t *testing.T) {
	h := createPhotosHandlerNative(testConfig(), newFakePhotoReader(), newTestStorage(t))

	req := newUpdateRequest(t, "photo123", `{invalid json}`)
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "invalid request body")
}

func TestPhotosHandler_Update_BadTakenAt(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("photo123", "abc123456789", "p", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	tests := []struct {
		name    string
		body    string
		wantMsg string
	}{
		{"unparseable", `{"taken_at": "not-a-date"}`, "invalid taken_at"},
		{"too old", `{"taken_at": "1750-01-01T00:00:00Z"}`, "taken_at out of range"},
		{"too new", `{"taken_at": "2200-01-01T00:00:00Z"}`, "taken_at out of range"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := newUpdateRequest(t, "photo123", tc.body)
			rec := httptest.NewRecorder()
			h.Update(rec, req)
			assertStatusCode(t, rec, http.StatusBadRequest)
			assertJSONError(t, rec, tc.wantMsg)
		})
	}
}

func TestPhotosHandler_Update_BadLatLng(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("photo123", "abc123456789", "p", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	tests := []struct {
		name    string
		body    string
		wantMsg string
	}{
		{"lat without lng", `{"lat": 50.0}`, "lat and lng must be provided together"},
		{"lng without lat", `{"lng": 14.0}`, "lat and lng must be provided together"},
		{"lat too high", `{"lat": 95.0, "lng": 14.0}`, "lat out of range"},
		{"lng too low", `{"lat": 50.0, "lng": -200.0}`, "lng out of range"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := newUpdateRequest(t, "photo123", tc.body)
			rec := httptest.NewRecorder()
			h.Update(rec, req)
			assertStatusCode(t, rec, http.StatusBadRequest)
			assertJSONError(t, rec, tc.wantMsg)
		})
	}
}

func TestPhotosHandler_Update_TitleTooLong(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("photo123", "abc123456789", "p", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	longTitle := strings.Repeat("x", titleMaxLen+1)
	body := `{"title": "` + longTitle + `"}`
	req := newUpdateRequest(t, "photo123", body)
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "title too long")
}

func TestPhotosHandler_Update_NotFound(t *testing.T) {
	h := createPhotosHandlerNative(testConfig(), newFakePhotoReader(), newTestStorage(t))
	req := newUpdateRequest(t, "missing", `{"title": "x"}`)
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	assertStatusCode(t, rec, http.StatusNotFound)
	assertJSONError(t, rec, "photo not found")
}

func TestPhotosHandler_Update_ArchivedNotFound(t *testing.T) {
	reader := newFakePhotoReader()
	p := samplePhoto("arch", "abc123456789", "p", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC))
	now := time.Now()
	p.ArchivedAt = &now
	reader.add(p)
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := newUpdateRequest(t, "arch", `{"title": "x"}`)
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	assertStatusCode(t, rec, http.StatusNotFound)
}

func TestPhotosHandler_Update_NoRepo(t *testing.T) {
	h := createPhotosHandlerForTest(testConfig())
	req := newUpdateRequest(t, "photo123", `{"title": "x"}`)
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	assertStatusCode(t, rec, http.StatusServiceUnavailable)
}

func TestPhotosHandler_Update_ViewerForbidden(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("photo123", "abc123456789", "p", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := newUpdateRequest(t, "photo123", `{"title": "x"}`)
	ctx := middleware.SetSessionInContext(req.Context(), &middleware.Session{Role: "viewer"})
	req = req.WithContext(ctx)
	req = requestWithChiParams(req, map[string]string{"uid": "photo123"})

	rec := httptest.NewRecorder()
	h.Update(rec, req)
	assertStatusCode(t, rec, http.StatusForbidden)
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
	labelRepo := newFakeLabelRepo()
	handler := createPhotosHandlerForTest(testConfig())
	handler.labels = labelRepo

	body := bytes.NewBufferString(`{"photo_uids": ["photo1", "photo2"], "label": "vacation"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos/batch/labels", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.BatchAddLabels(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var response BatchAddLabelsResponse
	parseJSONResponse(t, recorder, &response)

	if response.Updated != 2 {
		t.Errorf("expected updated=2, got %d", response.Updated)
	}
	// EnsureLabel should have created one label row and AddPhotoLabel
	// should have attached both photos to it.
	if len(labelRepo.labels) != 1 {
		t.Errorf("expected exactly 1 label created, got %d", len(labelRepo.labels))
	}
	for _, members := range labelRepo.members {
		if len(members) != 2 {
			t.Errorf("expected 2 members, got %d", len(members))
		}
	}
}

func TestPhotosHandler_BatchAddLabels_NoRepo(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"photo_uids": ["photo1"], "label": "vacation"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos/batch/labels", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.BatchAddLabels(recorder, req)

	assertStatusCode(t, recorder, http.StatusServiceUnavailable)
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

// newBatchRequest builds an authenticated POST request for the batch
// handlers. The session is intentionally empty-role so requireWriteRole
// admits the call.
func newBatchRequest(t *testing.T, path, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(
		context.Background(), "POST", path, bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestPhotosHandler_BatchEdit_Success(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("p1", "h1ffffffff00", "p1", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)))
	reader.add(samplePhoto("p2", "h2ffffffff00", "p2", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := newBatchRequest(t, "/api/v1/photos/batch/edit",
		`{"photo_uids": ["p1", "p2", "missing"], "favorite": true}`)
	rec := httptest.NewRecorder()
	h.BatchEdit(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var resp BatchResponse
	parseJSONResponse(t, rec, &resp)
	if resp.Updated != 2 {
		t.Errorf("Updated = %d, want 2", resp.Updated)
	}
	if len(resp.Errors) != 1 || resp.Errors[0].PhotoUID != "missing" {
		t.Errorf("Errors = %+v, want one entry for 'missing'", resp.Errors)
	}
	stored, _ := reader.GetPhoto(context.Background(), "p1")
	if !stored.Favorite {
		t.Errorf("p1 not flipped to favorite")
	}
}

func TestPhotosHandler_BatchEdit_RequiresField(t *testing.T) {
	h := createPhotosHandlerNative(testConfig(), newFakePhotoReader(), newTestStorage(t))
	req := newBatchRequest(t, "/api/v1/photos/batch/edit", `{"photo_uids": ["p1"]}`)
	rec := httptest.NewRecorder()
	h.BatchEdit(rec, req)
	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "at least one field (favorite, private) is required")
}

func TestPhotosHandler_BatchEdit_MissingUIDs(t *testing.T) {
	h := createPhotosHandlerNative(testConfig(), newFakePhotoReader(), newTestStorage(t))
	req := newBatchRequest(t, "/api/v1/photos/batch/edit", `{"favorite": true}`)
	rec := httptest.NewRecorder()
	h.BatchEdit(rec, req)
	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "photo_uids is required")
}

func TestPhotosHandler_BatchEdit_ViewerForbidden(t *testing.T) {
	h := createPhotosHandlerNative(testConfig(), newFakePhotoReader(), newTestStorage(t))
	req := newBatchRequest(t, "/api/v1/photos/batch/edit",
		`{"photo_uids": ["p1"], "favorite": true}`)
	ctx := middleware.SetSessionInContext(req.Context(), &middleware.Session{Role: "viewer"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.BatchEdit(rec, req)
	assertStatusCode(t, rec, http.StatusForbidden)
}

func TestPhotosHandler_BatchArchiveRestore_Roundtrip(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("p1", "h1ffffffff00", "p1", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)))
	reader.add(samplePhoto("p2", "h2ffffffff00", "p2", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	// Archive both.
	req := newBatchRequest(t, "/api/v1/photos/batch/archive",
		`{"photo_uids": ["p1", "p2"]}`)
	rec := httptest.NewRecorder()
	h.BatchArchive(rec, req)
	assertStatusCode(t, rec, http.StatusOK)
	var archResp BatchResponse
	parseJSONResponse(t, rec, &archResp)
	if archResp.Updated != 2 {
		t.Fatalf("archive Updated = %d, want 2", archResp.Updated)
	}
	for _, uid := range []string{"p1", "p2"} {
		stored, _ := reader.GetPhoto(context.Background(), uid)
		if stored.ArchivedAt == nil {
			t.Errorf("%s should be archived", uid)
		}
	}

	// Restore them.
	req = newBatchRequest(t, "/api/v1/photos/batch/restore",
		`{"photo_uids": ["p1", "p2"]}`)
	rec = httptest.NewRecorder()
	h.BatchRestore(rec, req)
	assertStatusCode(t, rec, http.StatusOK)
	var restResp BatchResponse
	parseJSONResponse(t, rec, &restResp)
	if restResp.Updated != 2 {
		t.Fatalf("restore Updated = %d, want 2", restResp.Updated)
	}
	for _, uid := range []string{"p1", "p2"} {
		stored, _ := reader.GetPhoto(context.Background(), uid)
		if stored.ArchivedAt != nil {
			t.Errorf("%s should be restored", uid)
		}
	}
}

func TestPhotosHandler_BatchArchive_ContinuesOnError(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("p1", "h1ffffffff00", "p1", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := newBatchRequest(t, "/api/v1/photos/batch/archive",
		`{"photo_uids": ["p1", "missing", "alsoMissing"]}`)
	rec := httptest.NewRecorder()
	h.BatchArchive(rec, req)
	assertStatusCode(t, rec, http.StatusOK)
	var resp BatchResponse
	parseJSONResponse(t, rec, &resp)
	if resp.Updated != 1 {
		t.Errorf("Updated = %d, want 1", resp.Updated)
	}
	if len(resp.Errors) != 2 {
		t.Errorf("Errors length = %d, want 2", len(resp.Errors))
	}
}

func TestPhotosHandler_BatchArchive_MissingUIDs(t *testing.T) {
	h := createPhotosHandlerNative(testConfig(), newFakePhotoReader(), newTestStorage(t))
	req := newBatchRequest(t, "/api/v1/photos/batch/archive", `{}`)
	rec := httptest.NewRecorder()
	h.BatchArchive(rec, req)
	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "photo_uids is required")
}

func TestPhotosHandler_BatchRestore_ViewerForbidden(t *testing.T) {
	h := createPhotosHandlerNative(testConfig(), newFakePhotoReader(), newTestStorage(t))
	req := newBatchRequest(t, "/api/v1/photos/batch/restore", `{"photo_uids": ["p1"]}`)
	ctx := middleware.SetSessionInContext(req.Context(), &middleware.Session{Role: "viewer"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.BatchRestore(rec, req)
	assertStatusCode(t, rec, http.StatusForbidden)
}

// archivePhoto marks the given UID as archived in the fake reader. Used by
// the trash tests to set up the precondition for BatchPurge / ListTrash.
func archivePhoto(t *testing.T, r *fakePhotoReader, uid string, archivedAt time.Time) {
	t.Helper()
	if err := r.ArchivePhoto(context.Background(), uid); err != nil {
		t.Fatalf("archive %s: %v", uid, err)
	}
	r.photos[uid].ArchivedAt = &archivedAt
}

// createPhotosHandlerWithTrash wires a PhotosHandler with the given native
// repo + storage and a fully-functional trash.Store backed by the mock
// embedding / face writers, so the BatchPurge endpoint can be exercised
// end-to-end.
func createPhotosHandlerWithTrash(
	t *testing.T, cfg *config.Config, repo *fakePhotoReader, store *storage.Storage,
) (*PhotosHandler, *mock.MockEmbeddingWriter, *mock.MockFaceWriter) {
	t.Helper()
	embs := mock.NewMockEmbeddingWriter()
	faces := mock.NewMockFaceWriter()
	h := &PhotosHandler{
		config: cfg,
		repo:   repo,
		store:  store,
		trashStore: &trash.Store{
			Photos:     repo,
			Embeddings: embs,
			Faces:      faces,
			Files:      store,
		},
	}
	return h, embs, faces
}

func TestPhotosHandler_ListTrash_OnlyArchived(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("live", "h1ffffffff00", "live", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)))
	reader.add(samplePhoto("arch", "h2ffffffff00", "arch", time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)))
	archivePhoto(t, reader, "arch", time.Now())
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/trash", nil)
	rec := httptest.NewRecorder()
	h.ListTrash(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var resp PhotoListResponse
	parseJSONResponse(t, rec, &resp)
	if resp.Total != 1 {
		t.Fatalf("Total = %d, want 1", resp.Total)
	}
	if resp.Photos[0].UID != "arch" {
		t.Errorf("expected arch in trash, got %s", resp.Photos[0].UID)
	}
}

func TestPhotosHandler_ListTrash_OverridesArchivedQueryParam(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("live", "h1ffffffff00", "live", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)))
	reader.add(samplePhoto("arch", "h2ffffffff00", "arch", time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)))
	archivePhoto(t, reader, "arch", time.Now())
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	// ?archived=false is intentionally ignored — /photos/trash always lists
	// archived rows.
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/trash?archived=false", nil)
	rec := httptest.NewRecorder()
	h.ListTrash(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var resp PhotoListResponse
	parseJSONResponse(t, rec, &resp)
	if resp.Total != 1 || resp.Photos[0].UID != "arch" {
		t.Errorf("trash list ignored archived=false override: total=%d photos=%+v", resp.Total, resp.Photos)
	}
}

func TestPhotosHandler_ListTrash_NoReader(t *testing.T) {
	h := createPhotosHandlerForTest(testConfig())
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/trash", nil)
	rec := httptest.NewRecorder()
	h.ListTrash(rec, req)
	assertStatusCode(t, rec, http.StatusServiceUnavailable)
}

func TestPhotosHandler_BatchPurge_NoTrashStore(t *testing.T) {
	h := createPhotosHandlerNative(testConfig(), newFakePhotoReader(), newTestStorage(t))
	req := newBatchRequest(t, "/api/v1/photos/batch/purge", `{"photo_uids": ["p1"]}`)
	rec := httptest.NewRecorder()
	h.BatchPurge(rec, req)
	assertStatusCode(t, rec, http.StatusServiceUnavailable)
}

func TestPhotosHandler_BatchPurge_MissingUIDs(t *testing.T) {
	reader := newFakePhotoReader()
	store := newTestStorage(t)
	h, _, _ := createPhotosHandlerWithTrash(t, testConfig(), reader, store)

	req := newBatchRequest(t, "/api/v1/photos/batch/purge", `{}`)
	rec := httptest.NewRecorder()
	h.BatchPurge(rec, req)
	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "photo_uids is required")
}

func TestPhotosHandler_BatchPurge_InvalidJSON(t *testing.T) {
	reader := newFakePhotoReader()
	store := newTestStorage(t)
	h, _, _ := createPhotosHandlerWithTrash(t, testConfig(), reader, store)

	req := newBatchRequest(t, "/api/v1/photos/batch/purge", `{invalid}`)
	rec := httptest.NewRecorder()
	h.BatchPurge(rec, req)
	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "invalid request body")
}

func TestPhotosHandler_BatchPurge_ViewerForbidden(t *testing.T) {
	reader := newFakePhotoReader()
	store := newTestStorage(t)
	h, _, _ := createPhotosHandlerWithTrash(t, testConfig(), reader, store)

	req := newBatchRequest(t, "/api/v1/photos/batch/purge", `{"photo_uids": ["p1"]}`)
	ctx := middleware.SetSessionInContext(req.Context(), &middleware.Session{Role: "viewer"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.BatchPurge(rec, req)
	assertStatusCode(t, rec, http.StatusForbidden)
}

func TestPhotosHandler_BatchPurge_Success(t *testing.T) {
	reader := newFakePhotoReader()
	store := newTestStorage(t)
	h, embs, faces := createPhotosHandlerWithTrash(t, testConfig(), reader, store)

	hash := "aabbcc112233"
	reader.add(samplePhoto("arch1", hash, "arch1", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)))
	archivePhoto(t, reader, "arch1", time.Now().Add(-31*24*time.Hour))
	// Stage an embedding + face so we can assert they were also cleaned.
	embs.DeleteEmbeddingCalls = nil
	faces.AddFaces("arch1", []database.StoredFace{{ID: 7, PhotoUID: "arch1"}})

	req := newBatchRequest(t, "/api/v1/photos/batch/purge", `{"photo_uids": ["arch1"]}`)
	rec := httptest.NewRecorder()
	h.BatchPurge(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var resp BatchPurgeResponse
	parseJSONResponse(t, rec, &resp)
	if resp.Purged != 1 {
		t.Errorf("Purged = %d, want 1", resp.Purged)
	}
	if len(resp.Errors) != 0 {
		t.Errorf("Errors = %+v, want empty", resp.Errors)
	}
	if _, err := reader.GetPhoto(context.Background(), "arch1"); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("photo should be hard-deleted; got err %v", err)
	}
	if len(embs.DeleteEmbeddingCalls) != 1 || embs.DeleteEmbeddingCalls[0] != "arch1" {
		t.Errorf("expected DeleteEmbedding(arch1), got %+v", embs.DeleteEmbeddingCalls)
	}
	if len(faces.DeleteFacesCalls) != 1 || faces.DeleteFacesCalls[0] != "arch1" {
		t.Errorf("expected DeleteFacesByPhoto(arch1), got %+v", faces.DeleteFacesCalls)
	}
}

func TestPhotosHandler_BatchPurge_RejectsLivePhoto(t *testing.T) {
	reader := newFakePhotoReader()
	store := newTestStorage(t)
	h, embs, _ := createPhotosHandlerWithTrash(t, testConfig(), reader, store)

	// "live" is intentionally not archived; "arch" is archived. The purge
	// must report the live photo in Errors but still hard-delete the
	// archived one.
	reader.add(samplePhoto("live", "111111abcabc", "live", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)))
	reader.add(samplePhoto("arch", "222222abcabc", "arch", time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)))
	archivePhoto(t, reader, "arch", time.Now())

	req := newBatchRequest(t, "/api/v1/photos/batch/purge",
		`{"photo_uids": ["live", "arch", "missing"]}`)
	rec := httptest.NewRecorder()
	h.BatchPurge(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var resp BatchPurgeResponse
	parseJSONResponse(t, rec, &resp)
	if resp.Purged != 1 {
		t.Errorf("Purged = %d, want 1", resp.Purged)
	}
	if len(resp.Errors) != 2 {
		t.Fatalf("Errors length = %d, want 2 (live + missing); got %+v", len(resp.Errors), resp.Errors)
	}

	uidToErr := map[string]string{}
	for _, e := range resp.Errors {
		uidToErr[e.PhotoUID] = e.Error
	}
	if !strings.Contains(uidToErr["live"], "not archived") {
		t.Errorf("live photo error = %q, want a 'not archived' message", uidToErr["live"])
	}
	if _, ok := uidToErr["missing"]; !ok {
		t.Errorf("missing photo should produce an error entry")
	}
	if _, err := reader.GetPhoto(context.Background(), "live"); errors.Is(err, database.ErrNotFound) {
		t.Errorf("live photo must NOT be hard-deleted")
	}
	if len(embs.DeleteEmbeddingCalls) != 1 {
		t.Errorf("DeleteEmbedding should run exactly once for arch; got %v", embs.DeleteEmbeddingCalls)
	}
}
