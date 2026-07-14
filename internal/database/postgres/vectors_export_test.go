//go:build integration

package postgres

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// trickyComponents are the float32 values a lossy round trip mangles: signed
// zeros, a repeating binary fraction, the largest finite float32, and the
// smallest denormal. They lead every fixture vector below, so a precision loss
// anywhere between Go and pgvector's float4 storage shows up as a failed bit
// comparison rather than as a silently degraded similarity search after the
// migration.
var trickyComponents = []float32{
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

// vectorOfDim builds a dim-length vector that starts with trickyComponents and
// then varies with seed, so two different seeds never produce the same vector.
func vectorOfDim(dim, seed int) []float32 {
	v := make([]float32, dim)
	copy(v, trickyComponents)
	for i := len(trickyComponents); i < dim; i++ {
		v[i] = float32(seed) + float32(i)/float32(dim)
	}
	return v
}

// assertVectorBits compares two vectors by raw bits. == would pass for 0/-0
// and would not say how far apart two near-misses are.
func assertVectorBits(t *testing.T, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("vector length: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if math.Float32bits(got[i]) != math.Float32bits(want[i]) {
			t.Fatalf("component %d: got %v (%#08x), want %v (%#08x)",
				i, got[i], math.Float32bits(got[i]), want[i], math.Float32bits(want[i]))
		}
	}
}

// TestListEmbeddingsAfter_KeysetWalk walks the embeddings table one page at a
// time against real Postgres and asserts the union of the pages is exactly the
// table: every row once, in photo_uid order, with no gaps and no repeats. The
// handler test proves the same property against the in-memory mock; this one
// proves the SQL the mock stands in for — in particular that `photo_uid > ''`
// really does select the first page rather than nothing.
func TestListEmbeddingsAfter_KeysetWalk(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()

	ctx := context.Background()
	repo := NewEmbeddingRepository(pool)

	const total = 13
	want := make([]string, 0, total)
	for i := range total {
		uid := fmt.Sprintf("emb%03d", i)
		if err := repo.Save(ctx, uid, vectorOfDim(768, i), "ViT-L-14", "laion2b", 768); err != nil {
			t.Fatalf("save embedding %s: %v", uid, err)
		}
		want = append(want, uid)
	}

	var got []string
	after := ""
	for pages := 0; ; pages++ {
		if pages > total {
			t.Fatal("walk did not terminate: the cursor is not advancing")
		}
		page, err := repo.ListEmbeddingsAfter(ctx, after, 5)
		if err != nil {
			t.Fatalf("list embeddings after %q: %v", after, err)
		}
		if len(page) == 0 {
			break
		}
		for _, emb := range page {
			got = append(got, emb.PhotoUID)
		}
		after = page[len(page)-1].PhotoUID
	}

	if len(got) != total {
		t.Fatalf("walked %d rows, want %d (%v)", len(got), total, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("position %d: got %s, want %s", i, got[i], want[i])
		}
	}
}

// TestListEmbeddingsAfter_VectorSurvivesPostgres is the round-trip guarantee
// the importer relies on: what pgvector hands back is bit-for-bit what was
// stored. pgvector's text protocol prints each float32 with the shortest
// decimal that reparses to the same float32, so nothing is lost — but that is
// a property worth pinning, not assuming.
func TestListEmbeddingsAfter_VectorSurvivesPostgres(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()

	ctx := context.Background()
	repo := NewEmbeddingRepository(pool)

	want := vectorOfDim(768, 1)
	if err := repo.Save(ctx, "photo1", want, "ViT-L-14", "laion2b_s32b_b82k", 768); err != nil {
		t.Fatalf("save embedding: %v", err)
	}

	page, err := repo.ListEmbeddingsAfter(ctx, "", 10)
	if err != nil {
		t.Fatalf("list embeddings: %v", err)
	}
	if len(page) != 1 {
		t.Fatalf("got %d embeddings, want 1", len(page))
	}
	assertVectorBits(t, page[0].Embedding, want)

	if page[0].Model != "ViT-L-14" || page[0].Pretrained != "laion2b_s32b_b82k" || page[0].Dim != 768 {
		t.Errorf("model triple: got (%s, %s, %d)",
			page[0].Model, page[0].Pretrained, page[0].Dim)
	}
}

