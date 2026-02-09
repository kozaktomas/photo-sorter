package database

import (
	"context"
	"fmt"
)

// HNSWRebuilder is an interface for repositories that support HNSW index rebuilding
type HNSWRebuilder interface {
	// RebuildHNSW rebuilds the in-memory HNSW index
	RebuildHNSW(ctx context.Context) error
	// HNSWCount returns the number of items in the HNSW index
	HNSWCount() int
	// IsHNSWEnabled returns whether HNSW is enabled
	IsHNSWEnabled() bool
	// SaveHNSWIndex saves the current index to disk (if path configured)
	SaveHNSWIndex() error
}

var (
	postgresEmbeddingReader    func() EmbeddingReader
	postgresEmbeddingWriter    func() EmbeddingWriter
	postgresFaceReader         func() FaceReader
	postgresFaceWriter         func() FaceWriter
	postgresEraEmbeddingWriter func() EraEmbeddingWriter
	postgresBookWriter         func() BookWriter
	postgresFaceHNSW           HNSWRebuilder // Singleton for face HNSW rebuilding
	postgresEmbeddingHNSW      HNSWRebuilder // Singleton for embedding HNSW rebuilding
	postgresInitialized        bool
)

// RegisterPostgresBackend registers PostgreSQL repository constructors.
// This is called by the postgres package to avoid import cycles.
func RegisterPostgresBackend(
	embReader func() EmbeddingReader,
	faceReader func() FaceReader,
	faceWriter func() FaceWriter,
) {
	postgresEmbeddingReader = embReader
	postgresFaceReader = faceReader
	postgresFaceWriter = faceWriter
	postgresInitialized = true
}

// RegisterFaceHNSWRebuilder registers the HNSW rebuilder for the face repository.
// This allows rebuilding the in-memory HNSW index without knowing the concrete type.
func RegisterFaceHNSWRebuilder(rebuilder HNSWRebuilder) {
	postgresFaceHNSW = rebuilder
}

// GetFaceHNSWRebuilder returns the registered face HNSW rebuilder, or nil if not registered.
func GetFaceHNSWRebuilder() HNSWRebuilder {
	return postgresFaceHNSW
}

// RegisterEmbeddingHNSWRebuilder registers the HNSW rebuilder for the embedding repository.
// This allows rebuilding the in-memory HNSW index without knowing the concrete type.
func RegisterEmbeddingHNSWRebuilder(rebuilder HNSWRebuilder) {
	postgresEmbeddingHNSW = rebuilder
}

// GetEmbeddingHNSWRebuilder returns the registered embedding HNSW rebuilder, or nil if not registered.
func GetEmbeddingHNSWRebuilder() HNSWRebuilder {
	return postgresEmbeddingHNSW
}

// IsInitialized returns whether the PostgreSQL backend has been initialized.
func IsInitialized() bool {
	return postgresInitialized
}

// GetEmbeddingReader returns an EmbeddingReader from the PostgreSQL backend
func GetEmbeddingReader(ctx context.Context) (EmbeddingReader, error) {
	if !postgresInitialized {
		return nil, fmt.Errorf("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresEmbeddingReader == nil {
		return nil, fmt.Errorf("PostgreSQL embedding reader not registered")
	}
	return postgresEmbeddingReader(), nil
}

// GetFaceReader returns a FaceReader from the PostgreSQL backend
func GetFaceReader(ctx context.Context) (FaceReader, error) {
	if !postgresInitialized {
		return nil, fmt.Errorf("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresFaceReader == nil {
		return nil, fmt.Errorf("PostgreSQL face reader not registered")
	}
	return postgresFaceReader(), nil
}

// GetFaceWriter returns a FaceWriter from the PostgreSQL backend
func GetFaceWriter(ctx context.Context) (FaceWriter, error) {
	if !postgresInitialized {
		return nil, fmt.Errorf("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresFaceWriter == nil {
		return nil, fmt.Errorf("PostgreSQL face writer not registered")
	}
	return postgresFaceWriter(), nil
}

// RegisterEmbeddingWriter registers the EmbeddingWriter constructor.
// Separate from RegisterPostgresBackend to avoid changing all existing callers.
func RegisterEmbeddingWriter(writer func() EmbeddingWriter) {
	postgresEmbeddingWriter = writer
}

// GetEmbeddingWriter returns an EmbeddingWriter from the PostgreSQL backend
func GetEmbeddingWriter(ctx context.Context) (EmbeddingWriter, error) {
	if !postgresInitialized {
		return nil, fmt.Errorf("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresEmbeddingWriter == nil {
		return nil, fmt.Errorf("PostgreSQL embedding writer not registered")
	}
	return postgresEmbeddingWriter(), nil
}

// RegisterEraEmbeddingWriter registers the EraEmbeddingWriter constructor.
func RegisterEraEmbeddingWriter(writer func() EraEmbeddingWriter) {
	postgresEraEmbeddingWriter = writer
}

// GetEraEmbeddingWriter returns an EraEmbeddingWriter from the PostgreSQL backend
func GetEraEmbeddingWriter(ctx context.Context) (EraEmbeddingWriter, error) {
	if !postgresInitialized {
		return nil, fmt.Errorf("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresEraEmbeddingWriter == nil {
		return nil, fmt.Errorf("PostgreSQL era embedding writer not registered")
	}
	return postgresEraEmbeddingWriter(), nil
}

// GetEraEmbeddingReader returns an EraEmbeddingReader from the PostgreSQL backend
func GetEraEmbeddingReader(ctx context.Context) (EraEmbeddingReader, error) {
	if !postgresInitialized {
		return nil, fmt.Errorf("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresEraEmbeddingWriter == nil {
		return nil, fmt.Errorf("PostgreSQL era embedding writer not registered")
	}
	return postgresEraEmbeddingWriter(), nil
}

// RegisterBookWriter registers the BookWriter constructor.
func RegisterBookWriter(writer func() BookWriter) {
	postgresBookWriter = writer
}

// GetBookWriter returns a BookWriter from the PostgreSQL backend
func GetBookWriter(ctx context.Context) (BookWriter, error) {
	if !postgresInitialized {
		return nil, fmt.Errorf("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresBookWriter == nil {
		return nil, fmt.Errorf("PostgreSQL book writer not registered")
	}
	return postgresBookWriter(), nil
}

// GetBookReader returns a BookReader from the PostgreSQL backend
func GetBookReader(ctx context.Context) (BookReader, error) {
	if !postgresInitialized {
		return nil, fmt.Errorf("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresBookWriter == nil {
		return nil, fmt.Errorf("PostgreSQL book writer not registered")
	}
	return postgresBookWriter(), nil
}
