package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// fakeAlbumRepo is an in-memory database.AlbumWriter used by handler tests.
// It is intentionally simple: enough to drive the handler paths and let the
// tests assert the JSON wire shape without standing up a Postgres container.
type fakeAlbumRepo struct {
	albums map[string]*database.Album
	// members[albumUID] holds the ordered photo UIDs in the album.
	members map[string][]string
	err     error
}

func newFakeAlbumRepo() *fakeAlbumRepo {
	return &fakeAlbumRepo{
		albums:  map[string]*database.Album{},
		members: map[string][]string{},
	}
}

func (f *fakeAlbumRepo) add(a *database.Album) {
	cp := *a
	f.albums[a.UID] = &cp
}

func (f *fakeAlbumRepo) GetAlbum(_ context.Context, uid string) (*database.Album, error) {
	if f.err != nil {
		return nil, f.err
	}
	a, ok := f.albums[uid]
	if !ok {
		return nil, database.ErrNotFound
	}
	cp := *a
	cp.PhotoCount = len(f.members[uid])
	return &cp, nil
}

func (f *fakeAlbumRepo) GetAlbumBySlug(_ context.Context, slug string) (*database.Album, error) {
	if f.err != nil {
		return nil, f.err
	}
	for _, a := range f.albums {
		if a.Slug == slug {
			cp := *a
			cp.PhotoCount = len(f.members[a.UID])
			return &cp, nil
		}
	}
	return nil, database.ErrNotFound
}

