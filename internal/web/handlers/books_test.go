package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/mock"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

var errMock = errors.New("mock error")

// setupBookTest registers a MockBookWriter via the database provider system.
// and returns it along with a BooksHandler. Cleanup deregisters the mock.
func setupBookTest(t *testing.T) (*mock.MockBookWriter, *BooksHandler) {
	t.Helper()

	mockBW := mock.NewMockBookWriter()

	// Register the mock as the postgres backend so getBookWriter() works.
	database.RegisterPostgresBackend(nil, nil, nil)
	database.RegisterBookWriter(func() database.BookWriter { return mockBW })

	t.Cleanup(func() {
		database.ResetForTesting()
	})

	handler := NewBooksHandler(testConfig(), nil)
	return mockBW, handler
}

// --- Books CRUD ---

func TestBooksHandler_ListBooks_Success(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	now := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	mockBW.AddBook(database.PhotoBook{ID: "b1", Title: "Book One", Description: "desc1", CreatedAt: now, UpdatedAt: now})
	mockBW.AddBook(database.PhotoBook{ID: "b2", Title: "Book Two", CreatedAt: now, UpdatedAt: now})
	mockBW.AddSection(database.BookSection{ID: "s1", BookID: "b1", Title: "Section 1"})
	mockBW.SetSectionPhotos("s1", []database.SectionPhoto{
		{SectionID: "s1", PhotoUID: "p1"},
		{SectionID: "s1", PhotoUID: "p2"},
	})

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/books", nil)
	recorder := httptest.NewRecorder()
	handler.ListBooks(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var resp []bookResponse
	parseJSONResponse(t, recorder, &resp)
	if len(resp) != 2 {
		t.Fatalf("expected 2 books, got %d", len(resp))
	}
}

func TestBooksHandler_ListBooks_Empty(t *testing.T) {
	_, handler := setupBookTest(t)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/books", nil)
	recorder := httptest.NewRecorder()
	handler.ListBooks(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var resp []bookResponse
	parseJSONResponse(t, recorder, &resp)
	if len(resp) != 0 {
		t.Errorf("expected 0 books, got %d", len(resp))
	}
}

func TestBooksHandler_ListBooks_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.ListBooksError = errMock

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/books", nil)
	recorder := httptest.NewRecorder()
	handler.ListBooks(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to list books")
}

func TestBooksHandler_CreateBook_Success(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"title":"My Book","description":"A description"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/books", body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.CreateBook(recorder, req)

	assertStatusCode(t, recorder, http.StatusCreated)
	assertContentType(t, recorder, "application/json")

	var resp bookResponse
	parseJSONResponse(t, recorder, &resp)
	if resp.Title != "My Book" {
		t.Errorf("expected title 'My Book', got '%s'", resp.Title)
	}
	if resp.Description != "A description" {
		t.Errorf("expected description 'A description', got '%s'", resp.Description)
	}
	if resp.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestBooksHandler_CreateBook_MissingTitle(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"description":"no title"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/books", body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.CreateBook(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "title is required")
}

func TestBooksHandler_CreateBook_InvalidJSON(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/books", body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.CreateBook(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestBooksHandler_CreateBook_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.CreateBookError = errMock

	body := bytes.NewBufferString(`{"title":"Book"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/books", body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.CreateBook(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to create book")
}

func TestBooksHandler_GetBook_Success(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	now := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	mockBW.AddBook(database.PhotoBook{ID: "b1", Title: "Test Book", Description: "desc", CreatedAt: now, UpdatedAt: now})
	mockBW.AddSection(database.BookSection{ID: "s1", BookID: "b1", Title: "Sec 1"})
	mockBW.AddPage(database.BookPage{ID: "p1", BookID: "b1", SectionID: "s1", Format: "4_landscape"})

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/books/b1", nil)
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.GetBook(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var resp bookDetailResponse
	parseJSONResponse(t, recorder, &resp)
	if resp.Title != "Test Book" {
		t.Errorf("expected title 'Test Book', got '%s'", resp.Title)
	}
	if len(resp.Sections) != 1 {
		t.Errorf("expected 1 section, got %d", len(resp.Sections))
	}
	if len(resp.Pages) != 1 {
		t.Errorf("expected 1 page, got %d", len(resp.Pages))
	}
}

func TestBooksHandler_GetBook_NotFound(t *testing.T) {
	_, handler := setupBookTest(t)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/books/nonexistent", nil)
	req = requestWithChiParams(req, map[string]string{"id": "nonexistent"})
	recorder := httptest.NewRecorder()
	handler.GetBook(recorder, req)

	assertStatusCode(t, recorder, http.StatusNotFound)
	assertJSONError(t, recorder, "book not found")
}

func TestBooksHandler_UpdateBook_Success(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.AddBook(database.PhotoBook{ID: "b1", Title: "Old Title"})

	body := bytes.NewBufferString(`{"title":"New Title"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/books/b1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.UpdateBook(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var resp map[string]string
	parseJSONResponse(t, recorder, &resp)
	if resp["id"] != "b1" {
		t.Errorf("expected id 'b1', got '%s'", resp["id"])
	}
}

func TestBooksHandler_UpdateBook_NotFound(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"title":"New Title"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/books/nonexistent", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "nonexistent"})
	recorder := httptest.NewRecorder()
	handler.UpdateBook(recorder, req)

	assertStatusCode(t, recorder, http.StatusNotFound)
	assertJSONError(t, recorder, "book not found")
}

func TestBooksHandler_UpdateBook_InvalidJSON(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.AddBook(database.PhotoBook{ID: "b1", Title: "Book"})

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/books/b1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.UpdateBook(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestBooksHandler_UpdateBook_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.AddBook(database.PhotoBook{ID: "b1", Title: "Book"})
	mockBW.UpdateBookError = errMock

	body := bytes.NewBufferString(`{"title":"Updated"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/books/b1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.UpdateBook(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to update book")
}

func TestBooksHandler_DeleteBook_Success(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.AddBook(database.PhotoBook{ID: "b1", Title: "Book"})

	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/books/b1", nil)
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.DeleteBook(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var resp map[string]bool
	parseJSONResponse(t, recorder, &resp)
	if !resp["deleted"] {
		t.Error("expected deleted=true")
	}
}

func TestBooksHandler_DeleteBook_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.DeleteBookError = errMock

	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/books/b1", nil)
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.DeleteBook(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to delete book")
}

// --- Sections ---

func TestBooksHandler_CreateSection_Success(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"title":"New Section"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/books/b1/sections", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.CreateSection(recorder, req)

	assertStatusCode(t, recorder, http.StatusCreated)
	assertContentType(t, recorder, "application/json")

	var resp sectionResponse
	parseJSONResponse(t, recorder, &resp)
	if resp.Title != "New Section" {
		t.Errorf("expected title 'New Section', got '%s'", resp.Title)
	}
	if resp.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestBooksHandler_CreateSection_MissingTitle(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/books/b1/sections", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.CreateSection(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "title is required")
}

func TestBooksHandler_CreateSection_InvalidJSON(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/books/b1/sections", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.CreateSection(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestBooksHandler_CreateSection_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.CreateSectionError = errMock

	body := bytes.NewBufferString(`{"title":"Section"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/books/b1/sections", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.CreateSection(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to create section")
}

func TestBooksHandler_UpdateSection_Success(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.AddSection(database.BookSection{ID: "s1", BookID: "b1", Title: "Old"})

	body := bytes.NewBufferString(`{"title":"Updated Section"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/sections/s1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "s1"})
	recorder := httptest.NewRecorder()
	handler.UpdateSection(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var resp map[string]string
	parseJSONResponse(t, recorder, &resp)
	if resp["id"] != "s1" {
		t.Errorf("expected id 's1', got '%s'", resp["id"])
	}
}

func TestBooksHandler_UpdateSection_InvalidJSON(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/sections/s1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "s1"})
	recorder := httptest.NewRecorder()
	handler.UpdateSection(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestBooksHandler_UpdateSection_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.AddSection(database.BookSection{ID: "s1", BookID: "b1", Title: "Original"})
	mockBW.UpdateSectionError = errMock

	body := bytes.NewBufferString(`{"title":"Updated"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/sections/s1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "s1"})
	recorder := httptest.NewRecorder()
	handler.UpdateSection(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to update section")
}

func TestBooksHandler_DeleteSection_Success(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.AddSection(database.BookSection{ID: "s1", BookID: "b1"})

	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/sections/s1", nil)
	req = requestWithChiParams(req, map[string]string{"id": "s1"})
	recorder := httptest.NewRecorder()
	handler.DeleteSection(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var resp map[string]bool
	parseJSONResponse(t, recorder, &resp)
	if !resp["deleted"] {
		t.Error("expected deleted=true")
	}
}

func TestBooksHandler_DeleteSection_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.DeleteSectionError = errMock

	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/sections/s1", nil)
	req = requestWithChiParams(req, map[string]string{"id": "s1"})
	recorder := httptest.NewRecorder()
	handler.DeleteSection(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to delete section")
}

func TestBooksHandler_ReorderSections_Success(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"section_ids":["s2","s1","s3"]}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/books/b1/sections/reorder", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.ReorderSections(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var resp map[string]bool
	parseJSONResponse(t, recorder, &resp)
	if !resp["reordered"] {
		t.Error("expected reordered=true")
	}
}

func TestBooksHandler_ReorderSections_InvalidJSON(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/books/b1/sections/reorder", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.ReorderSections(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestBooksHandler_ReorderSections_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.ReorderSectionsError = errMock

	body := bytes.NewBufferString(`{"section_ids":["s1"]}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/books/b1/sections/reorder", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.ReorderSections(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to reorder sections")
}

// --- Section Photos ---

func TestBooksHandler_GetSectionPhotos_Success(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	now := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)
	mockBW.SetSectionPhotos("s1", []database.SectionPhoto{
		{SectionID: "s1", PhotoUID: "p1", Description: "desc1", AddedAt: now},
		{SectionID: "s1", PhotoUID: "p2", Description: "desc2", AddedAt: now},
	})

	// Set up mock PhotoPrism server for photo name enrichment
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{
				{"UID": "p1", "Title": "Photo One", "FileName": "photo1.jpg"},
				{"UID": "p2", "Title": "Photo Two", "FileName": "photo2.jpg"},
			})
		},
	})
	defer server.Close()
	pp := createPhotoPrismClient(t, server)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/sections/s1/photos", nil)
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)
	req = requestWithChiParams(req, map[string]string{"id": "s1"})
	recorder := httptest.NewRecorder()
	handler.GetSectionPhotos(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var resp []sectionPhotoResponse
	parseJSONResponse(t, recorder, &resp)
	if len(resp) != 2 {
		t.Fatalf("expected 2 photos, got %d", len(resp))
	}
	if resp[0].PhotoUID != "p1" {
		t.Errorf("expected photo UID 'p1', got '%s'", resp[0].PhotoUID)
	}
	if resp[0].Title != "Photo One" {
		t.Errorf("expected title 'Photo One', got '%s'", resp[0].Title)
	}
	if resp[0].FileName != "photo1.jpg" {
		t.Errorf("expected file_name 'photo1.jpg', got '%s'", resp[0].FileName)
	}
}

func TestBooksHandler_GetSectionPhotos_Empty(t *testing.T) {
	_, handler := setupBookTest(t)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/sections/s1/photos", nil)
	req = requestWithChiParams(req, map[string]string{"id": "s1"})
	recorder := httptest.NewRecorder()
	handler.GetSectionPhotos(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var resp []sectionPhotoResponse
	parseJSONResponse(t, recorder, &resp)
	if len(resp) != 0 {
		t.Errorf("expected 0 photos, got %d", len(resp))
	}
}

func TestBooksHandler_GetSectionPhotos_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.GetSectionPhotosError = errMock

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/sections/s1/photos", nil)
	req = requestWithChiParams(req, map[string]string{"id": "s1"})
	recorder := httptest.NewRecorder()
	handler.GetSectionPhotos(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to get section photos")
}

func TestBooksHandler_AddSectionPhotos_Success(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"photo_uids":["p1","p2","p3"]}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/sections/s1/photos", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "s1"})
	recorder := httptest.NewRecorder()
	handler.AddSectionPhotos(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var resp map[string]int
	parseJSONResponse(t, recorder, &resp)
	if resp["added"] != 3 {
		t.Errorf("expected added=3, got %d", resp["added"])
	}
}

func TestBooksHandler_AddSectionPhotos_EmptyUIDs(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"photo_uids":[]}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/sections/s1/photos", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "s1"})
	recorder := httptest.NewRecorder()
	handler.AddSectionPhotos(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "photo_uids is required")
}

func TestBooksHandler_AddSectionPhotos_InvalidJSON(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/sections/s1/photos", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "s1"})
	recorder := httptest.NewRecorder()
	handler.AddSectionPhotos(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestBooksHandler_AddSectionPhotos_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.AddSectionPhotosError = errMock

	body := bytes.NewBufferString(`{"photo_uids":["p1"]}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/sections/s1/photos", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "s1"})
	recorder := httptest.NewRecorder()
	handler.AddSectionPhotos(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to add photos")
}

func TestBooksHandler_RemoveSectionPhotos_Success(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"photo_uids":["p1","p2"]}`)
	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/sections/s1/photos", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "s1"})
	recorder := httptest.NewRecorder()
	handler.RemoveSectionPhotos(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var resp map[string]int
	parseJSONResponse(t, recorder, &resp)
	if resp["removed"] != 2 {
		t.Errorf("expected removed=2, got %d", resp["removed"])
	}
}

func TestBooksHandler_RemoveSectionPhotos_InvalidJSON(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/sections/s1/photos", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "s1"})
	recorder := httptest.NewRecorder()
	handler.RemoveSectionPhotos(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestBooksHandler_RemoveSectionPhotos_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.RemoveSectionPhotosError = errMock

	body := bytes.NewBufferString(`{"photo_uids":["p1"]}`)
	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/sections/s1/photos", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "s1"})
	recorder := httptest.NewRecorder()
	handler.RemoveSectionPhotos(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to remove photos")
}

func TestBooksHandler_UpdatePhotoDescription_Success(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.SetSectionPhotos("s1", []database.SectionPhoto{
		{SectionID: "s1", PhotoUID: "p1"},
	})

	body := bytes.NewBufferString(`{"description":"new desc","note":"a note"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/sections/s1/photos/p1/description", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "s1", "photoUid": "p1"})
	recorder := httptest.NewRecorder()
	handler.UpdatePhotoDescription(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var resp map[string]bool
	parseJSONResponse(t, recorder, &resp)
	if !resp["updated"] {
		t.Error("expected updated=true")
	}
}

func TestBooksHandler_UpdatePhotoDescription_InvalidJSON(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/sections/s1/photos/p1/description", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "s1", "photoUid": "p1"})
	recorder := httptest.NewRecorder()
	handler.UpdatePhotoDescription(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestBooksHandler_UpdatePhotoDescription_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.UpdateSectionPhotoError = errMock

	body := bytes.NewBufferString(`{"description":"desc"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/sections/s1/photos/p1/description", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "s1", "photoUid": "p1"})
	recorder := httptest.NewRecorder()
	handler.UpdatePhotoDescription(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to update photo")
}

// --- Pages ---

func TestBooksHandler_CreatePage_Success(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"format":"4_landscape","section_id":"s1"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/books/b1/pages", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.CreatePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusCreated)
	assertContentType(t, recorder, "application/json")

	var resp pageResponse
	parseJSONResponse(t, recorder, &resp)
	if resp.Format != "4_landscape" {
		t.Errorf("expected format '4_landscape', got '%s'", resp.Format)
	}
	if resp.SectionID != "s1" {
		t.Errorf("expected section_id 's1', got '%s'", resp.SectionID)
	}
	if resp.ID == "" {
		t.Error("expected non-empty ID")
	}
	if resp.Slots == nil {
		t.Error("expected non-nil slots array")
	}
}

func TestBooksHandler_CreatePage_InvalidFormat(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"format":"invalid_fmt","section_id":"s1"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/books/b1/pages", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.CreatePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid format")
}

func TestBooksHandler_CreatePage_MissingSectionID(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"format":"4_landscape"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/books/b1/pages", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.CreatePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "section_id is required")
}

func TestBooksHandler_CreatePage_InvalidJSON(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/books/b1/pages", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.CreatePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestBooksHandler_CreatePage_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.CreatePageError = errMock

	body := bytes.NewBufferString(`{"format":"2_portrait","section_id":"s1"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/books/b1/pages", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.CreatePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to create page")
}

func TestBooksHandler_UpdatePage_Success(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.AddPage(database.BookPage{ID: "p1", BookID: "b1", SectionID: "s1", Format: "4_landscape"})

	body := bytes.NewBufferString(`{"description":"new desc"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1"})
	recorder := httptest.NewRecorder()
	handler.UpdatePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var resp map[string]string
	parseJSONResponse(t, recorder, &resp)
	if resp["id"] != "p1" {
		t.Errorf("expected id 'p1', got '%s'", resp["id"])
	}
}

func TestBooksHandler_UpdatePage_NotFound(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"description":"desc"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/nonexistent", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "nonexistent"})
	recorder := httptest.NewRecorder()
	handler.UpdatePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusNotFound)
	assertJSONError(t, recorder, "page not found")
}

func TestBooksHandler_UpdatePage_InvalidFormat(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.AddPage(database.BookPage{ID: "p1", BookID: "b1", SectionID: "s1", Format: "4_landscape"})

	body := bytes.NewBufferString(`{"format":"bad_format"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1"})
	recorder := httptest.NewRecorder()
	handler.UpdatePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid format")
}

func TestBooksHandler_UpdatePage_EmptySectionID(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.AddPage(database.BookPage{ID: "p1", BookID: "b1", SectionID: "s1", Format: "4_landscape"})

	body := bytes.NewBufferString(`{"section_id":""}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1"})
	recorder := httptest.NewRecorder()
	handler.UpdatePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "section_id is required")
}

func TestBooksHandler_UpdatePage_InvalidJSON(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.AddPage(database.BookPage{ID: "p1", BookID: "b1", SectionID: "s1", Format: "4_landscape"})

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1"})
	recorder := httptest.NewRecorder()
	handler.UpdatePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestBooksHandler_UpdatePage_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.AddPage(database.BookPage{ID: "p1", BookID: "b1", SectionID: "s1", Format: "4_landscape"})
	mockBW.UpdatePageError = errMock

	body := bytes.NewBufferString(`{"description":"desc"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1"})
	recorder := httptest.NewRecorder()
	handler.UpdatePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to update page")
}

func TestBooksHandler_UpdatePage_FormatDownsizeClearsExcessSlots(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.AddPage(database.BookPage{ID: "p1", BookID: "b1", SectionID: "s1", Format: "4_landscape"})
	// Assign 4 slots (indices 0-3).
	ctx := context.TODO()
	for i := range 4 {
		_ = mockBW.AssignSlot(ctx, "p1", i, fmt.Sprintf("photo%d", i))
	}

	// Change format to 2_portrait (2 slots) — slots 2 and 3 should be cleared.
	body := bytes.NewBufferString(`{"format":"2_portrait"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1"})
	recorder := httptest.NewRecorder()
	handler.UpdatePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	// Verify page format was updated.
	page, _ := mockBW.GetPage(ctx, "p1")
	if page.Format != "2_portrait" {
		t.Errorf("expected format '2_portrait', got '%s'", page.Format)
	}

	// Verify slots 0 and 1 still have photos.
	slots, _ := mockBW.GetPageSlots(ctx, "p1")
	for _, s := range slots {
		if s.SlotIndex < 2 && s.PhotoUID == "" {
			t.Errorf("slot %d should still have a photo", s.SlotIndex)
		}
		if s.SlotIndex >= 2 && s.PhotoUID != "" {
			t.Errorf("slot %d should have been cleared, but has '%s'", s.SlotIndex, s.PhotoUID)
		}
	}
}

func TestBooksHandler_UpdatePage_FormatUpsizePreservesSlots(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.AddPage(database.BookPage{ID: "p1", BookID: "b1", SectionID: "s1", Format: "2_portrait"})
	// Assign 2 slots.
	ctx := context.TODO()
	_ = mockBW.AssignSlot(ctx, "p1", 0, "photoA")
	_ = mockBW.AssignSlot(ctx, "p1", 1, "photoB")

	// Change format to 4_landscape (4 slots) — both existing slots should be preserved.
	body := bytes.NewBufferString(`{"format":"4_landscape"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1"})
	recorder := httptest.NewRecorder()
	handler.UpdatePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	// Verify page format was updated.
	page, _ := mockBW.GetPage(ctx, "p1")
	if page.Format != "4_landscape" {
		t.Errorf("expected format '4_landscape', got '%s'", page.Format)
	}

	// Verify both original slots are preserved.
	slots, _ := mockBW.GetPageSlots(ctx, "p1")
	photosBySlot := make(map[int]string)
	for _, s := range slots {
		if s.PhotoUID != "" {
			photosBySlot[s.SlotIndex] = s.PhotoUID
		}
	}
	if photosBySlot[0] != "photoA" {
		t.Errorf("slot 0 expected 'photoA', got '%s'", photosBySlot[0])
	}
	if photosBySlot[1] != "photoB" {
		t.Errorf("slot 1 expected 'photoB', got '%s'", photosBySlot[1])
	}
}

func TestBooksHandler_DeletePage_Success(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.AddPage(database.BookPage{ID: "p1", BookID: "b1"})

	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/pages/p1", nil)
	req = requestWithChiParams(req, map[string]string{"id": "p1"})
	recorder := httptest.NewRecorder()
	handler.DeletePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var resp map[string]bool
	parseJSONResponse(t, recorder, &resp)
	if !resp["deleted"] {
		t.Error("expected deleted=true")
	}
}

func TestBooksHandler_DeletePage_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.DeletePageError = errMock

	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/pages/p1", nil)
	req = requestWithChiParams(req, map[string]string{"id": "p1"})
	recorder := httptest.NewRecorder()
	handler.DeletePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to delete page")
}

func TestBooksHandler_ReorderPages_Success(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"page_ids":["p3","p1","p2"]}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/books/b1/pages/reorder", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.ReorderPages(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var resp map[string]bool
	parseJSONResponse(t, recorder, &resp)
	if !resp["reordered"] {
		t.Error("expected reordered=true")
	}
}

func TestBooksHandler_ReorderPages_InvalidJSON(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/books/b1/pages/reorder", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.ReorderPages(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestBooksHandler_ReorderPages_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.ReorderPagesError = errMock

	body := bytes.NewBufferString(`{"page_ids":["p1"]}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/books/b1/pages/reorder", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.ReorderPages(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to reorder pages")
}

// --- Slots ---

func TestBooksHandler_AssignSlot_Success(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"photo_uid":"photo1"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1/slots/0", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "0"})
	recorder := httptest.NewRecorder()
	handler.AssignSlot(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var resp map[string]bool
	parseJSONResponse(t, recorder, &resp)
	if !resp["assigned"] {
		t.Error("expected assigned=true")
	}
}

func TestBooksHandler_AssignSlot_InvalidSlotIndex(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"photo_uid":"photo1"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1/slots/abc", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "abc"})
	recorder := httptest.NewRecorder()
	handler.AssignSlot(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid slot index")
}

func TestBooksHandler_AssignSlot_MissingPhotoUID(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1/slots/0", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "0"})
	recorder := httptest.NewRecorder()
	handler.AssignSlot(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "photo_uid, text_content, or captions is required")
}

func TestBooksHandler_AssignSlot_InvalidJSON(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1/slots/0", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "0"})
	recorder := httptest.NewRecorder()
	handler.AssignSlot(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestBooksHandler_AssignSlot_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.AssignSlotError = errMock

	body := bytes.NewBufferString(`{"photo_uid":"photo1"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1/slots/0", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "0"})
	recorder := httptest.NewRecorder()
	handler.AssignSlot(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to assign slot")
}

func TestBooksHandler_SwapSlots_Success(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"slot_a":0,"slot_b":1}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/pages/p1/slots/swap", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1"})
	recorder := httptest.NewRecorder()
	handler.SwapSlots(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var resp map[string]bool
	parseJSONResponse(t, recorder, &resp)
	if !resp["swapped"] {
		t.Error("expected swapped=true")
	}
}

func TestBooksHandler_SwapSlots_SameSlot(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"slot_a":1,"slot_b":1}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/pages/p1/slots/swap", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1"})
	recorder := httptest.NewRecorder()
	handler.SwapSlots(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "slots must be different")
}

func TestBooksHandler_SwapSlots_InvalidJSON(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/pages/p1/slots/swap", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1"})
	recorder := httptest.NewRecorder()
	handler.SwapSlots(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestBooksHandler_SwapSlots_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.SwapSlotsError = errMock

	body := bytes.NewBufferString(`{"slot_a":0,"slot_b":1}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/pages/p1/slots/swap", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1"})
	recorder := httptest.NewRecorder()
	handler.SwapSlots(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to swap slots")
}

func TestBooksHandler_ClearSlot_Success(t *testing.T) {
	_, handler := setupBookTest(t)

	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/pages/p1/slots/2", nil)
	req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "2"})
	recorder := httptest.NewRecorder()
	handler.ClearSlot(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var resp map[string]bool
	parseJSONResponse(t, recorder, &resp)
	if !resp["cleared"] {
		t.Error("expected cleared=true")
	}
}

func TestBooksHandler_ClearSlot_InvalidSlotIndex(t *testing.T) {
	_, handler := setupBookTest(t)

	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/pages/p1/slots/xyz", nil)
	req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "xyz"})
	recorder := httptest.NewRecorder()
	handler.ClearSlot(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid slot index")
}

func TestBooksHandler_ClearSlot_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.ClearSlotError = errMock

	req := httptest.NewRequestWithContext(context.Background(), "DELETE", "/api/v1/pages/p1/slots/0", nil)
	req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "0"})
	recorder := httptest.NewRecorder()
	handler.ClearSlot(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to clear slot")
}

// --- Memberships ---

func TestBooksHandler_GetPhotoBookMemberships_Success(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.SetMemberships("photo1", []database.PhotoBookMembership{
		{BookID: "b1", BookTitle: "Book 1", SectionID: "s1", SectionTitle: "Section 1"},
		{BookID: "b2", BookTitle: "Book 2", SectionID: "s2", SectionTitle: "Section 2"},
	})

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/photo1/books", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "photo1"})
	recorder := httptest.NewRecorder()
	handler.GetPhotoBookMemberships(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var resp []photoBookMembershipResponse
	parseJSONResponse(t, recorder, &resp)
	if len(resp) != 2 {
		t.Fatalf("expected 2 memberships, got %d", len(resp))
	}
	if resp[0].BookID != "b1" {
		t.Errorf("expected book_id 'b1', got '%s'", resp[0].BookID)
	}
	if resp[0].SectionTitle != "Section 1" {
		t.Errorf("expected section_title 'Section 1', got '%s'", resp[0].SectionTitle)
	}
}

func TestBooksHandler_GetPhotoBookMemberships_Empty(t *testing.T) {
	_, handler := setupBookTest(t)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/photo1/books", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "photo1"})
	recorder := httptest.NewRecorder()
	handler.GetPhotoBookMemberships(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var resp []photoBookMembershipResponse
	parseJSONResponse(t, recorder, &resp)
	if len(resp) != 0 {
		t.Errorf("expected 0 memberships, got %d", len(resp))
	}
}

func TestBooksHandler_GetPhotoBookMemberships_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.GetPhotoBookMembershipsError = errMock

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/photos/photo1/books", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "photo1"})
	recorder := httptest.NewRecorder()
	handler.GetPhotoBookMemberships(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to get book memberships")
}

// --- Writer not available ---

func TestBooksHandler_WriterNotAvailable(t *testing.T) {
	// Deregister the book writer but keep postgres initialized.
	database.RegisterPostgresBackend(nil, nil, nil)
	database.RegisterBookWriter(nil)
	t.Cleanup(func() {
		database.ResetForTesting()
	})

	handler := NewBooksHandler(testConfig(), nil)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/books", nil)
	recorder := httptest.NewRecorder()
	handler.ListBooks(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "book storage not available")
}

// --- Page Style Tests ---

func TestBooksHandler_CreatePage_WithStyle(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"format":"4_landscape","section_id":"s1","style":"archival"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/books/b1/pages", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.CreatePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusCreated)

	var resp pageResponse
	parseJSONResponse(t, recorder, &resp)
	if resp.Style != "archival" {
		t.Errorf("expected style 'archival', got '%s'", resp.Style)
	}
}

func TestBooksHandler_CreatePage_InvalidStyle(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"format":"4_landscape","section_id":"s1","style":"vintage"}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/books/b1/pages", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.CreatePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "style must be 'modern' or 'archival'")
}

func TestBooksHandler_UpdatePage_Style(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.AddPage(database.BookPage{ID: "p1", BookID: "b1", SectionID: "s1", Format: "4_landscape", Style: "modern"})

	body := bytes.NewBufferString(`{"style":"archival"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1"})
	recorder := httptest.NewRecorder()
	handler.UpdatePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	page, _ := mockBW.GetPage(context.TODO(), "p1")
	if page.Style != "archival" {
		t.Errorf("expected style 'archival', got '%s'", page.Style)
	}
}

func TestBooksHandler_UpdatePage_InvalidStyle(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.AddPage(database.BookPage{ID: "p1", BookID: "b1", SectionID: "s1", Format: "4_landscape", Style: "modern"})

	body := bytes.NewBufferString(`{"style":"vintage"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1"})
	recorder := httptest.NewRecorder()
	handler.UpdatePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "style must be 'modern' or 'archival'")
}

func TestBooksHandler_GetBook_PageStyleInResponse(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	now := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	mockBW.AddBook(database.PhotoBook{ID: "b1", Title: "Test Book", CreatedAt: now, UpdatedAt: now})
	mockBW.AddSection(database.BookSection{ID: "s1", BookID: "b1", Title: "Sec 1"})
	mockBW.AddPage(database.BookPage{ID: "p1", BookID: "b1", SectionID: "s1", Format: "4_landscape", Style: "archival"})

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/books/b1", nil)
	req = requestWithChiParams(req, map[string]string{"id": "b1"})
	recorder := httptest.NewRecorder()
	handler.GetBook(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var resp bookDetailResponse
	parseJSONResponse(t, recorder, &resp)
	if len(resp.Pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(resp.Pages))
	}
	if resp.Pages[0].Style != "archival" {
		t.Errorf("expected style 'archival' in response, got '%s'", resp.Pages[0].Style)
	}
}

// --- UpdateSlotCrop ---

func TestBooksHandler_UpdateSlotCrop_Success(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.AddPage(database.BookPage{ID: "p1", BookID: "b1", Format: "4_landscape"})
	mockBW.SetPageSlots("p1", []database.PageSlot{{SlotIndex: 0, PhotoUID: "photo1", CropX: 0.5, CropY: 0.5, CropScale: 1.0}})

	body := bytes.NewBufferString(`{"crop_x":0.3,"crop_y":0.7,"crop_scale":0.8}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1/slots/0/crop", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "0"})
	recorder := httptest.NewRecorder()
	handler.UpdateSlotCrop(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var resp map[string]bool
	parseJSONResponse(t, recorder, &resp)
	if !resp["updated"] {
		t.Error("expected updated=true")
	}
}

func TestBooksHandler_UpdateSlotCrop_InvalidValues(t *testing.T) {
	_, handler := setupBookTest(t)

	tests := []struct {
		name string
		body string
		msg  string
	}{
		{"crop_x too low", `{"crop_x":-0.1,"crop_y":0.5}`, "crop_x and crop_y must be between 0.0 and 1.0"},
		{"crop_x too high", `{"crop_x":1.1,"crop_y":0.5}`, "crop_x and crop_y must be between 0.0 and 1.0"},
		{"crop_y too low", `{"crop_x":0.5,"crop_y":-0.1}`, "crop_x and crop_y must be between 0.0 and 1.0"},
		{"crop_y too high", `{"crop_x":0.5,"crop_y":1.1}`, "crop_x and crop_y must be between 0.0 and 1.0"},
		{"crop_scale too low", `{"crop_x":0.5,"crop_y":0.5,"crop_scale":0.05}`, "crop_scale must be between 0.1 and 1.0"},
		{"crop_scale too high", `{"crop_x":0.5,"crop_y":0.5,"crop_scale":1.5}`, "crop_scale must be between 0.1 and 1.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := bytes.NewBufferString(tt.body)
			req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1/slots/0/crop", body)
			req.Header.Set("Content-Type", "application/json")
			req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "0"})
			recorder := httptest.NewRecorder()
			handler.UpdateSlotCrop(recorder, req)

			assertStatusCode(t, recorder, http.StatusBadRequest)
			assertJSONError(t, recorder, tt.msg)
		})
	}
}

func TestBooksHandler_UpdateSlotCrop_InvalidSlotIndex(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"crop_x":0.5,"crop_y":0.5}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1/slots/abc/crop", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "abc"})
	recorder := httptest.NewRecorder()
	handler.UpdateSlotCrop(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid slot index")
}

func TestBooksHandler_UpdateSlotCrop_InvalidJSON(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1/slots/0/crop", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "0"})
	recorder := httptest.NewRecorder()
	handler.UpdateSlotCrop(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestBooksHandler_UpdateSlotCrop_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.UpdateSlotCropError = errMock

	body := bytes.NewBufferString(`{"crop_x":0.5,"crop_y":0.5}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1/slots/0/crop", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "0"})
	recorder := httptest.NewRecorder()
	handler.UpdateSlotCrop(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to update slot crop")
}

func TestBooksHandler_UpdateSlotCrop_BoundaryValues(t *testing.T) {
	_, handler := setupBookTest(t)

	// All boundary values should pass validation.
	tests := []struct {
		name string
		body string
	}{
		{"min crop", `{"crop_x":0.0,"crop_y":0.0,"crop_scale":0.1}`},
		{"max crop", `{"crop_x":1.0,"crop_y":1.0,"crop_scale":1.0}`},
		{"center", `{"crop_x":0.5,"crop_y":0.5}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := bytes.NewBufferString(tt.body)
			req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1/slots/0/crop", body)
			req.Header.Set("Content-Type", "application/json")
			req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "0"})
			recorder := httptest.NewRecorder()
			handler.UpdateSlotCrop(recorder, req)

			assertStatusCode(t, recorder, http.StatusOK)
		})
	}
}

// --- AssignSlot text_content ---

func TestBooksHandler_AssignSlot_TextContent(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"text_content":"Hello, this is some text for the page."}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1/slots/0", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "0"})
	recorder := httptest.NewRecorder()
	handler.AssignSlot(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var resp map[string]bool
	parseJSONResponse(t, recorder, &resp)
	if !resp["assigned"] {
		t.Error("expected assigned=true")
	}
}

func TestBooksHandler_AssignSlot_BothPhotoAndText(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"photo_uid":"photo1","text_content":"some text"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1/slots/0", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "0"})
	recorder := httptest.NewRecorder()
	handler.AssignSlot(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "slot must have exactly one of photo_uid, text_content, captions")
}

func TestBooksHandler_AssignSlot_Captions(t *testing.T) {
	t.Run("assigns captions slot", func(t *testing.T) {
		mockBW, handler := setupBookTest(t)

		body := bytes.NewBufferString(`{"captions":true}`)
		req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1/slots/2", body)
		req.Header.Set("Content-Type", "application/json")
		req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "2"})
		recorder := httptest.NewRecorder()
		handler.AssignSlot(recorder, req)

		assertStatusCode(t, recorder, http.StatusOK)
		var resp map[string]bool
		parseJSONResponse(t, recorder, &resp)
		if !resp["assigned"] {
			t.Error("expected assigned=true")
		}

		// Verify the slot is marked as captions slot in the mock.
		slots, _ := mockBW.GetPageSlots(context.Background(), "p1")
		var found bool
		for _, s := range slots {
			if s.SlotIndex == 2 && s.IsCaptionsSlot {
				found = true
			}
		}
		if !found {
			t.Error("expected slot 2 to be flagged as captions slot in mock")
		}
	})

	t.Run("rejects captions with photo_uid", func(t *testing.T) {
		_, handler := setupBookTest(t)

		body := bytes.NewBufferString(`{"captions":true,"photo_uid":"photo1"}`)
		req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1/slots/0", body)
		req.Header.Set("Content-Type", "application/json")
		req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "0"})
		recorder := httptest.NewRecorder()
		handler.AssignSlot(recorder, req)

		assertStatusCode(t, recorder, http.StatusBadRequest)
		assertJSONError(t, recorder, "slot must have exactly one of photo_uid, text_content, captions")
	})

	t.Run("conflict on second captions slot", func(t *testing.T) {
		mockBW, handler := setupBookTest(t)
		// Seed an existing captions slot at index 1.
		if err := mockBW.AssignCaptionsSlot(context.Background(), "p1", 1); err != nil {
			t.Fatalf("seed captions slot: %v", err)
		}

		body := bytes.NewBufferString(`{"captions":true}`)
		req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1/slots/2", body)
		req.Header.Set("Content-Type", "application/json")
		req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "2"})
		recorder := httptest.NewRecorder()
		handler.AssignSlot(recorder, req)

		assertStatusCode(t, recorder, http.StatusConflict)
		assertJSONError(t, recorder, "this page already has a captions slot")
	})

	t.Run("reassigning same captions slot index is idempotent", func(t *testing.T) {
		mockBW, handler := setupBookTest(t)
		if err := mockBW.AssignCaptionsSlot(context.Background(), "p1", 1); err != nil {
			t.Fatalf("seed captions slot: %v", err)
		}
		// Calling again on the same slot index must succeed — the uniqueness
		// rule is "at most one captions slot per page", not "cannot reassign".
		body := bytes.NewBufferString(`{"captions":true}`)
		req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1/slots/1", body)
		req.Header.Set("Content-Type", "application/json")
		req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "1"})
		recorder := httptest.NewRecorder()
		handler.AssignSlot(recorder, req)

		assertStatusCode(t, recorder, http.StatusOK)
	})

	t.Run("replacing captions slot with photo clears the flag", func(t *testing.T) {
		mockBW, handler := setupBookTest(t)
		if err := mockBW.AssignCaptionsSlot(context.Background(), "p1", 1); err != nil {
			t.Fatalf("seed captions slot: %v", err)
		}

		body := bytes.NewBufferString(`{"photo_uid":"photo1"}`)
		req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1/slots/1", body)
		req.Header.Set("Content-Type", "application/json")
		req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "1"})
		recorder := httptest.NewRecorder()
		handler.AssignSlot(recorder, req)

		assertStatusCode(t, recorder, http.StatusOK)

		slots, _ := mockBW.GetPageSlots(context.Background(), "p1")
		for _, s := range slots {
			if s.SlotIndex == 1 && s.IsCaptionsSlot {
				t.Error("captions flag must be cleared when slot is reassigned to a photo")
			}
		}
	})
}

// --- UpdatePage split_position ---

func TestBooksHandler_UpdatePage_SplitPosition(t *testing.T) {
	t.Run("set split_position", func(t *testing.T) {
		mockBW, handler := setupBookTest(t)
		mockBW.AddPage(database.BookPage{ID: "p1", BookID: "b1", SectionID: "s1", Format: "2l_1p"})

		body := bytes.NewBufferString(`{"split_position":0.6}`)
		req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1", body)
		req.Header.Set("Content-Type", "application/json")
		req = requestWithChiParams(req, map[string]string{"id": "p1"})
		recorder := httptest.NewRecorder()
		handler.UpdatePage(recorder, req)

		assertStatusCode(t, recorder, http.StatusOK)

		page, _ := mockBW.GetPage(context.Background(), "p1")
		if page.SplitPosition == nil || *page.SplitPosition != 0.6 {
			t.Errorf("expected split_position 0.6, got %v", page.SplitPosition)
		}
	})

	t.Run("clear split_position with null", func(t *testing.T) {
		mockBW, handler := setupBookTest(t)
		sp := 0.6
		mockBW.AddPage(database.BookPage{ID: "p1", BookID: "b1", SectionID: "s1", Format: "2l_1p", SplitPosition: &sp})

		body := bytes.NewBufferString(`{"split_position":null}`)
		req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1", body)
		req.Header.Set("Content-Type", "application/json")
		req = requestWithChiParams(req, map[string]string{"id": "p1"})
		recorder := httptest.NewRecorder()
		handler.UpdatePage(recorder, req)

		assertStatusCode(t, recorder, http.StatusOK)

		page, _ := mockBW.GetPage(context.Background(), "p1")
		if page.SplitPosition != nil {
			t.Errorf("expected nil split_position, got %v", *page.SplitPosition)
		}
	})

	t.Run("invalid split_position below 0.2", func(t *testing.T) {
		mockBW, handler := setupBookTest(t)
		mockBW.AddPage(database.BookPage{ID: "p1", BookID: "b1", SectionID: "s1", Format: "2l_1p"})

		body := bytes.NewBufferString(`{"split_position":0.1}`)
		req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1", body)
		req.Header.Set("Content-Type", "application/json")
		req = requestWithChiParams(req, map[string]string{"id": "p1"})
		recorder := httptest.NewRecorder()
		handler.UpdatePage(recorder, req)

		assertStatusCode(t, recorder, http.StatusBadRequest)
		assertJSONError(t, recorder, "split_position must be between 0.2 and 0.8")
	})

	t.Run("invalid split_position above 0.8", func(t *testing.T) {
		mockBW, handler := setupBookTest(t)
		mockBW.AddPage(database.BookPage{ID: "p1", BookID: "b1", SectionID: "s1", Format: "2l_1p"})

		body := bytes.NewBufferString(`{"split_position":0.9}`)
		req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1", body)
		req.Header.Set("Content-Type", "application/json")
		req = requestWithChiParams(req, map[string]string{"id": "p1"})
		recorder := httptest.NewRecorder()
		handler.UpdatePage(recorder, req)

		assertStatusCode(t, recorder, http.StatusBadRequest)
		assertJSONError(t, recorder, "split_position must be between 0.2 and 0.8")
	})

	t.Run("boundary values pass", func(t *testing.T) {
		mockBW, handler := setupBookTest(t)
		mockBW.AddPage(database.BookPage{ID: "p1", BookID: "b1", SectionID: "s1", Format: "2l_1p"})

		// 0.2 is valid
		body := bytes.NewBufferString(`{"split_position":0.2}`)
		req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1", body)
		req.Header.Set("Content-Type", "application/json")
		req = requestWithChiParams(req, map[string]string{"id": "p1"})
		recorder := httptest.NewRecorder()
		handler.UpdatePage(recorder, req)
		assertStatusCode(t, recorder, http.StatusOK)

		// 0.8 is valid
		body = bytes.NewBufferString(`{"split_position":0.8}`)
		req = httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/pages/p1", body)
		req.Header.Set("Content-Type", "application/json")
		req = requestWithChiParams(req, map[string]string{"id": "p1"})
		recorder = httptest.NewRecorder()
		handler.UpdatePage(recorder, req)
		assertStatusCode(t, recorder, http.StatusOK)
	})
}

// --- Auto-Layout Algorithm Tests ---

func allFormats() map[string]bool {
	return map[string]bool{
		"4_landscape": true, "2l_1p": true, "1p_2l": true,
		"2_portrait": true, "1_fullscreen": true,
	}
}

func TestComputeAutoLayout_FourLandscapes(t *testing.T) {
	specs := computeAutoLayout([]string{"l1", "l2", "l3", "l4"}, nil, allFormats(), 0)
	if len(specs) != 1 {
		t.Fatalf("expected 1 page, got %d", len(specs))
	}
	if specs[0].format != "4_landscape" {
		t.Errorf("expected 4_landscape, got %s", specs[0].format)
	}
	if len(specs[0].photos) != 4 {
		t.Errorf("expected 4 photos, got %d", len(specs[0].photos))
	}
}

func TestComputeAutoLayout_MixedAlternates(t *testing.T) {
	// 3 landscapes + 2 portraits: not enough for 4_landscape, so mixed pages
	specs := computeAutoLayout([]string{"l1", "l2", "l3"}, []string{"p1", "p2"}, allFormats(), 0)
	if len(specs) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(specs))
	}
	if specs[0].format != "2l_1p" {
		t.Errorf("expected 2l_1p, got %s", specs[0].format)
	}
	// Remaining 1 landscape + 1 portrait → 2_portrait
	if specs[1].format != "2_portrait" {
		t.Errorf("expected 2_portrait for remainder, got %s", specs[1].format)
	}
}

func TestComputeAutoLayout_MixedAlternatesMultiple(t *testing.T) {
	// 5 landscapes + 2 portraits: 4_landscape + 2l_1p (2L+1P) + remaining 1P → 1_fullscreen
	// Actually: 4L→4_landscape, then 1L+2P remaining, not enough for mixed (needs 2L+1P)
	// So: 4_landscape + 2_portrait + 1_fullscreen
	specs := computeAutoLayout(
		[]string{"l1", "l2", "l3", "l4", "l5"},
		[]string{"p1", "p2"},
		allFormats(), 0,
	)
	if len(specs) != 3 {
		t.Fatalf("expected 3 pages, got %d", len(specs))
	}
	if specs[0].format != "4_landscape" {
		t.Errorf("expected 4_landscape, got %s", specs[0].format)
	}
}

func TestComputeAutoLayout_TwoPortraits(t *testing.T) {
	specs := computeAutoLayout(nil, []string{"p1", "p2"}, allFormats(), 0)
	if len(specs) != 1 {
		t.Fatalf("expected 1 page, got %d", len(specs))
	}
	if specs[0].format != "2_portrait" {
		t.Errorf("expected 2_portrait, got %s", specs[0].format)
	}
}

func TestComputeAutoLayout_SinglesGoFullscreen(t *testing.T) {
	specs := computeAutoLayout([]string{"l1"}, []string{"p1"}, allFormats(), 0)
	// 1 landscape + 1 portrait: should pair as 2_portrait
	if len(specs) != 1 {
		t.Fatalf("expected 1 page, got %d", len(specs))
	}
	if specs[0].format != "2_portrait" {
		t.Errorf("expected 2_portrait, got %s", specs[0].format)
	}
}

func TestComputeAutoLayout_MaxPages(t *testing.T) {
	specs := computeAutoLayout(
		[]string{"l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8"},
		nil, allFormats(), 1,
	)
	if len(specs) != 1 {
		t.Fatalf("expected 1 page (max_pages=1), got %d", len(specs))
	}
}

func TestComputeAutoLayout_RestrictedFormats(t *testing.T) {
	allowed := map[string]bool{"1_fullscreen": true}
	specs := computeAutoLayout([]string{"l1", "l2"}, []string{"p1"}, allowed, 0)
	if len(specs) != 3 {
		t.Fatalf("expected 3 fullscreen pages, got %d", len(specs))
	}
	for _, s := range specs {
		if s.format != "1_fullscreen" {
			t.Errorf("expected 1_fullscreen, got %s", s.format)
		}
	}
}

func TestComputeAutoLayout_EmptyInput(t *testing.T) {
	specs := computeAutoLayout(nil, nil, allFormats(), 0)
	if len(specs) != 0 {
		t.Fatalf("expected 0 pages, got %d", len(specs))
	}
}
