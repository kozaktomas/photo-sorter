package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// baseTime is the reference instant every fixture in this file hangs off, so
// updated_at ordering is explicit rather than dependent on wall-clock timing.
var baseTime = time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

// exportPhoto builds a photo with an explicit updated_at, which is the column
// the incremental export walks.
func exportPhoto(uid string, updatedAt time.Time) *database.Photo {
	p := samplePhoto(uid, "hash-"+uid, uid, baseTime)
	p.UpdatedAt = updatedAt
	p.CreatedAt = updatedAt
	return p
}

// fakeRelationReader is an in-memory database.PhotoRelationReader. It records
// the UIDs and RelationSet it was asked for, so a test can assert the handler
// batches the whole page into ONE call rather than one call per photo.
type fakeRelationReader struct {
	labels  map[string][]database.PhotoLabelRelation
	albums  map[string][]database.PhotoAlbumRelation
	markers map[string][]database.PhotoMarkerRelation
	files   map[string][]database.PhotoFile
	err     error

	calls    int
	lastUIDs []string
	lastSet  database.RelationSet
}

func newFakeRelationReader() *fakeRelationReader {
	return &fakeRelationReader{
		labels:  map[string][]database.PhotoLabelRelation{},
		albums:  map[string][]database.PhotoAlbumRelation{},
		markers: map[string][]database.PhotoMarkerRelation{},
		files:   map[string][]database.PhotoFile{},
	}
}

// LoadPhotoRelations mirrors the postgres implementation's contract: every
// requested photo gets an entry, requested relations are non-nil (possibly
// empty), unrequested ones stay nil.
func (f *fakeRelationReader) LoadPhotoRelations(
	_ context.Context, photoUIDs []string, include database.RelationSet,
) (map[string]*database.PhotoRelations, error) {
	f.calls++
	f.lastUIDs = photoUIDs
	f.lastSet = include
	if f.err != nil {
		return nil, f.err
	}

	out := make(map[string]*database.PhotoRelations, len(photoUIDs))
	for _, uid := range photoUIDs {
		rel := &database.PhotoRelations{}
		if include.Labels {
			rel.Labels = append([]database.PhotoLabelRelation{}, f.labels[uid]...)
		}
		if include.Albums {
			rel.Albums = append([]database.PhotoAlbumRelation{}, f.albums[uid]...)
		}
		if include.Markers {
			rel.Markers = append([]database.PhotoMarkerRelation{}, f.markers[uid]...)
		}
		if include.Files {
			rel.Files = append([]database.PhotoFile{}, f.files[uid]...)
		}
		out[uid] = rel
	}
	return out, nil
}

// listPhotos drives the List handler against a raw query string and decodes
// the envelope.
func listPhotos(t *testing.T, h *PhotosHandler, query string) PhotoListResponse {
	t.Helper()
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/photos?"+query, nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	assertStatusCode(t, rec, http.StatusOK)

	var resp PhotoListResponse
	parseJSONResponse(t, rec, &resp)
	return resp
}

// --- Keyset cursor ---

// TestPhotosHandler_List_CursorNoGapsNoRepeats walks a library one page at a
// time and asserts the union of the pages is exactly the library: every photo
// seen once, none missed. This is the core guarantee an export depends on.
func TestPhotosHandler_List_CursorNoGapsNoRepeats(t *testing.T) {
	reader := newFakePhotoReader()
	const total = 23
	for i := range total {
		// Deliberately give several photos an identical updated_at. That is
		// the case a naive cursor keyed on the timestamp alone gets wrong:
		// it would either skip the rest of the tied group or serve it twice.
		// The (updated_at, uid) tiebreak is what saves it.
		reader.add(exportPhoto(
			fmt.Sprintf("photo%02d", i),
			baseTime.Add(time.Duration(i/5)*time.Minute),
		))
	}
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	seen := map[string]int{}
	var order []string
	cursor := ""
	for page := range 100 {
		query := "sort=updated&limit=5"
		if cursor != "" {
			query += "&cursor=" + cursor
		}
		resp := listPhotos(t, h, query)

		if resp.Total != total {
			t.Errorf("page %d: Total = %d, want %d (the count must ignore the cursor)",
				page, resp.Total, total)
		}
		for _, p := range resp.Photos {
			seen[p.UID]++
			order = append(order, p.UID)
		}
		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}

	if len(seen) != total {
		t.Errorf("saw %d distinct photos, want %d — the walk has gaps", len(seen), total)
	}
	for uid, n := range seen {
		if n != 1 {
			t.Errorf("photo %s returned %d times, want exactly 1", uid, n)
		}
	}
	// The walk must also be monotonic in the sort order.
	for i := 1; i < len(order); i++ {
		if order[i-1] >= order[i] {
			// UIDs are zero-padded and updated_at ascends with the index, so
			// the concatenated pages must come out in ascending UID order.
			t.Errorf("order broke at %d: %q then %q", i, order[i-1], order[i])
			break
		}
	}
}

