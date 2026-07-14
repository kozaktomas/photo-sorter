package handlers

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/mock"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// --- fixtures ---

// trickyVector returns float32 values chosen to break a lossy encoder: signed
// zeros, a repeating binary fraction, a value with no exact decimal form, the
// largest finite float32, and the smallest denormal. If these survive a round
// trip bit-for-bit, ordinary embedding components do too.
func trickyVector() []float32 {
	return []float32{
		0,
		float32(math.Copysign(0, -1)),
		1,
		-1,
		1.0 / 3.0,
		0.1,
		-0.30000001192092896,
		1e-7,
		math.MaxFloat32,
		-math.MaxFloat32,
		math.SmallestNonzeroFloat32,
		-math.SmallestNonzeroFloat32,
	}
}

// smallVector is a cheap stand-in for a real 768-dim embedding in the tests
// that care about pagination rather than payload fidelity.
func smallVector(seed int) []float32 {
	return []float32{float32(seed), float32(seed) + 0.5}
}

// seedEmbeddings inserts n embeddings with sortable UIDs (emb000, emb001, …)
// and returns those UIDs in ascending order — the exact order the feed must
// walk them in.
func seedEmbeddings(reader *mock.MockEmbeddingReader, n int) []string {
	uids := make([]string, 0, n)
	for i := range n {
		uid := fmt.Sprintf("emb%03d", i)
		reader.AddEmbedding(database.StoredEmbedding{
			PhotoUID:   uid,
			Embedding:  smallVector(i),
			Model:      "ViT-L-14",
			Pretrained: "laion2b_s32b_b82k",
			Dim:        len(smallVector(i)),
		})
		uids = append(uids, uid)
	}
	return uids
}

// seedFaces inserts n faces spread across a handful of photos, with ids 1..n
// assigned in insertion order — mirroring the BIGSERIAL the feed keysets on.
func seedFaces(reader *mock.MockFaceReader, n int) []int64 {
	const photosPerBatch = 3
	byPhoto := map[string][]database.StoredFace{}
	ids := make([]int64, 0, n)

	for i := range n {
		photoUID := fmt.Sprintf("photo%d", i%photosPerBatch)
		id := int64(i + 1)
		byPhoto[photoUID] = append(byPhoto[photoUID], database.StoredFace{
			ID:        id,
			PhotoUID:  photoUID,
			FaceIndex: len(byPhoto[photoUID]),
			Embedding: smallVector(i),
			BBox:      []float64{10, 20, 100, 150},
			DetScore:  0.9,
			Model:     "buffalo_l",
			Dim:       len(smallVector(i)),
		})
		ids = append(ids, id)
	}
	for photoUID, faces := range byPhoto {
		reader.AddFaces(photoUID, faces)
	}
	return ids
}

// --- drivers ---

func newVectorsHandler(
	embeddings database.EmbeddingExportReader, faces database.FaceExportReader,
) *VectorsHandler {
	return NewVectorsHandler(embeddings, faces)
}

func getEmbeddingFeed(t *testing.T, h *VectorsHandler, query string) EmbeddingFeedResponse {
	t.Helper()
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/embeddings?"+query, nil)
	rec := httptest.NewRecorder()
	h.ListEmbeddings(rec, req)
	assertStatusCode(t, rec, http.StatusOK)

	var resp EmbeddingFeedResponse
	parseJSONResponse(t, rec, &resp)
	return resp
}

func getFaceFeed(t *testing.T, h *VectorsHandler, query string) FaceFeedResponse {
	t.Helper()
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/faces?"+query, nil)
	rec := httptest.NewRecorder()
	h.ListFaces(rec, req)
	assertStatusCode(t, rec, http.StatusOK)

	var resp FaceFeedResponse
	parseJSONResponse(t, rec, &resp)
	return resp
}

// decodeVectorB64 is the consumer side of encodeVectorB64: base64 → 4-byte
// little-endian IEEE-754 float32 components. Kukátko's importer does exactly
// this, so the test doing it independently (rather than calling a shared
// helper) is the point — it proves the wire format is self-describing.
func decodeVectorB64(t *testing.T, s string) []float32 {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("decode base64 vector: %v", err)
	}
	if len(raw)%4 != 0 {
		t.Fatalf("base64 vector is %d bytes, not a whole number of float32s", len(raw))
	}
	out := make([]float32, len(raw)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return out
}

