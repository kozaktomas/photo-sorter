package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// fakeLabelRepo is an in-memory database.LabelWriter used by handler tests.
// It mimics just enough of the real PostgreSQL repository to drive the
// handler paths without standing up a database — slug collision resolution,
// idempotent EnsureLabel, idempotent AddPhotoLabel, and a "deleted count"
// that matches the count of UIDs that actually existed.
type fakeLabelRepo struct {
	mu        sync.Mutex
	labels    map[string]*database.Label
	bySlug    map[string]string
	members   map[string]map[string]bool
	listErr   error
	getErr    error
	ensureErr error
	updateErr error
	deleteErr error
	addErr    error
}

func newFakeLabelRepo() *fakeLabelRepo {
	return &fakeLabelRepo{
		labels:  map[string]*database.Label{},
		bySlug:  map[string]string{},
		members: map[string]map[string]bool{},
	}
}

func (f *fakeLabelRepo) seed(uid, name string) *database.Label {
	f.mu.Lock()
	defer f.mu.Unlock()
	slug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	now := time.Now()
	l := &database.Label{
		UID:       uid,
		Slug:      slug,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	f.labels[uid] = l
	f.bySlug[slug] = uid
	return l
}

func (f *fakeLabelRepo) addMember(labelUID, photoUID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.members[labelUID]; !ok {
		f.members[labelUID] = map[string]bool{}
	}
	f.members[labelUID][photoUID] = true
}

func (f *fakeLabelRepo) GetLabel(_ context.Context, uid string) (*database.Label, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	l, ok := f.labels[uid]
	if !ok {
		return nil, database.ErrNotFound
	}
	cp := *l
	cp.PhotoCount = len(f.members[uid])
	return &cp, nil
}

func (f *fakeLabelRepo) GetLabelBySlug(_ context.Context, slug string) (*database.Label, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	uid, ok := f.bySlug[slug]
	if !ok {
		return nil, database.ErrNotFound
	}
	cp := *f.labels[uid]
	cp.PhotoCount = len(f.members[uid])
	return &cp, nil
}

func (f *fakeLabelRepo) ListLabels(_ context.Context, q database.LabelQuery) ([]database.Label, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]database.Label, 0, len(f.labels))
	for uid, l := range f.labels {
		count := len(f.members[uid])
		if q.MinPhotos > 0 && count < q.MinPhotos {
			continue
		}
		if s := strings.ToLower(q.Search); s != "" {
			if !strings.Contains(strings.ToLower(l.Name+" "+l.Slug), s) {
				continue
			}
		}
		cp := *l
		cp.PhotoCount = count
		out = append(out, cp)
	}
	switch q.SortBy {
	case "-name":
		slices.SortFunc(out, func(a, b database.Label) int {
			return strings.Compare(b.Name, a.Name)
		})
	case "count":
		slices.SortFunc(out, func(a, b database.Label) int {
			return a.PhotoCount - b.PhotoCount
		})
	case "-count":
		slices.SortFunc(out, func(a, b database.Label) int {
			return b.PhotoCount - a.PhotoCount
		})
	default:
		slices.SortFunc(out, func(a, b database.Label) int {
			return strings.Compare(a.Name, b.Name)
		})
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 1000
	}
	start := min(q.Offset, len(out))
	end := min(start+limit, len(out))
	return out[start:end], nil
}

func (f *fakeLabelRepo) ListLabelsForPhoto(_ context.Context, photoUID string) ([]database.Label, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []database.Label
	for uid, photos := range f.members {
		if photos[photoUID] {
			if l, ok := f.labels[uid]; ok {
				cp := *l
				cp.PhotoCount = len(photos)
				out = append(out, cp)
			}
		}
	}
	return out, nil
}