// TestPhotosHandler_List_CursorStableUnderConcurrentWrite is the scenario the
// spec calls out: a photo is modified while an export is mid-walk.
//
// Because the walk is ordered by updated_at ASCENDING and any write pushes
// updated_at to now(), a modified row moves AHEAD of the cursor. So it must
// re-appear later in the walk — never vanish from it. A client may therefore
// see it twice (harmless: an import upserts by UID), but must never miss it.
func TestPhotosHandler_List_CursorStableUnderConcurrentWrite(t *testing.T) {
	reader := newFakePhotoReader()
	for i := range 10 {
		reader.add(exportPhoto(
			fmt.Sprintf("photo%02d", i),
			baseTime.Add(time.Duration(i)*time.Minute),
		))
	}
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	// Page 1 of 5.
	first := listPhotos(t, h, "sort=updated&limit=5")
	if len(first.Photos) != 5 {
		t.Fatalf("page 1 returned %d photos, want 5", len(first.Photos))
	}
	if first.NextCursor == "" {
		t.Fatal("page 1 returned no cursor despite a full page")
	}

	// Mid-export, someone edits photo00 — a row the client has ALREADY seen.
	// Its updated_at jumps far past everything.
	touched := reader.photos["photo00"]
	touched.UpdatedAt = baseTime.Add(time.Hour)
	touched.Title = "edited mid-export"

	// Resume. Walk to the end collecting everything after the cursor.
	seen := map[string]int{}
	cursor := first.NextCursor
	for range 100 {
		resp := listPhotos(t, h, "sort=updated&limit=5&cursor="+cursor)
		for _, p := range resp.Photos {
			seen[p.UID]++
		}
		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}

	// photo05..photo09 were never seen before and must all arrive.
	for i := 5; i < 10; i++ {
		uid := fmt.Sprintf("photo%02d", i)
		if seen[uid] != 1 {
			t.Errorf("photo %s appeared %d times after resume, want 1 — the export lost a row",
				uid, seen[uid])
		}
	}
	// The row edited mid-export must resurface, carrying its new state.
	if seen["photo00"] != 1 {
		t.Errorf("photo00 was edited mid-export and appeared %d times after the cursor, want 1; "+
			"an ascending updated_at walk must re-serve it, never drop it", seen["photo00"])
	}
}

// TestPhotosHandler_List_CursorRejectsWrongSort guards against the silent
// infinite loop: a cursor under any sort but `updated` is not a valid keyset,
// and quietly ignoring it would hand the client page 1 forever.
func TestPhotosHandler_List_CursorRejectsWrongSort(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(exportPhoto("photo1", baseTime))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	cursor := database.EncodePhotoCursor(database.PhotoCursor{UpdatedAt: baseTime, UID: "photo1"})

	for _, sortKey := range []string{"", "newest", "oldest", "name"} {
		query := "cursor=" + cursor
		if sortKey != "" {
			query += "&sort=" + sortKey
		}
		req := httptest.NewRequestWithContext(
			context.Background(), http.MethodGet, "/api/v1/photos?"+query, nil)
		rec := httptest.NewRecorder()
		h.List(rec, req)

		assertStatusCode(t, rec, http.StatusBadRequest)
		assertJSONError(t, rec, "cursor requires sort=updated")
	}
}

