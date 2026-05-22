package database

import (
	"context"
	"errors"
)

var (
	postgresEmbeddingReader    func() EmbeddingReader
	postgresEmbeddingWriter    func() EmbeddingWriter
	postgresFaceReader         func() FaceReader
	postgresFaceWriter         func() FaceWriter
	postgresEraEmbeddingWriter func() EraEmbeddingWriter
	postgresBookWriter         func() BookWriter
	postgresTextVersionStore   func() TextVersionStore
	postgresTextCheckStore     func() TextCheckStore
	postgresPhotoWriter        func() PhotoWriter
	postgresAlbumWriter        func() AlbumWriter
	postgresLabelWriter        func() LabelWriter
	postgresUserWriter         func() UserWriter
	postgresMarkerWriter       func() MarkerWriter
	postgresSubjectWriter      func() SubjectWriter
	postgresPHashWriter        func() PHashWriter
	postgresInitialized        bool
)

// ResetForTesting resets all registered backends. Only for use in tests.
func ResetForTesting() {
	postgresEmbeddingReader = nil
	postgresEmbeddingWriter = nil
	postgresFaceReader = nil
	postgresFaceWriter = nil
	postgresEraEmbeddingWriter = nil
	postgresBookWriter = nil
	postgresTextVersionStore = nil
	postgresTextCheckStore = nil
	postgresPhotoWriter = nil
	postgresAlbumWriter = nil
	postgresLabelWriter = nil
	postgresUserWriter = nil
	postgresMarkerWriter = nil
	postgresSubjectWriter = nil
	postgresPHashWriter = nil
	postgresInitialized = false
}

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

// IsInitialized returns whether the PostgreSQL backend has been initialized.
func IsInitialized() bool {
	return postgresInitialized
}

// GetEmbeddingReader returns an EmbeddingReader from the PostgreSQL backend.
func GetEmbeddingReader(ctx context.Context) (EmbeddingReader, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresEmbeddingReader == nil {
		return nil, errors.New("PostgreSQL embedding reader not registered")
	}
	return postgresEmbeddingReader(), nil
}

// GetFaceReader returns a FaceReader from the PostgreSQL backend.
func GetFaceReader(ctx context.Context) (FaceReader, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresFaceReader == nil {
		return nil, errors.New("PostgreSQL face reader not registered")
	}
	return postgresFaceReader(), nil
}

// GetFaceWriter returns a FaceWriter from the PostgreSQL backend.
func GetFaceWriter(ctx context.Context) (FaceWriter, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresFaceWriter == nil {
		return nil, errors.New("PostgreSQL face writer not registered")
	}
	return postgresFaceWriter(), nil
}

// RegisterEmbeddingWriter registers the EmbeddingWriter constructor.
// Separate from RegisterPostgresBackend to avoid changing all existing callers.
func RegisterEmbeddingWriter(writer func() EmbeddingWriter) {
	postgresEmbeddingWriter = writer
}

// GetEmbeddingWriter returns an EmbeddingWriter from the PostgreSQL backend.
func GetEmbeddingWriter(ctx context.Context) (EmbeddingWriter, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresEmbeddingWriter == nil {
		return nil, errors.New("PostgreSQL embedding writer not registered")
	}
	return postgresEmbeddingWriter(), nil
}

// RegisterEraEmbeddingWriter registers the EraEmbeddingWriter constructor.
func RegisterEraEmbeddingWriter(writer func() EraEmbeddingWriter) {
	postgresEraEmbeddingWriter = writer
}

// GetEraEmbeddingWriter returns an EraEmbeddingWriter from the PostgreSQL backend.
func GetEraEmbeddingWriter(ctx context.Context) (EraEmbeddingWriter, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresEraEmbeddingWriter == nil {
		return nil, errors.New("PostgreSQL era embedding writer not registered")
	}
	return postgresEraEmbeddingWriter(), nil
}

// GetEraEmbeddingReader returns an EraEmbeddingReader from the PostgreSQL backend.
func GetEraEmbeddingReader(ctx context.Context) (EraEmbeddingReader, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresEraEmbeddingWriter == nil {
		return nil, errors.New("PostgreSQL era embedding writer not registered")
	}
	return postgresEraEmbeddingWriter(), nil
}