func (f *fakeLabelRepo) EnsureLabel(_ context.Context, name string) (*database.Label, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ensureErr != nil {
		return nil, f.ensureErr
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("name is required")
	}
	slug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	if uid, ok := f.bySlug[slug]; ok {
		cp := *f.labels[uid]
		cp.PhotoCount = len(f.members[uid])
		return &cp, nil
	}
	uid := "l-fake-" + slug
	now := time.Now()
	l := &database.Label{
		UID:       uid,
		Slug:      slug,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	f.labels[uid] = l
	f.bySlug[slug] = uid
	cp := *l
	return &cp, nil
}

func (f *fakeLabelRepo) UpdateLabel(_ context.Context, l *database.Label) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateErr != nil {
		return f.updateErr
	}
	existing, ok := f.labels[l.UID]
	if !ok {
		return database.ErrNotFound
	}
	// Re-slug from name if Slug is empty (matches the real repo).
	slug := l.Slug
	if slug == "" {
		slug = strings.ToLower(strings.ReplaceAll(l.Name, " ", "-"))
	}
	// Free up the old slug if it changes.
	if existing.Slug != slug {
		delete(f.bySlug, existing.Slug)
		f.bySlug[slug] = l.UID
	}
	l.Slug = slug
	l.UpdatedAt = time.Now()
	cp := *l
	f.labels[l.UID] = &cp
	return nil
}

func (f *fakeLabelRepo) DeleteLabels(_ context.Context, uids []string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return 0, f.deleteErr
	}
	count := 0
	for _, uid := range uids {
		if l, ok := f.labels[uid]; ok {
			delete(f.bySlug, l.Slug)
			delete(f.labels, uid)
			delete(f.members, uid)
			count++
		}
	}
	return count, nil
}

func (f *fakeLabelRepo) AddPhotoLabel(
	_ context.Context, photoUID, labelUID, source string, uncertainty int,
) error {
	_ = source
	_ = uncertainty
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addErr != nil {
		return f.addErr
	}
	if _, ok := f.labels[labelUID]; !ok {
		return errors.New("no such label")
	}
	if _, ok := f.members[labelUID]; !ok {
		f.members[labelUID] = map[string]bool{}
	}
	f.members[labelUID][photoUID] = true
	return nil
}

func (f *fakeLabelRepo) RemovePhotoLabel(_ context.Context, photoUID, labelUID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if photos, ok := f.members[labelUID]; ok {
		delete(photos, photoUID)
	}
	return nil
}

// createLabelsHandler wires a LabelsHandler against the fake repo.
func createLabelsHandler(t *testing.T, repo database.LabelWriter) *LabelsHandler {
	t.Helper()
	return NewLabelsHandler(testConfig(), nil, repo)
}

func TestLabelsHandler_List_Success(t *testing.T) {
	repo := newFakeLabelRepo()
	repo.seed("label1", "Nature")
	repo.seed("label2", "People")
	repo.addMember("label1", "p1")
	repo.addMember("label1", "p2")

	h := createLabelsHandler(t, repo)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/labels", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	assertContentType(t, rec, "application/json")

	var labels []LabelResponse
	parseJSONResponse(t, rec, &labels)
	if len(labels) != 2 {
		t.Fatalf("expected 2 labels, got %d", len(labels))
	}
	got := map[string]LabelResponse{}
	for _, l := range labels {
		got[l.UID] = l
	}
	if got["label1"].PhotoCount != 2 {
		t.Errorf("label1 PhotoCount = %d, want 2", got["label1"].PhotoCount)
	}
	if got["label1"].Name != "Nature" {
		t.Errorf("label1 Name = %q, want Nature", got["label1"].Name)
	}
}

func TestLabelsHandler_List_MinPhotosFilter(t *testing.T) {
	repo := newFakeLabelRepo()
	repo.seed("label1", "Empty")
	repo.seed("label2", "Used")
	repo.addMember("label2", "p1")
	repo.addMember("label2", "p2")

	h := createLabelsHandler(t, repo)

	req := httptest.NewRequestWithContext(
		context.Background(), "GET", "/api/v1/labels?min_photos=1", nil,
	)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var labels []LabelResponse
	parseJSONResponse(t, rec, &labels)
	if len(labels) != 1 || labels[0].UID != "label2" {
		t.Errorf("min_photos=1 filter wrong: %+v", labels)
	}
}