func TestPhotosHandler_List_CursorRejectsMalformed(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(exportPhoto("photo1", baseTime))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/v1/photos?sort=updated&cursor=!!!garbage!!!", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "invalid cursor")
}

// TestPhotosHandler_List_NoCursorOnShortPage asserts the walk terminates: a
// page that comes back short means there is nothing more, so no cursor is
// handed out and the client's loop ends.
func TestPhotosHandler_List_NoCursorOnShortPage(t *testing.T) {
	reader := newFakePhotoReader()
	for i := range 3 {
		reader.add(exportPhoto(fmt.Sprintf("photo%02d", i), baseTime.Add(time.Duration(i)*time.Minute)))
	}
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	resp := listPhotos(t, h, "sort=updated&limit=10")
	if len(resp.Photos) != 3 {
		t.Fatalf("got %d photos, want 3", len(resp.Photos))
	}
	if resp.NextCursor != "" {
		t.Errorf("NextCursor = %q on a short page, want \"\" — the client would loop forever",
			resp.NextCursor)
	}
}

// TestPhotosHandler_List_NoCursorWithoutUpdatedSort documents that the cursor
// is opt-in: the UI's default listing must not start emitting one.
func TestPhotosHandler_List_NoCursorWithoutUpdatedSort(t *testing.T) {
	reader := newFakePhotoReader()
	for i := range 5 {
		reader.add(exportPhoto(fmt.Sprintf("photo%02d", i), baseTime.Add(time.Duration(i)*time.Minute)))
	}
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	resp := listPhotos(t, h, "limit=5")
	if resp.NextCursor != "" {
		t.Errorf("NextCursor = %q for the default sort, want \"\"", resp.NextCursor)
	}
}

// --- updated_since ---

func TestPhotosHandler_List_UpdatedSince(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(exportPhoto("old", baseTime))
	reader.add(exportPhoto("boundary", baseTime.Add(time.Hour)))
	reader.add(exportPhoto("new", baseTime.Add(2*time.Hour)))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	since := baseTime.Add(time.Hour).UTC().Format(time.RFC3339)
	resp := listPhotos(t, h, "sort=updated&updated_since="+since)

	got := map[string]bool{}
	for _, p := range resp.Photos {
		got[p.UID] = true
	}
	// The bound is inclusive: a client that stores "the newest updated_at I
	// have" and passes it back must re-receive that row, not skip it.
	if !got["boundary"] {
		t.Error("the row exactly at updated_since was excluded; the bound must be >=, not >")
	}
	if !got["new"] {
		t.Error("a row after updated_since was excluded")
	}
	if got["old"] {
		t.Error("a row before updated_since was included")
	}
	if resp.Total != 2 {
		t.Errorf("Total = %d, want 2", resp.Total)
	}
}

func TestPhotosHandler_List_UpdatedSinceInvalid(t *testing.T) {
	reader := newFakePhotoReader()
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/v1/photos?updated_since=yesterday", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "invalid updated_since")
}

// --- Payload completeness ---