func (f *fakeAlbumRepo) ListAlbums(_ context.Context, q database.AlbumQuery) ([]database.Album, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []database.Album
	for _, a := range f.albums {
		if q.Type != "" && a.Type != q.Type {
			continue
		}
		if q.Favorite != nil && a.Favorite != *q.Favorite {
			continue
		}
		if s := strings.ToLower(q.Search); s != "" {
			hay := strings.ToLower(a.Title + " " + a.Description)
			if !strings.Contains(hay, s) {
				continue
			}
		}
		cp := *a
		cp.PhotoCount = len(f.members[a.UID])
		out = append(out, cp)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	start := min(q.Offset, len(out))
	end := min(start+limit, len(out))
	return out[start:end], nil
}

func (f *fakeAlbumRepo) ListAlbumPhotoUIDs(_ context.Context, albumUID string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	cp := append([]string(nil), f.members[albumUID]...)
	return cp, nil
}

func (f *fakeAlbumRepo) ListAlbumsForPhoto(_ context.Context, photoUID string) ([]database.Album, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []database.Album
	for uid, list := range f.members {
		if slices.Contains(list, photoUID) {
			if a, ok := f.albums[uid]; ok {
				cp := *a
				cp.PhotoCount = len(list)
				out = append(out, cp)
			}
		}
	}
	return out, nil
}

func (f *fakeAlbumRepo) CreateAlbum(_ context.Context, a *database.Album) error {
	if f.err != nil {
		return f.err
	}
	if a.UID == "" {
		a.UID = "a-fake-" + a.Title
	}
	if a.Slug == "" {
		a.Slug = strings.ToLower(strings.ReplaceAll(a.Title, " ", "-"))
	}
	if a.Type == "" {
		a.Type = "album"
	}
	now := time.Now()
	a.CreatedAt = now
	a.UpdatedAt = now
	cp := *a
	f.albums[a.UID] = &cp
	return nil
}

func (f *fakeAlbumRepo) UpdateAlbum(_ context.Context, a *database.Album) error {
	if f.err != nil {
		return f.err
	}
	if _, ok := f.albums[a.UID]; !ok {
		return database.ErrNotFound
	}
	if a.Slug == "" {
		a.Slug = strings.ToLower(strings.ReplaceAll(a.Title, " ", "-"))
	}
	a.UpdatedAt = time.Now()
	cp := *a
	f.albums[a.UID] = &cp
	return nil
}

func (f *fakeAlbumRepo) DeleteAlbum(_ context.Context, uid string) error {
	if f.err != nil {
		return f.err
	}
	if _, ok := f.albums[uid]; !ok {
		return database.ErrNotFound
	}
	delete(f.albums, uid)
	delete(f.members, uid)
	return nil
}

func (f *fakeAlbumRepo) AddPhotos(_ context.Context, albumUID string, photoUIDs []string) error {
	if f.err != nil {
		return f.err
	}
	if _, ok := f.albums[albumUID]; !ok {
		return errors.New("no such album")
	}
	existing := map[string]bool{}
	for _, p := range f.members[albumUID] {
		existing[p] = true
	}
	for _, p := range photoUIDs {
		if !existing[p] {
			f.members[albumUID] = append(f.members[albumUID], p)
			existing[p] = true
		}
	}
	return nil
}

func (f *fakeAlbumRepo) RemovePhotos(_ context.Context, albumUID string, photoUIDs []string) error {
	if f.err != nil {
		return f.err
	}
	rm := map[string]bool{}
	for _, p := range photoUIDs {
		rm[p] = true
	}
	list := f.members[albumUID]
	out := list[:0]
	for _, p := range list {
		if !rm[p] {
			out = append(out, p)
		}
	}
	f.members[albumUID] = out
	if a, ok := f.albums[albumUID]; ok && a.CoverPhotoUID != "" && rm[a.CoverPhotoUID] {
		a.CoverPhotoUID = ""
	}
	return nil
}

func (f *fakeAlbumRepo) SetCoverPhoto(_ context.Context, albumUID, photoUID string) error {
	if f.err != nil {
		return f.err
	}
	a, ok := f.albums[albumUID]
	if !ok {
		return database.ErrNotFound
	}
	if !slices.Contains(f.members[albumUID], photoUID) {
		return database.ErrAlbumPhotoNotInAlbum
	}
	a.CoverPhotoUID = photoUID
	return nil
}

// createAlbumsHandler wires an AlbumsHandler against the two fake repos.
func createAlbumsHandler(t *testing.T, albumRepo database.AlbumWriter, photoRepo database.PhotoReader) *AlbumsHandler {
	t.Helper()
	return NewAlbumsHandler(testConfig(), nil, albumRepo, photoRepo)
}

func sampleAlbum(uid, title string) *database.Album {
	now := time.Now().Add(-time.Hour)
	return &database.Album{
		UID:       uid,
		Slug:      strings.ToLower(strings.ReplaceAll(title, " ", "-")),
		Title:     title,
		Type:      "album",
		OrderBy:   "newest",
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestAlbumsHandler_List_Success(t *testing.T) {
	repo := newFakeAlbumRepo()
	repo.add(sampleAlbum("album1", "Album One"))
	repo.add(sampleAlbum("album2", "Album Two"))
	repo.members["album1"] = []string{"p1", "p2"}

	h := createAlbumsHandler(t, repo, nil)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/albums", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	assertContentType(t, rec, "application/json")

	var albums []AlbumResponse
	parseJSONResponse(t, rec, &albums)
	if len(albums) != 2 {
		t.Fatalf("expected 2 albums, got %d", len(albums))
	}

	got := map[string]AlbumResponse{}
	for _, a := range albums {
		got[a.UID] = a
	}
	if got["album1"].PhotoCount != 2 {
		t.Errorf("album1 PhotoCount = %d, want 2", got["album1"].PhotoCount)
	}
	if got["album2"].PhotoCount != 0 {
		t.Errorf("album2 PhotoCount = %d, want 0", got["album2"].PhotoCount)
	}
}

func TestAlbumsHandler_List_NoRepo(t *testing.T) {
	h := createAlbumsHandler(t, nil, nil)
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/albums", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	assertStatusCode(t, rec, http.StatusServiceUnavailable)
}

func TestAlbumsHandler_List_TypeFilter(t *testing.T) {
	repo := newFakeAlbumRepo()
	a := sampleAlbum("album1", "Album One")
	repo.add(a)
	folder := sampleAlbum("folder1", "A Folder")
	folder.Type = "folder"
	repo.add(folder)

	h := createAlbumsHandler(t, repo, nil)
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/albums?type=folder", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var albums []AlbumResponse
	parseJSONResponse(t, rec, &albums)
	if len(albums) != 1 || albums[0].UID != "folder1" {
		t.Errorf("type=folder filter wrong: %+v", albums)
	}
}

func TestAlbumsHandler_Get_Success(t *testing.T) {
	repo := newFakeAlbumRepo()
	repo.add(sampleAlbum("album123", "Test Album"))
	repo.members["album123"] = []string{"p1", "p2", "p3"}

	h := createAlbumsHandler(t, repo, nil)
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/albums/album123", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "album123"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	assertContentType(t, rec, "application/json")
	var album AlbumResponse
	parseJSONResponse(t, rec, &album)
	if album.UID != "album123" {
		t.Errorf("UID = %q, want album123", album.UID)
	}
	if album.PhotoCount != 3 {
		t.Errorf("PhotoCount = %d, want 3", album.PhotoCount)
	}
}

func TestAlbumsHandler_Get_NotFound(t *testing.T) {
	repo := newFakeAlbumRepo()
	h := createAlbumsHandler(t, repo, nil)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/albums/missing", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "missing"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	assertStatusCode(t, rec, http.StatusNotFound)
	assertJSONError(t, rec, "album not found")
}

func TestAlbumsHandler_Get_MissingUID(t *testing.T) {
	repo := newFakeAlbumRepo()
	h := createAlbumsHandler(t, repo, nil)
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/albums/", nil)
	req = requestWithChiParams(req, map[string]string{})
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "missing album UID")
}

func TestAlbumsHandler_Create_Success(t *testing.T) {
	repo := newFakeAlbumRepo()
	h := createAlbumsHandler(t, repo, nil)

	body := bytes.NewBufferString(`{"title": "New Album"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/albums", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	assertStatusCode(t, rec, http.StatusCreated)
	var album AlbumResponse
	parseJSONResponse(t, rec, &album)
	if album.Title != "New Album" {
		t.Errorf("Title = %q", album.Title)
	}
	if album.UID == "" {
		t.Errorf("UID should be populated")
	}
	if album.Type != "album" {
		t.Errorf("Type = %q, want album", album.Type)
	}
}

func TestAlbumsHandler_Create_MissingTitle(t *testing.T) {
	repo := newFakeAlbumRepo()
	h := createAlbumsHandler(t, repo, nil)

	body := bytes.NewBufferString(`{"title": ""}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/albums", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "title is required")
}

func TestAlbumsHandler_Create_InvalidJSON(t *testing.T) {
	repo := newFakeAlbumRepo()
	h := createAlbumsHandler(t, repo, nil)

	body := bytes.NewBufferString(`{invalid json}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/albums", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "invalid request body")
}

func TestAlbumsHandler_Update_Success(t *testing.T) {
	repo := newFakeAlbumRepo()
	repo.add(sampleAlbum("album1", "Original"))
	h := createAlbumsHandler(t, repo, nil)

	body := bytes.NewBufferString(`{"title": "Renamed", "favorite": true}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/albums/album1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "album1"})
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var album AlbumResponse
	parseJSONResponse(t, rec, &album)
	if album.Title != "Renamed" {
		t.Errorf("Title = %q, want Renamed", album.Title)
	}
	if !album.Favorite {
		t.Errorf("Favorite should be true")
	}
}

func TestAlbumsHandler_Update_NotFound(t *testing.T) {
	repo := newFakeAlbumRepo()
	h := createAlbumsHandler(t, repo, nil)

	body := bytes.NewBufferString(`{"title": "Renamed"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/albums/missing", body)
	req = requestWithChiParams(req, map[string]string{"uid": "missing"})
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	assertStatusCode(t, rec, http.StatusNotFound)
}

func TestAlbumsHandler_Delete_Success(t *testing.T) {
	repo := newFakeAlbumRepo()
	repo.add(sampleAlbum("album1", "To Delete"))
	h := createAlbumsHandler(t, repo, nil)

	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/albums/album1", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "album1"})
	rec := httptest.NewRecorder()
	h.Delete(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	if _, ok := repo.albums["album1"]; ok {
		t.Error("album should have been deleted")
	}
}

func TestAlbumsHandler_Delete_NotFound(t *testing.T) {
	repo := newFakeAlbumRepo()
	h := createAlbumsHandler(t, repo, nil)

	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/albums/missing", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "missing"})
	rec := httptest.NewRecorder()
	h.Delete(rec, req)

	assertStatusCode(t, rec, http.StatusNotFound)
}

func TestAlbumsHandler_GetPhotos_DelegatesToPhotoReader(t *testing.T) {
	albumRepo := newFakeAlbumRepo()
	albumRepo.add(sampleAlbum("album1", "Album"))
	albumRepo.members["album1"] = []string{"p1", "p2"}

	photoRepo := newFakePhotoReader()
	photoRepo.add(samplePhoto("p1", "hash1", "first", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)))
	photoRepo.add(samplePhoto("p2", "hash2", "second", time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC)))
	photoRepo.add(samplePhoto("p3", "hash3", "not in album", time.Date(2024, 8, 1, 0, 0, 0, 0, time.UTC)))

	// To simulate the filter, our fakePhotoReader currently does not honor
	// album_uid. The handler still hits ListPhotos and the test asserts
	// that the call goes through and the response decodes cleanly.
	h := createAlbumsHandler(t, albumRepo, photoRepo)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/albums/album1/photos", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "album1"})
	rec := httptest.NewRecorder()
	h.GetPhotos(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var photos []PhotoResponse
	parseJSONResponse(t, rec, &photos)
	if len(photos) == 0 {
		t.Error("expected photo list, got empty")
	}
}

func TestAlbumsHandler_GetPhotos_MissingUID(t *testing.T) {
	h := createAlbumsHandler(t, newFakeAlbumRepo(), newFakePhotoReader())
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/albums//photos", nil)
	req = requestWithChiParams(req, map[string]string{})
	rec := httptest.NewRecorder()
	h.GetPhotos(rec, req)
	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "missing album UID")
}

func TestAlbumsHandler_AddPhotos_Success(t *testing.T) {
	repo := newFakeAlbumRepo()
	repo.add(sampleAlbum("album1", "Album"))
	h := createAlbumsHandler(t, repo, nil)

	body := bytes.NewBufferString(`{"photo_uids": ["p1", "p2"]}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/albums/album1/photos", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "album1"})
	rec := httptest.NewRecorder()
	h.AddPhotos(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var result map[string]int
	parseJSONResponse(t, rec, &result)
	if result["added"] != 2 {
		t.Errorf("added = %d, want 2", result["added"])
	}
	if len(repo.members["album1"]) != 2 {
		t.Errorf("members = %v, want 2 entries", repo.members["album1"])
	}
}

func TestAlbumsHandler_AddPhotos_EmptyList(t *testing.T) {
	repo := newFakeAlbumRepo()
	repo.add(sampleAlbum("album1", "Album"))
	h := createAlbumsHandler(t, repo, nil)

	body := bytes.NewBufferString(`{"photo_uids": []}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/albums/album1/photos", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "album1"})
	rec := httptest.NewRecorder()
	h.AddPhotos(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "photo_uids is required")
}

func TestAlbumsHandler_AddPhotos_Idempotent(t *testing.T) {
	repo := newFakeAlbumRepo()
	repo.add(sampleAlbum("album1", "Album"))
	repo.members["album1"] = []string{"p1"}
	h := createAlbumsHandler(t, repo, nil)

	body := bytes.NewBufferString(`{"photo_uids": ["p1", "p2"]}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/albums/album1/photos", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "album1"})
	rec := httptest.NewRecorder()
	h.AddPhotos(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	if len(repo.members["album1"]) != 2 {
		t.Errorf("members = %v, want exactly 2 entries", repo.members["album1"])
	}
}

func TestAlbumsHandler_ClearPhotos_All(t *testing.T) {
	repo := newFakeAlbumRepo()
	repo.add(sampleAlbum("album1", "Album"))
	repo.members["album1"] = []string{"p1", "p2"}
	h := createAlbumsHandler(t, repo, nil)

	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/albums/album1/photos", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "album1"})
	rec := httptest.NewRecorder()
	h.ClearPhotos(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var result map[string]int
	parseJSONResponse(t, rec, &result)
	if result["removed"] != 2 {
		t.Errorf("removed = %d, want 2", result["removed"])
	}
	if len(repo.members["album1"]) != 0 {
		t.Errorf("members should be empty, got %v", repo.members["album1"])
	}
}

func TestAlbumsHandler_ClearPhotos_EmptyAlbum(t *testing.T) {
	repo := newFakeAlbumRepo()
	repo.add(sampleAlbum("album1", "Album"))
	h := createAlbumsHandler(t, repo, nil)

	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/albums/album1/photos", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "album1"})
	rec := httptest.NewRecorder()
	h.ClearPhotos(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var result map[string]int
	parseJSONResponse(t, rec, &result)
	if result["removed"] != 0 {
		t.Errorf("removed = %d, want 0", result["removed"])
	}
}

func TestAlbumsHandler_RemovePhotos_Success(t *testing.T) {
	repo := newFakeAlbumRepo()
	repo.add(sampleAlbum("album1", "Album"))
	repo.members["album1"] = []string{"p1", "p2", "p3"}
	h := createAlbumsHandler(t, repo, nil)

	body := bytes.NewBufferString(`{"photo_uids": ["p2"]}`)
	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/albums/album1/photos/batch", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "album1"})
	rec := httptest.NewRecorder()
	h.RemovePhotos(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	if len(repo.members["album1"]) != 2 {
		t.Errorf("expected 2 remaining members, got %v", repo.members["album1"])
	}
}

func TestAlbumsHandler_GetPhotoAlbums_Success(t *testing.T) {
	repo := newFakeAlbumRepo()
	repo.add(sampleAlbum("album1", "A1"))
	repo.add(sampleAlbum("album2", "A2"))
	repo.add(sampleAlbum("album3", "A3"))
	repo.members["album1"] = []string{"shared", "x"}
	repo.members["album2"] = []string{"shared"}
	repo.members["album3"] = []string{"y"}
	h := createAlbumsHandler(t, repo, nil)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/shared/albums", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "shared"})
	rec := httptest.NewRecorder()
	h.GetPhotoAlbums(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var hits []AlbumMembershipResponse
	parseJSONResponse(t, rec, &hits)
	if len(hits) != 2 {
		t.Fatalf("expected 2 memberships, got %d (%+v)", len(hits), hits)
	}
	got := map[string]bool{}
	for _, h := range hits {
		got[h.UID] = true
	}
	if !got["album1"] || !got["album2"] {
		t.Errorf("unexpected memberships: %+v", hits)
	}
}

func TestAlbumsHandler_GetPhotoAlbums_NoRepo(t *testing.T) {
	h := createAlbumsHandler(t, nil, nil)
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/p1/albums", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "p1"})
	rec := httptest.NewRecorder()
	h.GetPhotoAlbums(rec, req)
	assertStatusCode(t, rec, http.StatusServiceUnavailable)
}

// TestAlbumsHandler_Get_IncludesGapFixFields verifies the
// location / category / notes / filter / order columns added by task
// 332a727c are surfaced on the API response.
func TestAlbumsHandler_Get_IncludesGapFixFields(t *testing.T) {
	repo := newFakeAlbumRepo()
	a := sampleAlbum("album1", "Holiday")
	a.Location = "Italy, Tuscany"
	a.Category = "Travel"
	a.Notes = "trip notes"
	a.Filter = "public:true year:2024"
	a.Order = "newest"
	repo.add(a)
	h := createAlbumsHandler(t, repo, nil)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/albums/album1", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "album1"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var got AlbumResponse
	parseJSONResponse(t, rec, &got)
	if got.Location != "Italy, Tuscany" {
		t.Errorf("location = %q", got.Location)
	}
	if got.Category != "Travel" {
		t.Errorf("category = %q", got.Category)
	}
	if got.Notes != "trip notes" {
		t.Errorf("notes = %q", got.Notes)
	}
	if got.Filter != "public:true year:2024" {
		t.Errorf("filter = %q (smart-album DSL must be preserved verbatim)", got.Filter)
	}
	if got.Order != "newest" {
		t.Errorf("order = %q", got.Order)
	}
}

// TestAlbumsHandler_Create_GapFixFieldsRoundTrip exercises POST with the
// new columns and asserts the response + storage reflect them.
func TestAlbumsHandler_Create_GapFixFieldsRoundTrip(t *testing.T) {
	repo := newFakeAlbumRepo()
	h := createAlbumsHandler(t, repo, nil)

	body := bytes.NewBufferString(
		`{"title":"Holiday","location":"Italy","category":"Travel",` +
			`"notes":"n","filter":"public:true","order":"newest"}`,
	)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/albums", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	assertStatusCode(t, rec, http.StatusCreated)
	var got AlbumResponse
	parseJSONResponse(t, rec, &got)
	if got.Location != "Italy" || got.Category != "Travel" || got.Notes != "n" ||
		got.Filter != "public:true" || got.Order != "newest" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

// TestAlbumsHandler_Update_GapFixFieldsRoundTrip exercises PUT with the
// new columns on an existing album.
func TestAlbumsHandler_Update_GapFixFieldsRoundTrip(t *testing.T) {
	repo := newFakeAlbumRepo()
	repo.add(sampleAlbum("album1", "Holiday"))
	h := createAlbumsHandler(t, repo, nil)

	body := bytes.NewBufferString(
		`{"location":"Italy","category":"Travel","notes":"n",` +
			`"filter":"public:true","order":"oldest"}`,
	)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/albums/album1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "album1"})
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var got AlbumResponse
	parseJSONResponse(t, rec, &got)
	if got.Location != "Italy" || got.Order != "oldest" || got.Filter != "public:true" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	stored, _ := repo.GetAlbum(context.Background(), "album1")
	if stored.Location != "Italy" || stored.Notes != "n" || stored.Filter != "public:true" {
		t.Errorf("storage mismatch: %+v", stored)
	}
}

// TestAlbumsHandler_Update_NotesTooLong asserts the 8 KiB cap on notes.
func TestAlbumsHandler_Update_NotesTooLong(t *testing.T) {
	repo := newFakeAlbumRepo()
	repo.add(sampleAlbum("album1", "Holiday"))
	h := createAlbumsHandler(t, repo, nil)

	oversized := make([]byte, 8*1024+1)
	for i := range oversized {
		oversized[i] = 'x'
	}
	body := bytes.NewBufferString(`{"notes":"` + string(oversized) + `"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/albums/album1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "album1"})
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "notes exceeds 8 KiB limit")
}