// RegisterBookWriter registers the BookWriter constructor.
func RegisterBookWriter(writer func() BookWriter) {
	postgresBookWriter = writer
}

// GetBookWriter returns a BookWriter from the PostgreSQL backend.
func GetBookWriter(ctx context.Context) (BookWriter, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresBookWriter == nil {
		return nil, errors.New("PostgreSQL book writer not registered")
	}
	return postgresBookWriter(), nil
}

// GetBookReader returns a BookReader from the PostgreSQL backend.
func GetBookReader(ctx context.Context) (BookReader, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresBookWriter == nil {
		return nil, errors.New("PostgreSQL book writer not registered")
	}
	return postgresBookWriter(), nil
}

// RegisterTextVersionStore registers the TextVersionStore constructor.
func RegisterTextVersionStore(store func() TextVersionStore) {
	postgresTextVersionStore = store
}

// GetTextVersionStore returns a TextVersionStore from the PostgreSQL backend.
func GetTextVersionStore(ctx context.Context) (TextVersionStore, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresTextVersionStore == nil {
		return nil, errors.New("PostgreSQL text version store not registered")
	}
	return postgresTextVersionStore(), nil
}

// RegisterTextCheckStore registers the TextCheckStore constructor.
func RegisterTextCheckStore(store func() TextCheckStore) {
	postgresTextCheckStore = store
}

// GetTextCheckStore returns a TextCheckStore from the PostgreSQL backend.
func GetTextCheckStore(ctx context.Context) (TextCheckStore, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresTextCheckStore == nil {
		return nil, errors.New("PostgreSQL text check store not registered")
	}
	return postgresTextCheckStore(), nil
}

// RegisterPhotoWriter registers the PhotoWriter constructor. The same value
// also serves PhotoReader, since the writer embeds the reader interface.
func RegisterPhotoWriter(writer func() PhotoWriter) {
	postgresPhotoWriter = writer
}

// GetPhotoWriter returns a PhotoWriter from the PostgreSQL backend.
func GetPhotoWriter(ctx context.Context) (PhotoWriter, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresPhotoWriter == nil {
		return nil, errors.New("PostgreSQL photo writer not registered")
	}
	return postgresPhotoWriter(), nil
}

// GetPhotoReader returns a PhotoReader from the PostgreSQL backend.
func GetPhotoReader(ctx context.Context) (PhotoReader, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresPhotoWriter == nil {
		return nil, errors.New("PostgreSQL photo reader not registered")
	}
	return postgresPhotoWriter(), nil
}

// GetPhotoBrowseReader returns a PhotoBrowseReader from the PostgreSQL
// backend. The same registered PhotoWriter constructor is reused — every
// PhotoWriter implementation in this repo also implements
// PhotoBrowseReader, so a separate registration would be dead weight. The
// type assertion guards against future implementations that forget to
// expose the browse methods.
func GetPhotoBrowseReader(ctx context.Context) (PhotoBrowseReader, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresPhotoWriter == nil {
		return nil, errors.New("PostgreSQL photo reader not registered")
	}
	browse, ok := postgresPhotoWriter().(PhotoBrowseReader)
	if !ok {
		return nil, errors.New("registered PhotoWriter does not implement PhotoBrowseReader")
	}
	return browse, nil
}

// RegisterAlbumWriter registers the AlbumWriter constructor. The same value
// also serves AlbumReader, since the writer embeds the reader interface.
func RegisterAlbumWriter(writer func() AlbumWriter) {
	postgresAlbumWriter = writer
}

// GetAlbumWriter returns an AlbumWriter from the PostgreSQL backend.
func GetAlbumWriter(ctx context.Context) (AlbumWriter, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresAlbumWriter == nil {
		return nil, errors.New("PostgreSQL album writer not registered")
	}
	return postgresAlbumWriter(), nil
}

// GetAlbumReader returns an AlbumReader from the PostgreSQL backend.
func GetAlbumReader(ctx context.Context) (AlbumReader, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresAlbumWriter == nil {
		return nil, errors.New("PostgreSQL album reader not registered")
	}
	return postgresAlbumWriter(), nil
}