// TestPhotosHandler_Get_PayloadCompleteness pins every column the spec named
// as missing. If someone adds a column to photos and forgets the wire mapping,
// this is what should fail.
func TestPhotosHandler_Get_PayloadCompleteness(t *testing.T) {
	lat, lng, alt := 49.35, 16.72, 412.5
	iso := 400
	aperture, focal := 2.8, 35.0
	archived := baseTime.Add(-24 * time.Hour)

	photo := exportPhoto("photo1", baseTime)
	photo.FilePath = "2024/06/photo1.jpg"
	photo.FileSize = 4_194_304
	photo.FileMime = "image/jpeg"
	photo.FileOrientation = 6
	photo.TakenAtSource = "exif"
	photo.Notes = "Analyzed by: gpt-4.1-mini"
	photo.Lat, photo.Lng, photo.Altitude = &lat, &lng, &alt
	photo.CameraMake = "Canon"
	photo.CameraModel = "EOS R6"
	photo.LensModel = "RF 35mm F1.8"
	photo.ISO = &iso
	photo.Aperture = &aperture
	photo.Exposure = "1/250"
	photo.FocalLength = &focal
	photo.Exif = map[string]any{"Make": "Canon", "Flash": "off"}
	photo.UploadedBy = "u123"
	photo.ArchivedAt = &archived

	reader := newFakePhotoReader()
	reader.add(photo)
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/v1/photos/photo1?include_archived=true", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "photo1"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	assertStatusCode(t, rec, http.StatusOK)

	var got PhotoResponse
	parseJSONResponse(t, rec, &got)

	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"file_path", got.FilePath, "2024/06/photo1.jpg"},
		{"file_size", got.FileSize, int64(4_194_304)},
		{"file_mime", got.FileMime, "image/jpeg"},
		{"file_orientation", got.FileOrientation, 6},
		{"taken_at_source", got.TakenAtSource, "exif"},
		{"notes", got.Notes, "Analyzed by: gpt-4.1-mini"},
		{"camera_make", got.CameraMake, "Canon"},
		{"lens_model", got.LensModel, "RF 35mm F1.8"},
		{"exposure", got.Exposure, "1/250"},
		{"uploaded_by", got.UploadedBy, "u123"},
		{"hash", got.Hash, "hash-photo1"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.field, c.got, c.want)
		}
	}

	assertFloatPtr(t, "lat", got.Lat, lat)
	assertFloatPtr(t, "lng", got.Lng, lng)
	assertFloatPtr(t, "altitude", got.Altitude, alt)
	assertFloatPtr(t, "aperture", got.Aperture, aperture)
	assertFloatPtr(t, "focal_length", got.FocalLength, focal)

	if got.ISO == nil || *got.ISO != iso {
		t.Errorf("iso = %v, want %d", got.ISO, iso)
	}
	if got.Exif["Make"] != "Canon" {
		t.Errorf("exif = %v, want the full JSONB blob", got.Exif)
	}
	if got.ArchivedAt == nil || *got.ArchivedAt != archived.UTC().Format(time.RFC3339) {
		t.Errorf("archived_at = %v, want %s", got.ArchivedAt, archived.UTC().Format(time.RFC3339))
	}
	if got.CreatedAt == "" || got.UpdatedAt == "" {
		t.Errorf("created_at/updated_at must be on the wire, got %q / %q", got.CreatedAt, got.UpdatedAt)
	}
	if got.UpdatedAt != baseTime.UTC().Format(time.RFC3339) {
		t.Errorf("updated_at = %q, want %q", got.UpdatedAt, baseTime.UTC().Format(time.RFC3339))
	}
}

// TestPhotosHandler_Get_NullGPSIsNull is the bug the spec called out: lat/lng
// used to serialise as 0.0 when NULL, making "no GPS" indistinguishable from
// a photo taken at the intersection of the equator and the prime meridian.
func TestPhotosHandler_Get_NullGPSIsNull(t *testing.T) {
	photo := exportPhoto("photo1", baseTime)
	photo.Lat, photo.Lng, photo.Altitude = nil, nil, nil

	reader := newFakePhotoReader()
	reader.add(photo)
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/v1/photos/photo1", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "photo1"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	assertStatusCode(t, rec, http.StatusOK)

	var got PhotoResponse
	parseJSONResponse(t, rec, &got)

	if got.Lat != nil {
		t.Errorf("lat = %v for a photo with no GPS, want null", *got.Lat)
	}
	if got.Lng != nil {
		t.Errorf("lng = %v for a photo with no GPS, want null", *got.Lng)
	}
	if got.Altitude != nil {
		t.Errorf("altitude = %v for a photo with no GPS, want null", *got.Altitude)
	}
	// And it must be literal JSON null, not an omitted field.
	if body := rec.Body.String(); !strings.Contains(body, `"lat":null`) {
		t.Errorf("body does not carry `\"lat\":null`: %s", body)
	}
}