// assertVectorsBitIdentical compares two float32 slices by their raw bits, not
// by ==. Only bit equality proves nothing was lost: 0 == -0 is true, and two
// values a hair apart still compare unequal without telling you by how much.
func assertVectorsBitIdentical(t *testing.T, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("vector length: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		gotBits, wantBits := math.Float32bits(got[i]), math.Float32bits(want[i])
		if gotBits != wantBits {
			t.Errorf("component %d: got %v (bits %#08x), want %v (bits %#08x)",
				i, got[i], gotBits, want[i], wantBits)
		}
	}
}

// --- embeddings feed: the keyset walk ---

// TestVectorsHandler_ListEmbeddings_WalkCoversEveryRowOnce paginates the whole
// feed and asserts the union of the pages is exactly the table: every
// embedding seen once, none missed, none repeated. This is the guarantee a
// migration lives or dies on — a skipped page means a photo whose vectors have
// to be recomputed on a GPU that may not exist any more.
func TestVectorsHandler_ListEmbeddings_WalkCoversEveryRowOnce(t *testing.T) {
	reader := mock.NewMockEmbeddingReader()
	const total = 23
	want := seedEmbeddings(reader, total)
	h := newVectorsHandler(reader, nil)

	seen := map[string]int{}
	var order []string
	after := ""
	pages := 0

	for {
		resp := getEmbeddingFeed(t, h, "limit=5&after="+after)
		pages++
		if pages > total+2 {
			t.Fatal("feed did not terminate: cursor is not advancing")
		}
		if resp.Total != total {
			t.Errorf("total: got %d, want %d (it must ignore ?after=)", resp.Total, total)
		}
		for _, item := range resp.Embeddings {
			seen[item.PhotoUID]++
			order = append(order, item.PhotoUID)
		}
		if resp.NextAfter == nil {
			break
		}
		if *resp.NextAfter != resp.Embeddings[len(resp.Embeddings)-1].PhotoUID {
			t.Fatalf("next_after %q is not the last row of the page", *resp.NextAfter)
		}
		after = *resp.NextAfter
	}

	if len(seen) != total {
		t.Errorf("saw %d distinct embeddings, want %d", len(seen), total)
	}
	for _, uid := range want {
		switch seen[uid] {
		case 1:
		case 0:
			t.Errorf("embedding %s was never returned", uid)
		default:
			t.Errorf("embedding %s was returned %d times", uid, seen[uid])
		}
	}
	// The order must be the stable photo_uid ascending one: that is what makes
	// ?after= a resume point rather than a guess.
	for i := range order {
		if order[i] != want[i] {
			t.Fatalf("position %d: got %s, want %s (feed is not photo_uid-ordered)", i, order[i], want[i])
		}
	}
}

// TestVectorsHandler_ListEmbeddings_ResumesFromCursor checks the property the
// walk above depends on: handing back a cursor from the middle of the table
// yields exactly the rows after it, so an export killed at page 3 does not
// have to restart at page 1.
func TestVectorsHandler_ListEmbeddings_ResumesFromCursor(t *testing.T) {
	reader := mock.NewMockEmbeddingReader()
	uids := seedEmbeddings(reader, 10)
	h := newVectorsHandler(reader, nil)

	resp := getEmbeddingFeed(t, h, "limit=4&after="+uids[5])

	if len(resp.Embeddings) != 4 {
		t.Fatalf("page size: got %d, want 4", len(resp.Embeddings))
	}
	for i, item := range resp.Embeddings {
		if want := uids[6+i]; item.PhotoUID != want {
			t.Errorf("row %d: got %s, want %s", i, item.PhotoUID, want)
		}
	}
}