func TestLabelsHandler_List_SortByCount(t *testing.T) {
	repo := newFakeLabelRepo()
	repo.seed("a", "Alpha")
	repo.seed("b", "Bravo")
	repo.addMember("b", "p1")
	repo.addMember("b", "p2")

	h := createLabelsHandler(t, repo)

	req := httptest.NewRequestWithContext(
		context.Background(), "GET", "/api/v1/labels?sort=-count", nil,
	)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var labels []LabelResponse
	parseJSONResponse(t, rec, &labels)
	if len(labels) != 2 || labels[0].UID != "b" {
		t.Errorf("-count sort wrong, expected b first: %+v", labels)
	}
}

func TestLabelsHandler_List_NoRepo(t *testing.T) {
	h := createLabelsHandler(t, nil)
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/labels", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	assertStatusCode(t, rec, http.StatusServiceUnavailable)
}

func TestLabelsHandler_List_InvalidMinPhotos(t *testing.T) {
	repo := newFakeLabelRepo()
	h := createLabelsHandler(t, repo)

	req := httptest.NewRequestWithContext(
		context.Background(), "GET", "/api/v1/labels?min_photos=abc", nil,
	)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "invalid min_photos")
}

func TestLabelsHandler_List_RepoError(t *testing.T) {
	repo := newFakeLabelRepo()
	repo.listErr = errors.New("boom")
	h := createLabelsHandler(t, repo)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/labels", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	assertStatusCode(t, rec, http.StatusInternalServerError)
	assertJSONError(t, rec, "failed to get labels")
}

func TestLabelsHandler_Get_Success(t *testing.T) {
	repo := newFakeLabelRepo()
	repo.seed("label1", "Nature")
	repo.addMember("label1", "p1")

	h := createLabelsHandler(t, repo)
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/labels/label1", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "label1"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	assertContentType(t, rec, "application/json")
	var label LabelResponse
	parseJSONResponse(t, rec, &label)
	if label.UID != "label1" {
		t.Errorf("UID = %q, want label1", label.UID)
	}
	if label.Name != "Nature" {
		t.Errorf("Name = %q, want Nature", label.Name)
	}
	if label.PhotoCount != 1 {
		t.Errorf("PhotoCount = %d, want 1", label.PhotoCount)
	}
}

func TestLabelsHandler_Get_NotFound(t *testing.T) {
	repo := newFakeLabelRepo()
	h := createLabelsHandler(t, repo)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/labels/missing", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "missing"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	assertStatusCode(t, rec, http.StatusNotFound)
	assertJSONError(t, rec, "label not found")
}

func TestLabelsHandler_Get_MissingUID(t *testing.T) {
	repo := newFakeLabelRepo()
	h := createLabelsHandler(t, repo)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/labels/", nil)
	req = requestWithChiParams(req, map[string]string{})
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "uid is required")
}

func TestLabelsHandler_Update_Success(t *testing.T) {
	repo := newFakeLabelRepo()
	repo.seed("label1", "Original")

	h := createLabelsHandler(t, repo)
	body := bytes.NewBufferString(`{"name": "Renamed", "favorite": true, "priority": 5}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/labels/label1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "label1"})
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var label LabelResponse
	parseJSONResponse(t, rec, &label)
	if label.Name != "Renamed" {
		t.Errorf("Name = %q, want Renamed", label.Name)
	}
	if !label.Favorite {
		t.Errorf("Favorite should be true")
	}
	if label.Priority != 5 {
		t.Errorf("Priority = %d, want 5", label.Priority)
	}
	if label.Slug != "renamed" {
		t.Errorf("Slug = %q, want renamed (re-slugged from new name)", label.Slug)
	}
}

func TestLabelsHandler_Update_PartialUpdate(t *testing.T) {
	repo := newFakeLabelRepo()
	repo.seed("label1", "Original")

	h := createLabelsHandler(t, repo)
	body := bytes.NewBufferString(`{"favorite": true}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/labels/label1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "label1"})
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var label LabelResponse
	parseJSONResponse(t, rec, &label)
	if label.Name != "Original" {
		t.Errorf("Name should be preserved, got %q", label.Name)
	}
	if !label.Favorite {
		t.Errorf("Favorite should be true")
	}
}