// --- ?include= relations ---

func TestPhotosHandler_List_IncludeRelations(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(exportPhoto("photo1", baseTime))
	reader.add(exportPhoto("photo2", baseTime.Add(time.Minute)))

	rels := newFakeRelationReader()
	rels.labels["photo1"] = []database.PhotoLabelRelation{
		{UID: "l1", Name: "svatba", Source: "ai", Uncertainty: 20},
	}
	rels.albums["photo1"] = []database.PhotoAlbumRelation{{UID: "a1", Title: "Veselice 1998"}}
	rels.markers["photo1"] = []database.PhotoMarkerRelation{
		{UID: "m1", SubjectUID: "s1", Type: "face", X: 0.1, Y: 0.2, W: 0.3, H: 0.4,
			Score: 90, Invalid: false, Reviewed: true},
	}
	rels.files["photo1"] = []database.PhotoFile{
		{PhotoUID: "photo1", FilePath: "2024/06/photo1.jpg", FileHash: "h1",
			FileSize: 100, FileMime: "image/jpeg", IsPrimary: true, Role: "original"},
		{PhotoUID: "photo1", FilePath: "2024/06/photo1.cr2", FileHash: "h2",
			FileSize: 200, FileMime: "image/x-canon-cr2", IsPrimary: false, Role: "sidecar"},
	}

	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))
	h.relations = rels

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/v1/photos?sort=updated&include=labels,albums,markers,files", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	assertStatusCode(t, rec, http.StatusOK)

	var resp PhotoListResponse
	parseJSONResponse(t, rec, &resp)
	if len(resp.Photos) != 2 {
		t.Fatalf("got %d photos, want 2", len(resp.Photos))
	}

	// One batched call for the whole page — not one per photo.
	if rels.calls != 1 {
		t.Errorf("LoadPhotoRelations called %d times for a 2-photo page, want 1 (N+1 regression)",
			rels.calls)
	}
	if len(rels.lastUIDs) != 2 {
		t.Errorf("relation reader got %d UIDs, want 2", len(rels.lastUIDs))
	}
	want := database.RelationSet{Labels: true, Albums: true, Markers: true, Files: true}
	if rels.lastSet != want {
		t.Errorf("RelationSet = %+v, want %+v", rels.lastSet, want)
	}

	byUID := map[string]PhotoResponse{}
	for _, p := range resp.Photos {
		byUID[p.UID] = p
	}

	p1 := byUID["photo1"]
	if p1.Labels == nil || len(*p1.Labels) != 1 {
		t.Fatalf("photo1 labels = %v, want 1", p1.Labels)
	}
	label := (*p1.Labels)[0]
	if label.Name != "svatba" || label.Source != "ai" || label.Uncertainty != 20 {
		t.Errorf("photo1 label = %+v, want svatba/ai/20 (provenance must survive)", label)
	}
	if p1.Albums == nil || len(*p1.Albums) != 1 || (*p1.Albums)[0].Title != "Veselice 1998" {
		t.Errorf("photo1 albums = %v", p1.Albums)
	}
	if p1.Markers == nil || len(*p1.Markers) != 1 {
		t.Fatalf("photo1 markers = %v, want 1", p1.Markers)
	}
	marker := (*p1.Markers)[0]
	// subject_uid is the whole point of the markers expansion.
	if marker.SubjectUID != "s1" {
		t.Errorf("marker subject_uid = %q, want %q — identity cannot be rebuilt without it",
			marker.SubjectUID, "s1")
	}
	if !marker.Reviewed || marker.Invalid {
		t.Errorf("marker flags = reviewed:%v invalid:%v, want reviewed:true invalid:false",
			marker.Reviewed, marker.Invalid)
	}
	if p1.Files == nil || len(*p1.Files) != 2 {
		t.Fatalf("photo1 files = %v, want 2", p1.Files)
	}
	if !(*p1.Files)[0].IsPrimary || (*p1.Files)[1].Role != "sidecar" {
		t.Errorf("photo1 files = %+v, want the primary then the sidecar", *p1.Files)
	}

	// photo2 has no relations, but they WERE requested — so each must render
	// as an empty list, NOT as an absent field. An importer reading an absent
	// field would leave its local labels alone; reading [] it clears them.
	// A plain `[]T` with omitempty collapses these two cases into one, which
	// is why the wire type is a pointer.
	p2 := byUID["photo2"]
	if p2.Labels == nil {
		t.Error("photo2 labels is absent, want an empty list — " +
			"\"requested but empty\" must be distinguishable from \"not requested\"")
	} else if len(*p2.Labels) != 0 {
		t.Errorf("photo2 labels = %+v, want empty", *p2.Labels)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"labels":[]`) {
		t.Errorf("body does not carry an empty `\"labels\":[]` for photo2: %s", body)
	}
}