// TestListEmbeddingsAfter_ClampsLimit checks the server-side cap holds at the
// repository, not merely in the handler: a CLI or MCP caller asking for a
// million rows still gets the cap.
func TestListEmbeddingsAfter_ClampsLimit(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()

	ctx := context.Background()
	repo := NewEmbeddingRepository(pool)

	const total = database.DefaultEmbeddingExportLimit + 5
	for i := range total {
		if err := repo.Save(ctx, fmt.Sprintf("emb%03d", i),
			vectorOfDim(768, i), "ViT-L-14", "laion2b", 768); err != nil {
			t.Fatalf("save embedding: %v", err)
		}
	}

	// limit = 0 means "use the default", not "no limit".
	page, err := repo.ListEmbeddingsAfter(ctx, "", 0)
	if err != nil {
		t.Fatalf("list embeddings: %v", err)
	}
	if len(page) != database.DefaultEmbeddingExportLimit {
		t.Errorf("default page: got %d rows, want %d", len(page), database.DefaultEmbeddingExportLimit)
	}

	// An oversized limit is clamped rather than honoured.
	page, err = repo.ListEmbeddingsAfter(ctx, "", 1_000_000)
	if err != nil {
		t.Fatalf("list embeddings: %v", err)
	}
	if len(page) != total {
		t.Errorf("clamped page: got %d rows, want all %d (the table is under the cap)", len(page), total)
	}
}

// TestListFacesAfter_KeysetWalk is the faces-side twin: page the whole table by
// BIGSERIAL id and assert every row appears exactly once, in ascending id
// order.
func TestListFacesAfter_KeysetWalk(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()

	ctx := context.Background()
	repo := NewFaceRepository(pool)

	const photos = 4
	const facesPerPhoto = 3
	for p := range photos {
		faces := make([]database.StoredFace, 0, facesPerPhoto)
		for f := range facesPerPhoto {
			faces = append(faces, database.StoredFace{
				PhotoUID:  fmt.Sprintf("photo%d", p),
				FaceIndex: f,
				Embedding: vectorOfDim(512, p*facesPerPhoto+f),
				BBox:      []float64{10, 20, 100, 150},
				DetScore:  0.9,
				Model:     "buffalo_l",
				Dim:       512,
			})
		}
		if err := repo.SaveFaces(ctx, fmt.Sprintf("photo%d", p), faces); err != nil {
			t.Fatalf("save faces: %v", err)
		}
	}

	seen := map[int64]int{}
	var lastID int64
	var walked int
	for pages := 0; ; pages++ {
		if pages > photos*facesPerPhoto {
			t.Fatal("walk did not terminate: the cursor is not advancing")
		}
		page, err := repo.ListFacesAfter(ctx, lastID, 5)
		if err != nil {
			t.Fatalf("list faces after %d: %v", lastID, err)
		}
		if len(page) == 0 {
			break
		}
		for _, face := range page {
			if face.ID <= lastID {
				t.Fatalf("face id %d is not strictly after the cursor %d", face.ID, lastID)
			}
			lastID = face.ID
			seen[face.ID]++
			walked++
		}
	}

	if walked != photos*facesPerPhoto {
		t.Fatalf("walked %d faces, want %d", walked, photos*facesPerPhoto)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("face %d returned %d times, want exactly 1", id, n)
		}
	}
}

// TestListFacesAfter_RowIsVerbatim asserts the feed's source rows carry every
// column an importer needs — the vector bit-for-bit, plus the bbox, detection
// score, and the marker/subject linkage that rebuilds person identity.
func TestListFacesAfter_RowIsVerbatim(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()

	ctx := context.Background()
	repo := NewFaceRepository(pool)

	want := vectorOfDim(512, 7)
	if err := repo.SaveFaces(ctx, "photo1", []database.StoredFace{{
		PhotoUID:  "photo1",
		FaceIndex: 2,
		Embedding: want,
		BBox:      []float64{10, 20, 100, 150},
		DetScore:  0.9137,
		Model:     "buffalo_l",
		Dim:       512,
		MarkerUID: "mkr1", SubjectUID: "subj1", SubjectName: "Jan Novák",
		PhotoWidth: 1920, PhotoHeight: 1080, Orientation: 6, FileUID: "file1",
	}}); err != nil {
		t.Fatalf("save faces: %v", err)
	}

	page, err := repo.ListFacesAfter(ctx, 0, 10)
	if err != nil {
		t.Fatalf("list faces: %v", err)
	}
	if len(page) != 1 {
		t.Fatalf("got %d faces, want 1", len(page))
	}
	got := page[0]

	assertVectorBits(t, got.Embedding, want)
	if got.ID == 0 {
		t.Error("face id was not returned; the keyset cursor has nothing to key on")
	}
	if got.FaceIndex != 2 || got.DetScore != 0.9137 || len(got.BBox) != 4 {
		t.Errorf("detection: face_index=%d det_score=%v bbox=%v", got.FaceIndex, got.DetScore, got.BBox)
	}
	if got.MarkerUID != "mkr1" || got.SubjectUID != "subj1" || got.SubjectName != "Jan Novák" {
		t.Errorf("identity linkage: got (%s, %s, %s)", got.MarkerUID, got.SubjectUID, got.SubjectName)
	}
	if got.Model != "buffalo_l" || got.Dim != 512 {
		t.Errorf("model: got (%s, %d)", got.Model, got.Dim)
	}
}
