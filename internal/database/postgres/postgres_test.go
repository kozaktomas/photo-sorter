//go:build integration

package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupTestContainer(t *testing.T) (*Pool, func()) {
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "pgvector/pgvector:pg16",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
			"POSTGRES_DB":       "testdb",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Skipf("Docker not available or container failed to start, skipping integration test: %v", err)
		return nil, func() {}
	}
	if container == nil {
		t.Skip("Docker not available, skipping integration test")
		return nil, func() {}
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get container host: %v", err)
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("Failed to get container port: %v", err)
	}

	dbURL := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable", host, port.Port())

	cfg := &config.DatabaseConfig{
		URL:          dbURL,
		MaxOpenConns: 5,
		MaxIdleConns: 2,
	}

	pool, err := NewPool(cfg)
	if err != nil {
		container.Terminate(ctx)
		t.Fatalf("Failed to create pool: %v", err)
	}

	// Run migrations.
	if err := pool.Migrate(ctx); err != nil {
		pool.Close()
		container.Terminate(ctx)
		t.Fatalf("Failed to run migrations: %v", err)
	}

	cleanup := func() {
		pool.Close()
		container.Terminate(ctx)
	}

	return pool, cleanup
}

func TestEmbeddingRepository(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()

	ctx := context.Background()
	repo := NewEmbeddingRepository(pool)

	// Test Save and Get.
	t.Run("SaveAndGet", func(t *testing.T) {
		embedding := make([]float32, 768)
		for i := range embedding {
			embedding[i] = float32(i) / 768.0
		}

		err := repo.Save(ctx, "photo123", embedding, "clip", "openai", 768)
		if err != nil {
			t.Fatalf("Failed to save embedding: %v", err)
		}

		got, err := repo.Get(ctx, "photo123")
		if err != nil {
			t.Fatalf("Failed to get embedding: %v", err)
		}
		if got == nil {
			t.Fatal("Expected embedding, got nil")
			return
		}
		if got.PhotoUID != "photo123" {
			t.Errorf("Expected PhotoUID 'photo123', got '%s'", got.PhotoUID)
		}
		if got.Model != "clip" {
			t.Errorf("Expected Model 'clip', got '%s'", got.Model)
		}
		if len(got.Embedding) != 768 {
			t.Errorf("Expected 768 dimensions, got %d", len(got.Embedding))
		}
	})

	// Test Has.
	t.Run("Has", func(t *testing.T) {
		has, err := repo.Has(ctx, "photo123")
		if err != nil {
			t.Fatalf("Failed to check has: %v", err)
		}
		if !has {
			t.Error("Expected true, got false")
		}

		has, err = repo.Has(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("Failed to check has: %v", err)
		}
		if has {
			t.Error("Expected false, got true")
		}
	})

	// Test Count.
	t.Run("Count", func(t *testing.T) {
		count, err := repo.Count(ctx)
		if err != nil {
			t.Fatalf("Failed to count: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected 1, got %d", count)
		}
	})

	// Test FindSimilar.
	t.Run("FindSimilar", func(t *testing.T) {
		// Add more embeddings.
		for i := 0; i < 5; i++ {
			emb := make([]float32, 768)
			for j := range emb {
				emb[j] = float32(j+i) / 768.0
			}
			repo.Save(ctx, fmt.Sprintf("photo%d", i+100), emb, "clip", "openai", 768)
		}

		query := make([]float32, 768)
		for i := range query {
			query[i] = float32(i) / 768.0
		}

		results, err := repo.FindSimilar(ctx, query, 3)
		if err != nil {
			t.Fatalf("Failed to find similar: %v", err)
		}
		if len(results) != 3 {
			t.Errorf("Expected 3 results, got %d", len(results))
		}
	})

	// Test FindSimilarWithDistance.
	t.Run("FindSimilarWithDistance", func(t *testing.T) {
		query := make([]float32, 768)
		for i := range query {
			query[i] = float32(i) / 768.0
		}

		results, distances, err := repo.FindSimilarWithDistance(ctx, query, 10, 1.0)
		if err != nil {
			t.Fatalf("Failed to find similar with distance: %v", err)
		}
		if len(results) == 0 {
			t.Error("Expected results, got none")
		}
		if len(results) != len(distances) {
			t.Errorf("Results and distances length mismatch: %d vs %d", len(results), len(distances))
		}
		// First result should be the most similar (smallest distance).
		for i := 1; i < len(distances); i++ {
			if distances[i] < distances[i-1] {
				t.Error("Distances not sorted")
			}
		}
	})
}