func TestLabelsHandler_Update_NotFound(t *testing.T) {
	repo := newFakeLabelRepo()
	h := createLabelsHandler(t, repo)

	body := bytes.NewBufferString(`{"name": "X"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/labels/missing", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "missing"})
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	assertStatusCode(t, rec, http.StatusNotFound)
}

func TestLabelsHandler_Update_MissingUID(t *testing.T) {
	repo := newFakeLabelRepo()
	h := createLabelsHandler(t, repo)

	body := bytes.NewBufferString(`{"name": "Updated"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/labels/", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{})
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "uid is required")
}

func TestLabelsHandler_Update_InvalidJSON(t *testing.T) {
	repo := newFakeLabelRepo()
	repo.seed("label1", "X")
	h := createLabelsHandler(t, repo)

	body := bytes.NewBufferString(`{invalid json}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/labels/label1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "label1"})
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "invalid request body")
}

func TestLabelsHandler_BatchDelete_Success(t *testing.T) {
	repo := newFakeLabelRepo()
	repo.seed("l1", "One")
	repo.seed("l2", "Two")
	// l3 does not exist — DeleteLabels should silently skip it and report
	// the count of rows that actually existed (2).
	h := createLabelsHandler(t, repo)

	body := bytes.NewBufferString(`{"uids": ["l1", "l2", "l3"]}`)
	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/labels", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.BatchDelete(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var out map[string]int
	parseJSONResponse(t, rec, &out)
	if out["deleted"] != 2 {
		t.Errorf("deleted = %d, want 2 (l1 + l2 only; l3 unknown)", out["deleted"])
	}
	if _, ok := repo.labels["l1"]; ok {
		t.Error("l1 should have been deleted")
	}
}

func TestLabelsHandler_BatchDelete_EmptyList(t *testing.T) {
	repo := newFakeLabelRepo()
	h := createLabelsHandler(t, repo)

	body := bytes.NewBufferString(`{"uids": []}`)
	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/labels", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.BatchDelete(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "no labels specified")
}

func TestLabelsHandler_BatchDelete_InvalidJSON(t *testing.T) {
	repo := newFakeLabelRepo()
	h := createLabelsHandler(t, repo)

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/labels", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.BatchDelete(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "invalid request body")
}

func TestLabelsHandler_BatchDelete_NoRepo(t *testing.T) {
	h := createLabelsHandler(t, nil)

	body := bytes.NewBufferString(`{"uids": ["l1"]}`)
	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/labels", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.BatchDelete(rec, req)

	assertStatusCode(t, rec, http.StatusServiceUnavailable)
}

// TestLabelsHandler_Get_IncludesDescriptionAndCategories verifies the
// description and categories columns added by task 332a727c surface on
// the API response.
func TestLabelsHandler_Get_IncludesDescriptionAndCategories(t *testing.T) {
	repo := newFakeLabelRepo()
	l := repo.seed("label1", "Nature")
	l.Description = "Outdoor scenes."
	l.Categories = []string{"family", "kids", "travel"}
	h := createLabelsHandler(t, repo)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/labels/label1", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "label1"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var got LabelResponse
	parseJSONResponse(t, rec, &got)
	if got.Description != "Outdoor scenes." {
		t.Errorf("description = %q", got.Description)
	}
	if len(got.Categories) != 3 {
		t.Errorf("categories = %v, want 3 entries", got.Categories)
	}
}

// TestLabelsHandler_Update_DescriptionAndCategoriesRoundTrip exercises a
// PUT that writes the new columns and asserts the response + storage
// reflect them.
func TestLabelsHandler_Update_DescriptionAndCategoriesRoundTrip(t *testing.T) {
	repo := newFakeLabelRepo()
	repo.seed("label1", "Nature")
	h := createLabelsHandler(t, repo)

	body := bytes.NewBufferString(
		`{"description":"new desc","categories":["a","b","c"]}`,
	)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/labels/label1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "label1"})
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var got LabelResponse
	parseJSONResponse(t, rec, &got)
	if got.Description != "new desc" {
		t.Errorf("description = %q", got.Description)
	}
	if len(got.Categories) != 3 || got.Categories[1] != "b" {
		t.Errorf("categories = %v", got.Categories)
	}
	stored, _ := repo.GetLabel(context.Background(), "label1")
	if stored.Description != "new desc" || len(stored.Categories) != 3 {
		t.Errorf("storage mismatch: desc=%q cats=%v", stored.Description, stored.Categories)
	}
}

// TestLabelsHandler_Update_CategoriesTooMany asserts the 50-entry cap on
// the categories array.
func TestLabelsHandler_Update_CategoriesTooMany(t *testing.T) {
	repo := newFakeLabelRepo()
	repo.seed("label1", "Nature")
	h := createLabelsHandler(t, repo)

	// Build a 51-entry JSON array literal.
	cats := make([]byte, 0, 51*8)
	cats = append(cats, '[')
	for i := range 51 {
		if i > 0 {
			cats = append(cats, ',')
		}
		cats = append(cats, '"', 'x', '"')
	}
	cats = append(cats, ']')
	body := bytes.NewBufferString(`{"categories":` + string(cats) + `}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/labels/label1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "label1"})
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "categories exceeds 50 entry limit")
}