// TestVectorsHandler_ListEmbeddings_LimitIsCappedAndCursorTracksTheCap pins the
// interaction that silently truncates an export when it goes wrong: the client
// asks for a huge page, the server clamps it, and the "was this page full?"
// check must be made against the *clamped* size. Comparing against the
// requested size would read the full page as short, mint no cursor, and end
// the export after one page.
func TestVectorsHandler_ListEmbeddings_LimitIsCappedAndCursorTracksTheCap(t *testing.T) {
	reader := mock.NewMockEmbeddingReader()
	total := database.MaxEmbeddingExportLimit + 3
	uids := seedEmbeddings(reader, total)
	h := newVectorsHandler(reader, nil)

	first := getEmbeddingFeed(t, h, "limit=100000")

	if first.Limit != database.MaxEmbeddingExportLimit {
		t.Errorf("limit: got %d, want the cap %d", first.Limit, database.MaxEmbeddingExportLimit)
	}
	if len(first.Embeddings) != database.MaxEmbeddingExportLimit {
		t.Fatalf("page size: got %d, want the cap %d",
			len(first.Embeddings), database.MaxEmbeddingExportLimit)
	}
	if first.NextAfter == nil {
		t.Fatal("a full page must mint a cursor, otherwise the export stops here")
	}

	second := getEmbeddingFeed(t, h, "limit=100000&after="+*first.NextAfter)
	if len(second.Embeddings) != 3 {
		t.Fatalf("second page: got %d rows, want the remaining 3", len(second.Embeddings))
	}
	if second.NextAfter != nil {
		t.Errorf("a short page must not mint a cursor, got %q", *second.NextAfter)
	}
	if last := second.Embeddings[2].PhotoUID; last != uids[total-1] {
		t.Errorf("last row: got %s, want %s", last, uids[total-1])
	}
}

// TestVectorsHandler_ListEmbeddings_EmptyTable asserts the terminating case:
// no rows, no cursor, and an embeddings array rather than a JSON null.
func TestVectorsHandler_ListEmbeddings_EmptyTable(t *testing.T) {
	h := newVectorsHandler(mock.NewMockEmbeddingReader(), nil)

	resp := getEmbeddingFeed(t, h, "")

	if len(resp.Embeddings) != 0 {
		t.Errorf("got %d embeddings from an empty table", len(resp.Embeddings))
	}
	if resp.NextAfter != nil {
		t.Errorf("empty page minted a cursor: %q", *resp.NextAfter)
	}
	if resp.Total != 0 {
		t.Errorf("total: got %d, want 0", resp.Total)
	}
}

// --- embeddings feed: payload fidelity ---

// TestVectorsHandler_ListEmbeddings_JSONRoundTripIsLossless proves the default
// number-array encoding does not quietly cost precision. Go prints a float32
// with the shortest decimal that reparses to the same float32, so the trip
// survives — but only if both ends stay in float32. This test is the guard on
// that, since a client decoding into float64 would see the drift instead.
func TestVectorsHandler_ListEmbeddings_JSONRoundTripIsLossless(t *testing.T) {
	reader := mock.NewMockEmbeddingReader()
	want := trickyVector()
	reader.AddEmbedding(database.StoredEmbedding{
		PhotoUID: "p1", Embedding: want,
		Model: "ViT-L-14", Pretrained: "laion2b_s32b_b82k", Dim: len(want),
	})
	h := newVectorsHandler(reader, nil)

	resp := getEmbeddingFeed(t, h, "")

	if len(resp.Embeddings) != 1 {
		t.Fatalf("got %d embeddings, want 1", len(resp.Embeddings))
	}
	item := resp.Embeddings[0]
	if item.EmbeddingB64 != "" {
		t.Error("json encoding must not also emit embedding_b64")
	}
	assertVectorsBitIdentical(t, item.Embedding, want)

	// The model triple rides along on every row: without it an importer cannot
	// prove the vectors match the model it will query with.
	if item.Model != "ViT-L-14" || item.Pretrained != "laion2b_s32b_b82k" || item.Dim != len(want) {
		t.Errorf("model triple: got (%s, %s, %d)", item.Model, item.Pretrained, item.Dim)
	}
}

// TestVectorsHandler_ListEmbeddings_Base64RoundTripIsLossless does the same for
// the compact encoding, decoding the payload the way a consumer would rather
// than by calling back into the encoder.
func TestVectorsHandler_ListEmbeddings_Base64RoundTripIsLossless(t *testing.T) {
	reader := mock.NewMockEmbeddingReader()
	want := trickyVector()
	reader.AddEmbedding(database.StoredEmbedding{
		PhotoUID: "p1", Embedding: want, Model: "ViT-L-14", Dim: len(want),
	})
	h := newVectorsHandler(reader, nil)

	resp := getEmbeddingFeed(t, h, "encoding=base64")

	item := resp.Embeddings[0]
	if item.Embedding != nil {
		t.Error("base64 encoding must not also emit the number array")
	}
	if resp.Encoding != "base64" {
		t.Errorf("encoding echo: got %q, want base64", resp.Encoding)
	}
	got := decodeVectorB64(t, item.EmbeddingB64)
	assertVectorsBitIdentical(t, got, want)
}