func TestFaceRepository(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()

	ctx := context.Background()
	repo := NewFaceRepository(pool)

	// Test SaveFaces and GetFaces.
	t.Run("SaveAndGetFaces", func(t *testing.T) {
		embedding := make([]float32, 512)
		for i := range embedding {
			embedding[i] = float32(i) / 512.0
		}

		faces := []database.StoredFace{
			{
				PhotoUID:    "photo456",
				FaceIndex:   0,
				Embedding:   embedding,
				BBox:        []float64{10, 20, 100, 150},
				DetScore:    0.95,
				Model:       "buffalo_l",
				Dim:         512,
				MarkerUID:   "marker1",
				SubjectUID:  "subject1",
				SubjectName: "John Doe",
				PhotoWidth:  1920,
				PhotoHeight: 1080,
				Orientation: 1,
				FileUID:     "file1",
			},
			{
				PhotoUID:  "photo456",
				FaceIndex: 1,
				Embedding: embedding,
				BBox:      []float64{200, 50, 300, 200},
				DetScore:  0.88,
				Model:     "buffalo_l",
				Dim:       512,
			},
		}

		err := repo.SaveFaces(ctx, "photo456", faces)
		if err != nil {
			t.Fatalf("Failed to save faces: %v", err)
		}

		got, err := repo.GetFaces(ctx, "photo456")
		if err != nil {
			t.Fatalf("Failed to get faces: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("Expected 2 faces, got %d", len(got))
		}
		if got[0].SubjectName != "John Doe" {
			t.Errorf("Expected SubjectName 'John Doe', got '%s'", got[0].SubjectName)
		}
		if got[0].PhotoWidth != 1920 {
			t.Errorf("Expected PhotoWidth 1920, got %d", got[0].PhotoWidth)
		}
	})

	// Test HasFaces.
	t.Run("HasFaces", func(t *testing.T) {
		has, err := repo.HasFaces(ctx, "photo456")
		if err != nil {
			t.Fatalf("Failed to check has faces: %v", err)
		}
		if !has {
			t.Error("Expected true, got false")
		}
	})

	// Test MarkFacesProcessed and IsFacesProcessed.
	t.Run("MarkAndCheckProcessed", func(t *testing.T) {
		err := repo.MarkFacesProcessed(ctx, "photo789", 3)
		if err != nil {
			t.Fatalf("Failed to mark processed: %v", err)
		}

		processed, err := repo.IsFacesProcessed(ctx, "photo789")
		if err != nil {
			t.Fatalf("Failed to check processed: %v", err)
		}
		if !processed {
			t.Error("Expected true, got false")
		}
	})

	// Test Count.
	t.Run("Count", func(t *testing.T) {
		count, err := repo.Count(ctx)
		if err != nil {
			t.Fatalf("Failed to count: %v", err)
		}
		if count != 2 {
			t.Errorf("Expected 2, got %d", count)
		}
	})

	// Test CountPhotos.
	t.Run("CountPhotos", func(t *testing.T) {
		count, err := repo.CountPhotos(ctx)
		if err != nil {
			t.Fatalf("Failed to count photos: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected 1, got %d", count)
		}
	})

	// Test UpdateFaceMarker.
	t.Run("UpdateFaceMarker", func(t *testing.T) {
		err := repo.UpdateFaceMarker(ctx, "photo456", 1, "newMarker", "newSubject", "Jane Doe")
		if err != nil {
			t.Fatalf("Failed to update marker: %v", err)
		}

		faces, _ := repo.GetFaces(ctx, "photo456")
		found := false
		for _, f := range faces {
			if f.FaceIndex == 1 && f.SubjectName == "Jane Doe" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Marker update not reflected")
		}
	})

	// Test that UpdateFaceMarker persists the new marker columns; the
	// pgvector index inside Postgres is kept current by the UPDATE itself.
	t.Run("UpdateFaceMarker_PersistsColumns", func(t *testing.T) {
		if err := repo.UpdateFaceMarker(ctx, "photo456", 0, "newMarker2", "newSubject2", "Person Two"); err != nil {
			t.Fatalf("Failed to update marker: %v", err)
		}

		faces, err := repo.GetFaces(ctx, "photo456")
		if err != nil {
			t.Fatalf("Failed to get faces: %v", err)
		}
		var found bool
		for _, f := range faces {
			if f.FaceIndex == 0 {
				found = true
				if f.MarkerUID != "newMarker2" {
					t.Errorf("MarkerUID: got %q, want %q", f.MarkerUID, "newMarker2")
				}
				if f.SubjectUID != "newSubject2" {
					t.Errorf("SubjectUID: got %q, want %q", f.SubjectUID, "newSubject2")
				}
				if f.SubjectName != "Person Two" {
					t.Errorf("SubjectName: got %q, want %q", f.SubjectName, "Person Two")
				}
			}
		}
		if !found {
			t.Fatal("Face with index 0 not found after update")
		}
	})

	// Test FindSimilarWithDistance.
	t.Run("FindSimilarWithDistance", func(t *testing.T) {
		query := make([]float32, 512)
		for i := range query {
			query[i] = float32(i) / 512.0
		}

		results, distances, err := repo.FindSimilarWithDistance(ctx, query, 10, 1.0)
		if err != nil {
			t.Fatalf("Failed to find similar: %v", err)
		}
		if len(results) == 0 {
			t.Error("Expected results, got none")
		}
		if len(results) != len(distances) {
			t.Errorf("Results and distances length mismatch")
		}
	})
}

func TestMigrations(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()

	ctx := context.Background()

	// Check migrations were applied.
	applied, err := pool.MigrationsApplied(ctx)
	if err != nil {
		t.Fatalf("Failed to get applied migrations: %v", err)
	}

	// Assert against the embedded migration set rather than a hardcoded
	// list. The previous version of this test pinned an explicit slice of
	// five filenames and asserted the total count equalled it, so it went
	// red the moment migration 006 landed and stayed red ("Expected 5
	// migrations, got 43") — a permanently-failing test tells you nothing.
	// Deriving the expectation from migrationsFS keeps it meaningful as
	// migrations are added.
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("Failed to read embedded migrations: %v", err)
	}
	expectedMigrations := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			expectedMigrations = append(expectedMigrations, e.Name())
		}
	}
	sort.Strings(expectedMigrations)

	if len(applied) != len(expectedMigrations) {
		t.Errorf("Expected %d migrations, got %d", len(expectedMigrations), len(applied))
	}

	// Applied order must match lexical filename order — that is the order
	// the runner uses, and the migrations depend on it.
	for i, expected := range expectedMigrations {
		if i < len(applied) && applied[i] != expected {
			t.Errorf("Migration %d: expected '%s', got '%s'", i, expected, applied[i])
		}
	}

	// Sanity check that the embedded set is actually the one we think it
	// is, so a broken embed does not turn this test into a tautology.
	if len(expectedMigrations) == 0 || expectedMigrations[0] != "001_create_embeddings.sql" {
		t.Errorf("embedded migrations look wrong: first = %v", expectedMigrations)
	}
}