func TestLabelsHandler_RoundTrip(t *testing.T) {
	// Exercise the create + list + update + delete path through the
	// handlers. There is no public POST /labels endpoint (labels are born
	// via EnsureLabel in the AI sort + bulk-action pipelines), so the
	// "create" step uses the writer directly.
	repo := newFakeLabelRepo()
	created, err := repo.EnsureLabel(context.Background(), "Round Trip")
	if err != nil {
		t.Fatalf("seed via EnsureLabel: %v", err)
	}
	h := createLabelsHandler(t, repo)

	// List should now return the seeded label.
	listReq := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/labels", nil)
	listRec := httptest.NewRecorder()
	h.List(listRec, listReq)
	assertStatusCode(t, listRec, http.StatusOK)
	var listed []LabelResponse
	parseJSONResponse(t, listRec, &listed)
	if len(listed) != 1 || listed[0].UID != created.UID {
		t.Fatalf("list after seed: %+v", listed)
	}

	// Update via the handler.
	updateBody := bytes.NewBufferString(`{"favorite": true}`)
	updateReq := httptest.NewRequestWithContext(
		context.Background(), "PUT", "/api/v1/labels/"+created.UID, updateBody,
	)
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq = requestWithChiParams(updateReq, map[string]string{"uid": created.UID})
	updateRec := httptest.NewRecorder()
	h.Update(updateRec, updateReq)
	assertStatusCode(t, updateRec, http.StatusOK)

	// Delete via the handler.
	deleteBody := bytes.NewBufferString(`{"uids": ["` + created.UID + `"]}`)
	deleteReq := httptest.NewRequestWithContext(
		context.Background(), "DELETE", "/api/v1/labels", deleteBody,
	)
	deleteReq.Header.Set("Content-Type", "application/json")
	deleteRec := httptest.NewRecorder()
	h.BatchDelete(deleteRec, deleteReq)
	assertStatusCode(t, deleteRec, http.StatusOK)

	// Final GET should 404.
	getReq := httptest.NewRequestWithContext(
		context.Background(), "GET", "/api/v1/labels/"+created.UID, nil,
	)
	getReq = requestWithChiParams(getReq, map[string]string{"uid": created.UID})
	getRec := httptest.NewRecorder()
	h.Get(getRec, getReq)
	assertStatusCode(t, getRec, http.StatusNotFound)
}