// RegisterLabelWriter registers the LabelWriter constructor. The same value
// also serves LabelReader, since the writer embeds the reader interface.
func RegisterLabelWriter(writer func() LabelWriter) {
	postgresLabelWriter = writer
}

// GetLabelWriter returns a LabelWriter from the PostgreSQL backend.
func GetLabelWriter(ctx context.Context) (LabelWriter, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresLabelWriter == nil {
		return nil, errors.New("PostgreSQL label writer not registered")
	}
	return postgresLabelWriter(), nil
}

// GetLabelReader returns a LabelReader from the PostgreSQL backend.
func GetLabelReader(ctx context.Context) (LabelReader, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresLabelWriter == nil {
		return nil, errors.New("PostgreSQL label reader not registered")
	}
	return postgresLabelWriter(), nil
}

// RegisterUserWriter registers the UserWriter constructor. The same value
// also serves UserReader, since the writer embeds the reader interface.
func RegisterUserWriter(writer func() UserWriter) {
	postgresUserWriter = writer
}

// GetUserWriter returns a UserWriter from the PostgreSQL backend.
func GetUserWriter(ctx context.Context) (UserWriter, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresUserWriter == nil {
		return nil, errors.New("PostgreSQL user writer not registered")
	}
	return postgresUserWriter(), nil
}

// GetUserReader returns a UserReader from the PostgreSQL backend.
func GetUserReader(ctx context.Context) (UserReader, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresUserWriter == nil {
		return nil, errors.New("PostgreSQL user reader not registered")
	}
	return postgresUserWriter(), nil
}

// RegisterMarkerWriter registers the MarkerWriter constructor. The same
// value also serves MarkerReader, since the writer embeds the reader
// interface.
func RegisterMarkerWriter(writer func() MarkerWriter) {
	postgresMarkerWriter = writer
}

// GetMarkerWriter returns a MarkerWriter from the PostgreSQL backend.
func GetMarkerWriter(ctx context.Context) (MarkerWriter, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresMarkerWriter == nil {
		return nil, errors.New("PostgreSQL marker writer not registered")
	}
	return postgresMarkerWriter(), nil
}

// GetMarkerReader returns a MarkerReader from the PostgreSQL backend.
func GetMarkerReader(ctx context.Context) (MarkerReader, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresMarkerWriter == nil {
		return nil, errors.New("PostgreSQL marker reader not registered")
	}
	return postgresMarkerWriter(), nil
}

// RegisterSubjectWriter registers the SubjectWriter constructor. The same
// value also serves SubjectReader, since the writer embeds the reader
// interface.
func RegisterSubjectWriter(writer func() SubjectWriter) {
	postgresSubjectWriter = writer
}

// GetSubjectWriter returns a SubjectWriter from the PostgreSQL backend.
func GetSubjectWriter(ctx context.Context) (SubjectWriter, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresSubjectWriter == nil {
		return nil, errors.New("PostgreSQL subject writer not registered")
	}
	return postgresSubjectWriter(), nil
}

// GetSubjectReader returns a SubjectReader from the PostgreSQL backend.
func GetSubjectReader(ctx context.Context) (SubjectReader, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresSubjectWriter == nil {
		return nil, errors.New("PostgreSQL subject reader not registered")
	}
	return postgresSubjectWriter(), nil
}

// RegisterPHashWriter registers the PHashWriter constructor. The same
// value also serves PHashReader, since the writer embeds the reader
// interface.
func RegisterPHashWriter(writer func() PHashWriter) {
	postgresPHashWriter = writer
}

// GetPHashWriter returns a PHashWriter from the PostgreSQL backend.
func GetPHashWriter(ctx context.Context) (PHashWriter, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresPHashWriter == nil {
		return nil, errors.New("PostgreSQL phash writer not registered")
	}
	return postgresPHashWriter(), nil
}

// GetPHashReader returns a PHashReader from the PostgreSQL backend.
func GetPHashReader(ctx context.Context) (PHashReader, error) {
	if !postgresInitialized {
		return nil, errors.New("PostgreSQL backend not initialized: DATABASE_URL is required")
	}
	if postgresPHashWriter == nil {
		return nil, errors.New("PostgreSQL phash reader not registered")
	}
	return postgresPHashWriter(), nil
}