// --- feed input validation ---

func TestVectorsHandler_ListEmbeddings_RejectsBadInput(t *testing.T) {
	h := newVectorsHandler(mock.NewMockEmbeddingReader(), mock.NewMockFaceReader())

	cases := []struct {
		name  string
		query string
	}{
		// A misspelled encoding must not silently fall back to numbers: a
		// client expecting bytes would decode 9 KB of digits as a vector.
		{"unknown encoding", "encoding=float32"},
		{"non-numeric limit", "limit=all"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(
				context.Background(), http.MethodGet, "/api/v1/embeddings?"+tc.query, nil)
			rec := httptest.NewRecorder()
			h.ListEmbeddings(rec, req)
			assertStatusCode(t, rec, http.StatusBadRequest)
		})
	}
}

func TestVectorsHandler_ListFaces_RejectsBadAfter(t *testing.T) {
	h := newVectorsHandler(nil, mock.NewMockFaceReader())

	for _, query := range []string{"after=abc", "after=-1"} {
		t.Run(query, func(t *testing.T) {
			req := httptest.NewRequestWithContext(
				context.Background(), http.MethodGet, "/api/v1/faces?"+query, nil)
			rec := httptest.NewRecorder()
			h.ListFaces(rec, req)
			assertStatusCode(t, rec, http.StatusBadRequest)
		})
	}
}