// TestPhotosHandler_List_IncludeOmittedWhenNotRequested is the other half of
// the contract: an unrequested relation must be ABSENT from the JSON, so an
// importer can tell "no labels" from "labels not fetched" and does not wipe
// its local state on the strength of a field it never asked for.
func TestPhotosHandler_List_IncludeOmittedWhenNotRequested(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(exportPhoto("photo1", baseTime))

	rels := newFakeRelationReader()
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))
	h.relations = rels

	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/photos", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	assertStatusCode(t, rec, http.StatusOK)

	body := rec.Body.String()
	for _, field := range []string{`"labels"`, `"albums"`, `"markers"`, `"files"`} {
		if strings.Contains(body, field) {
			t.Errorf("body carries %s without ?include=; it must be omitted: %s", field, body)
		}
	}
	// And the relation reader must not even be consulted.
	if rels.calls != 0 {
		t.Errorf("LoadPhotoRelations called %d times without ?include=, want 0", rels.calls)
	}
}

func TestPhotosHandler_List_IncludeInvalid(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(exportPhoto("photo1", baseTime))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/v1/photos?include=labels,bogus", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
}

func TestPhotosHandler_List_IncludeRelationsError(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(exportPhoto("photo1", baseTime))

	rels := newFakeRelationReader()
	rels.err = errors.New("boom")
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))
	h.relations = rels

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/v1/photos?include=labels", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	assertStatusCode(t, rec, http.StatusInternalServerError)
}

// TestParseInclude covers the parser in isolation, including the tolerated
// sloppy forms.
func TestParseInclude(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    database.RelationSet
		wantErr bool
	}{
		{name: "empty", raw: "", want: database.RelationSet{}},
		{name: "single", raw: "labels", want: database.RelationSet{Labels: true}},
		{
			name: "all four",
			raw:  "labels,albums,markers,files",
			want: database.RelationSet{Labels: true, Albums: true, Markers: true, Files: true},
		},
		{
			name: "whitespace and empty segments tolerated",
			raw:  " labels , , albums ,",
			want: database.RelationSet{Labels: true, Albums: true},
		},
		{name: "repeats are idempotent", raw: "files,files", want: database.RelationSet{Files: true}},
		{name: "unknown name rejected", raw: "labels,marker", wantErr: true},
		{name: "typo rejected", raw: "label", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			got, ok := parseInclude(rec, tt.raw)
			if tt.wantErr {
				if ok {
					t.Fatalf("parseInclude(%q) succeeded, want a 400", tt.raw)
				}
				if rec.Code != http.StatusBadRequest {
					t.Errorf("status = %d, want 400", rec.Code)
				}
				return
			}
			if !ok {
				t.Fatalf("parseInclude(%q) failed, want success", tt.raw)
			}
			if got != tt.want {
				t.Errorf("parseInclude(%q) = %+v, want %+v", tt.raw, got, tt.want)
			}
		})
	}
}

// assertFloatPtr fails when a *float64 wire field is nil or not the wanted
// value.
func assertFloatPtr(t *testing.T, field string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = null, want %v", field, want)
		return
	}
	if *got != want {
		t.Errorf("%s = %v, want %v", field, *got, want)
	}
}
