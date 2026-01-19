package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tomas/photo-sorter/internal/config"
)

// Connect creates a connection pool to PostgreSQL
func Connect(ctx context.Context, cfg *config.PostgresConfig) (*pgxpool.Pool, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("POSTGRES_HOST not set")
	}

	// Set default port
	port := cfg.Port
	if port == "" {
		port = "5432"
	}

	connStr := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		cfg.Host, port, cfg.User, cfg.Password, cfg.Database,
	)

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return pool, nil
}

// FaceEmbeddingDim is the fixed dimension for face embeddings (512 for buffalo_l/ResNet100)
const FaceEmbeddingDim = 512

// Migrate runs database migrations
func Migrate(ctx context.Context, pool *pgxpool.Pool, embeddingDim int) error {
	// Create pgvector extension
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector")
	if err != nil {
		return fmt.Errorf("failed to create vector extension: %w", err)
	}

	// Create embeddings table
	createTable := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS embeddings (
			photo_uid    VARCHAR(255) PRIMARY KEY,
			embedding    vector(%d),
			model        VARCHAR(255) NOT NULL,
			pretrained   VARCHAR(255) NOT NULL,
			dim          INTEGER NOT NULL,
			created_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)
	`, embeddingDim)

	_, err = pool.Exec(ctx, createTable)
	if err != nil {
		return fmt.Errorf("failed to create embeddings table: %w", err)
	}

	// Create faces table (fixed 512 dimensions for face embeddings)
	createFacesTable := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS faces (
			id           BIGSERIAL PRIMARY KEY,
			photo_uid    VARCHAR(255) NOT NULL,
			face_index   INTEGER NOT NULL,
			embedding    vector(%d) NOT NULL,
			bbox         DOUBLE PRECISION[4] NOT NULL,
			det_score    DOUBLE PRECISION NOT NULL,
			model        VARCHAR(255) NOT NULL,
			dim          INTEGER NOT NULL DEFAULT %d,
			created_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			UNIQUE(photo_uid, face_index)
		)
	`, FaceEmbeddingDim, FaceEmbeddingDim)

	_, err = pool.Exec(ctx, createFacesTable)
	if err != nil {
		return fmt.Errorf("failed to create faces table: %w", err)
	}

	// Create index on photo_uid for fast lookups
	_, err = pool.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS faces_photo_uid_idx ON faces(photo_uid)
	`)
	if err != nil {
		return fmt.Errorf("failed to create faces photo_uid index: %w", err)
	}

	return nil
}

// CreateVectorIndex creates the IVFFlat index for similarity search
// This should be called after the table has some data for optimal performance
func CreateVectorIndex(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS embeddings_vector_idx
		ON embeddings USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100)
	`)
	if err != nil {
		return fmt.Errorf("failed to create vector index: %w", err)
	}
	return nil
}

// CreateFaceVectorIndex creates the IVFFlat index for face similarity search
// This should be called after the table has some data for optimal performance
func CreateFaceVectorIndex(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS faces_vector_idx
		ON faces USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100)
	`)
	if err != nil {
		return fmt.Errorf("failed to create face vector index: %w", err)
	}
	return nil
}