func TestVectorsHandler_Unavailable(t *testing.T) {
	h := newVectorsHandler(nil, nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/embeddings", nil)
	rec := httptest.NewRecorder()
	h.ListEmbeddings(rec, req)
	assertStatusCode(t, rec, http.StatusServiceUnavailable)

	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/faces", nil)
	rec = httptest.NewRecorder()
	h.ListFaces(rec, req)
	assertStatusCode(t, rec, http.StatusServiceUnavailable)
}

func TestVectorsHandler_ListEmbeddings_RepositoryError(t *testing.T) {
	reader := mock.NewMockEmbeddingReader()
	reader.CountError = errors.New("boom")
	seedEmbeddings(reader, 2)
	h := newVectorsHandler(reader, nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/embeddings", nil)
	rec := httptest.NewRecorder()
	h.ListEmbeddings(rec, req)

	assertStatusCode(t, rec, http.StatusInternalServerError)
}

// --- faces feed ---

// TestVectorsHandler_ListFaces_WalkCoversEveryRowOnce is the faces-side twin of
// the embedding walk: paginate the whole table by id and assert every row is
// seen exactly once, in ascending id order.
func TestVectorsHandler_ListFaces_WalkCoversEveryRowOnce(t *testing.T) {
	reader := mock.NewMockFaceReader()
	const total = 17
	wantIDs := seedFaces(reader, total)
	h := newVectorsHandler(nil, reader)

	seen := map[int64]int{}
	var order []int64
	after := "0"
	pages := 0

	for {
		resp := getFaceFeed(t, h, "limit=4&after="+after)
		pages++
		if pages > total+2 {
			t.Fatal("feed did not terminate: cursor is not advancing")
		}
		if resp.Total != total {
			t.Errorf("total: got %d, want %d", resp.Total, total)
		}
		for _, item := range resp.Faces {
			seen[item.ID]++
			order = append(order, item.ID)
		}
		if resp.NextAfter == nil {
			break
		}
		after = strconv.FormatInt(*resp.NextAfter, 10)
	}

	if len(seen) != total {
		t.Errorf("saw %d distinct faces, want %d", len(seen), total)
	}
	for _, id := range wantIDs {
		if seen[id] != 1 {
			t.Errorf("face %d was returned %d times, want exactly 1", id, seen[id])
		}
	}
	for i := 1; i < len(order); i++ {
		if order[i] <= order[i-1] {
			t.Fatalf("ids out of order at %d: %d after %d", i, order[i], order[i-1])
		}
	}
}

// TestVectorsHandler_ListFaces_PayloadCarriesEveryColumn asserts the feed hands
// back the faces row verbatim — an importer that has to re-derive bbox,
// det_score, or the marker/subject linkage from somewhere else has not really
// been given the table.
func TestVectorsHandler_ListFaces_PayloadCarriesEveryColumn(t *testing.T) {
	reader := mock.NewMockFaceReader()
	want := trickyVector()
	reader.AddFaces("photo1", []database.StoredFace{{
		ID: 7, PhotoUID: "photo1", FaceIndex: 2,
		Embedding: want,
		BBox:      []float64{10, 20, 100, 150},
		DetScore:  0.9137,
		Model:     "buffalo_l", Dim: len(want),
		MarkerUID: "mkr1", SubjectUID: "subj1", SubjectName: "Jan Novák",
		PhotoWidth: 1920, PhotoHeight: 1080, Orientation: 6, FileUID: "file1",
	}})
	h := newVectorsHandler(nil, reader)

	resp := getFaceFeed(t, h, "")

	if len(resp.Faces) != 1 {
		t.Fatalf("got %d faces, want 1", len(resp.Faces))
	}
	got := resp.Faces[0]
	assertVectorsBitIdentical(t, got.Embedding, want)

	if got.ID != 7 || got.PhotoUID != "photo1" || got.FaceIndex != 2 {
		t.Errorf("identity: got (%d, %s, %d)", got.ID, got.PhotoUID, got.FaceIndex)
	}
	if got.DetScore != 0.9137 || len(got.BBox) != 4 || got.BBox[3] != 150 {
		t.Errorf("detection: det_score=%v bbox=%v", got.DetScore, got.BBox)
	}
	if got.Model != "buffalo_l" || got.Dim != len(want) {
		t.Errorf("model: got (%s, %d)", got.Model, got.Dim)
	}
	// marker_uid + subject_uid are the whole point of shipping the row rather
	// than just the vector: they are what rebuilds person identity on import.
	if got.MarkerUID != "mkr1" || got.SubjectUID != "subj1" || got.SubjectName != "Jan Novák" {
		t.Errorf("identity linkage: got (%s, %s, %s)", got.MarkerUID, got.SubjectUID, got.SubjectName)
	}
	if got.PhotoWidth != 1920 || got.PhotoHeight != 1080 || got.Orientation != 6 || got.FileUID != "file1" {
		t.Errorf("cached photo info: got (%d, %d, %d, %s)",
			got.PhotoWidth, got.PhotoHeight, got.Orientation, got.FileUID)
	}
}

func TestVectorsHandler_ListFaces_Base64RoundTripIsLossless(t *testing.T) {
	reader := mock.NewMockFaceReader()
	want := trickyVector()
	reader.AddFaces("photo1", []database.StoredFace{{
		ID: 1, PhotoUID: "photo1", Embedding: want, Model: "buffalo_l", Dim: len(want),
		BBox: []float64{1, 2, 3, 4},
	}})
	h := newVectorsHandler(nil, reader)

	resp := getFaceFeed(t, h, "encoding=base64")

	item := resp.Faces[0]
	if item.Embedding != nil {
		t.Error("base64 encoding must not also emit the number array")
	}
	assertVectorsBitIdentical(t, decodeVectorB64(t, item.EmbeddingB64), want)
}

// --- per-photo spot checks ---

func TestVectorsHandler_GetPhotoEmbedding(t *testing.T) {
	reader := mock.NewMockEmbeddingReader()
	want := trickyVector()
	reader.AddEmbedding(database.StoredEmbedding{
		PhotoUID: "photo1", Embedding: want,
		Model: "ViT-L-14", Pretrained: "laion2b_s32b_b82k", Dim: len(want),
	})
	h := newVectorsHandler(reader, nil)

	t.Run("json", func(t *testing.T) {
		rec := getPhotoEmbedding(t, h, "photo1", "")
		assertStatusCode(t, rec, http.StatusOK)

		var item EmbeddingItem
		parseJSONResponse(t, rec, &item)
		assertVectorsBitIdentical(t, item.Embedding, want)
		if item.Pretrained != "laion2b_s32b_b82k" {
			t.Errorf("pretrained: got %q", item.Pretrained)
		}
	})

	t.Run("base64", func(t *testing.T) {
		rec := getPhotoEmbedding(t, h, "photo1", "encoding=base64")
		assertStatusCode(t, rec, http.StatusOK)

		var item EmbeddingItem
		parseJSONResponse(t, rec, &item)
		assertVectorsBitIdentical(t, decodeVectorB64(t, item.EmbeddingB64), want)
	})

	// A photo with no embedding is a 404 rather than a zero vector: "not
	// embedded yet" and "embedded to all zeros" must not look alike.
	t.Run("unknown photo", func(t *testing.T) {
		rec := getPhotoEmbedding(t, h, "nope", "")
		assertStatusCode(t, rec, http.StatusNotFound)
	})
}

func getPhotoEmbedding(
	t *testing.T, h *VectorsHandler, uid, query string,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/v1/photos/"+uid+"/embedding?"+query, nil)
	req = requestWithChiParams(req, map[string]string{"uid": uid})
	rec := httptest.NewRecorder()
	h.GetPhotoEmbedding(rec, req)
	return rec
}

// --- per-photo face vectors ---

// facesHandlerWithVectors wires the minimum FacesHandler needed to exercise
// GET /photos/{uid}/faces against an in-memory face store.
func facesHandlerWithVectors(t *testing.T, faces []database.StoredFace) *FacesHandler {
	t.Helper()
	photoReader := newFakePhotoReader()
	seedFacePhoto(photoReader, "photo1", 1920, 1080, 1)

	faceReader := mock.NewMockFaceReader()
	faceReader.AddFaces("photo1", faces)

	return &FacesHandler{
		config:         testConfig(),
		sessionManager: middleware.NewSessionManager("test-secret", nil),
		faceReader:     faceReader,
		photoReader:    photoReader,
	}
}

func getPhotoFaces(t *testing.T, h *FacesHandler, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/v1/photos/photo1/faces?"+query, nil)
	req = requestWithChiParams(req, map[string]string{"uid": "photo1"})
	rec := httptest.NewRecorder()
	h.GetPhotoFaces(rec, req)
	return rec
}

// TestFacesHandler_GetPhotoFaces_IncludeEmbeddings covers the per-photo spot
// check: the vectors appear only when asked for, in the requested encoding,
// and the UI's default response keeps its current (vector-free) shape.
func TestFacesHandler_GetPhotoFaces_IncludeEmbeddings(t *testing.T) {
	want := trickyVector()
	h := facesHandlerWithVectors(t, []database.StoredFace{{
		ID: 1, PhotoUID: "photo1", FaceIndex: 0,
		Embedding: want, BBox: []float64{192, 108, 384, 270}, DetScore: 0.95,
		Model: "buffalo_l", Dim: len(want),
	}})

	t.Run("absent by default", func(t *testing.T) {
		rec := getPhotoFaces(t, h, "")
		assertStatusCode(t, rec, http.StatusOK)

		var resp PhotoFacesResponse
		parseJSONResponse(t, rec, &resp)
		if len(resp.Faces) != 1 {
			t.Fatalf("got %d faces, want 1", len(resp.Faces))
		}
		if resp.Faces[0].Embedding != nil || resp.Faces[0].EmbeddingB64 != "" {
			t.Error("the UI path must not pay for 512 floats it never reads")
		}
	})

	t.Run("json", func(t *testing.T) {
		rec := getPhotoFaces(t, h, "include_embeddings=true")
		assertStatusCode(t, rec, http.StatusOK)

		var resp PhotoFacesResponse
		parseJSONResponse(t, rec, &resp)
		face := resp.Faces[0]
		assertVectorsBitIdentical(t, face.Embedding, want)
		if face.Model != "buffalo_l" || face.Dim != len(want) {
			t.Errorf("model triple: got (%s, %d)", face.Model, face.Dim)
		}
	})

	t.Run("base64", func(t *testing.T) {
		rec := getPhotoFaces(t, h, "include_embeddings=true&encoding=base64")
		assertStatusCode(t, rec, http.StatusOK)

		var resp PhotoFacesResponse
		parseJSONResponse(t, rec, &resp)
		face := resp.Faces[0]
		if face.Embedding != nil {
			t.Error("base64 encoding must not also emit the number array")
		}
		assertVectorsBitIdentical(t, decodeVectorB64(t, face.EmbeddingB64), want)
	})

	// A typo'd export parameter is a 400, not a silently vector-free response.
	t.Run("rejects garbage", func(t *testing.T) {
		assertStatusCode(t, getPhotoFaces(t, h, "include_embeddings=yes"), http.StatusBadRequest)
		assertStatusCode(t, getPhotoFaces(t, h, "encoding=float32"), http.StatusBadRequest)
	})
}
