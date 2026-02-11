package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/mock"
)

var errMock = errors.New("mock error")

// setupBookTest registers a MockBookWriter via the database provider system
// and returns it along with a BooksHandler. Cleanup deregisters the mock.
func setupBookTest(t *testing.T) (*mock.MockBookWriter, *BooksHandler) {
	t.Helper()

	mockBW := mock.NewMockBookWriter()

	// Register the mock as the postgres backend so getBookWriter() works
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

	req := httptest.NewRequest("GET", "/api/v1/books", nil)
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

	req := httptest.NewRequest("GET", "/api/v1/books", nil)
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

	req := httptest.NewRequest("GET", "/api/v1/books", nil)
	recorder := httptest.NewRecorder()
	handler.ListBooks(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to list books")
}

func TestBooksHandler_CreateBook_Success(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"title":"My Book","description":"A description"}`)
	req := httptest.NewRequest("POST", "/api/v1/books", body)
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
	req := httptest.NewRequest("POST", "/api/v1/books", body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.CreateBook(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "title is required")
}

func TestBooksHandler_CreateBook_InvalidJSON(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequest("POST", "/api/v1/books", body)
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
	req := httptest.NewRequest("POST", "/api/v1/books", body)
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

	req := httptest.NewRequest("GET", "/api/v1/books/b1", nil)
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

	req := httptest.NewRequest("GET", "/api/v1/books/nonexistent", nil)
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
	req := httptest.NewRequest("PUT", "/api/v1/books/b1", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/books/nonexistent", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/books/b1", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/books/b1", body)
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

	req := httptest.NewRequest("DELETE", "/api/v1/books/b1", nil)
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

	req := httptest.NewRequest("DELETE", "/api/v1/books/b1", nil)
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
	req := httptest.NewRequest("POST", "/api/v1/books/b1/sections", body)
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
	req := httptest.NewRequest("POST", "/api/v1/books/b1/sections", body)
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
	req := httptest.NewRequest("POST", "/api/v1/books/b1/sections", body)
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
	req := httptest.NewRequest("POST", "/api/v1/books/b1/sections", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/sections/s1", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/sections/s1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "s1"})
	recorder := httptest.NewRecorder()
	handler.UpdateSection(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestBooksHandler_UpdateSection_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.UpdateSectionError = errMock

	body := bytes.NewBufferString(`{"title":"Updated"}`)
	req := httptest.NewRequest("PUT", "/api/v1/sections/s1", body)
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

	req := httptest.NewRequest("DELETE", "/api/v1/sections/s1", nil)
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

	req := httptest.NewRequest("DELETE", "/api/v1/sections/s1", nil)
	req = requestWithChiParams(req, map[string]string{"id": "s1"})
	recorder := httptest.NewRecorder()
	handler.DeleteSection(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to delete section")
}

func TestBooksHandler_ReorderSections_Success(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"section_ids":["s2","s1","s3"]}`)
	req := httptest.NewRequest("PUT", "/api/v1/books/b1/sections/reorder", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/books/b1/sections/reorder", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/books/b1/sections/reorder", body)
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

	req := httptest.NewRequest("GET", "/api/v1/sections/s1/photos", nil)
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
}

func TestBooksHandler_GetSectionPhotos_Empty(t *testing.T) {
	_, handler := setupBookTest(t)

	req := httptest.NewRequest("GET", "/api/v1/sections/s1/photos", nil)
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

	req := httptest.NewRequest("GET", "/api/v1/sections/s1/photos", nil)
	req = requestWithChiParams(req, map[string]string{"id": "s1"})
	recorder := httptest.NewRecorder()
	handler.GetSectionPhotos(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to get section photos")
}

func TestBooksHandler_AddSectionPhotos_Success(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"photo_uids":["p1","p2","p3"]}`)
	req := httptest.NewRequest("POST", "/api/v1/sections/s1/photos", body)
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
	req := httptest.NewRequest("POST", "/api/v1/sections/s1/photos", body)
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
	req := httptest.NewRequest("POST", "/api/v1/sections/s1/photos", body)
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
	req := httptest.NewRequest("POST", "/api/v1/sections/s1/photos", body)
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
	req := httptest.NewRequest("DELETE", "/api/v1/sections/s1/photos", body)
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
	req := httptest.NewRequest("DELETE", "/api/v1/sections/s1/photos", body)
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
	req := httptest.NewRequest("DELETE", "/api/v1/sections/s1/photos", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/sections/s1/photos/p1/description", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/sections/s1/photos/p1/description", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/sections/s1/photos/p1/description", body)
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
	req := httptest.NewRequest("POST", "/api/v1/books/b1/pages", body)
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
	req := httptest.NewRequest("POST", "/api/v1/books/b1/pages", body)
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
	req := httptest.NewRequest("POST", "/api/v1/books/b1/pages", body)
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
	req := httptest.NewRequest("POST", "/api/v1/books/b1/pages", body)
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
	req := httptest.NewRequest("POST", "/api/v1/books/b1/pages", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/pages/p1", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/pages/nonexistent", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/pages/p1", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/pages/p1", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/pages/p1", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/pages/p1", body)
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
	// Assign 4 slots (indices 0-3)
	ctx := context.TODO()
	for i := range 4 {
		_ = mockBW.AssignSlot(ctx, "p1", i, fmt.Sprintf("photo%d", i))
	}

	// Change format to 2_portrait (2 slots) — slots 2 and 3 should be cleared
	body := bytes.NewBufferString(`{"format":"2_portrait"}`)
	req := httptest.NewRequest("PUT", "/api/v1/pages/p1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1"})
	recorder := httptest.NewRecorder()
	handler.UpdatePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	// Verify page format was updated
	page, _ := mockBW.GetPage(ctx, "p1")
	if page.Format != "2_portrait" {
		t.Errorf("expected format '2_portrait', got '%s'", page.Format)
	}

	// Verify slots 0 and 1 still have photos
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
	// Assign 2 slots
	ctx := context.TODO()
	_ = mockBW.AssignSlot(ctx, "p1", 0, "photoA")
	_ = mockBW.AssignSlot(ctx, "p1", 1, "photoB")

	// Change format to 4_landscape (4 slots) — both existing slots should be preserved
	body := bytes.NewBufferString(`{"format":"4_landscape"}`)
	req := httptest.NewRequest("PUT", "/api/v1/pages/p1", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1"})
	recorder := httptest.NewRecorder()
	handler.UpdatePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	// Verify page format was updated
	page, _ := mockBW.GetPage(ctx, "p1")
	if page.Format != "4_landscape" {
		t.Errorf("expected format '4_landscape', got '%s'", page.Format)
	}

	// Verify both original slots are preserved
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

	req := httptest.NewRequest("DELETE", "/api/v1/pages/p1", nil)
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

	req := httptest.NewRequest("DELETE", "/api/v1/pages/p1", nil)
	req = requestWithChiParams(req, map[string]string{"id": "p1"})
	recorder := httptest.NewRecorder()
	handler.DeletePage(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to delete page")
}

func TestBooksHandler_ReorderPages_Success(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{"page_ids":["p3","p1","p2"]}`)
	req := httptest.NewRequest("PUT", "/api/v1/books/b1/pages/reorder", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/books/b1/pages/reorder", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/books/b1/pages/reorder", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/pages/p1/slots/0", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/pages/p1/slots/abc", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/pages/p1/slots/0", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "0"})
	recorder := httptest.NewRecorder()
	handler.AssignSlot(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "photo_uid is required")
}

func TestBooksHandler_AssignSlot_InvalidJSON(t *testing.T) {
	_, handler := setupBookTest(t)

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequest("PUT", "/api/v1/pages/p1/slots/0", body)
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
	req := httptest.NewRequest("PUT", "/api/v1/pages/p1/slots/0", body)
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
	req := httptest.NewRequest("POST", "/api/v1/pages/p1/slots/swap", body)
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
	req := httptest.NewRequest("POST", "/api/v1/pages/p1/slots/swap", body)
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
	req := httptest.NewRequest("POST", "/api/v1/pages/p1/slots/swap", body)
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
	req := httptest.NewRequest("POST", "/api/v1/pages/p1/slots/swap", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"id": "p1"})
	recorder := httptest.NewRecorder()
	handler.SwapSlots(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to swap slots")
}

func TestBooksHandler_ClearSlot_Success(t *testing.T) {
	_, handler := setupBookTest(t)

	req := httptest.NewRequest("DELETE", "/api/v1/pages/p1/slots/2", nil)
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

	req := httptest.NewRequest("DELETE", "/api/v1/pages/p1/slots/xyz", nil)
	req = requestWithChiParams(req, map[string]string{"id": "p1", "index": "xyz"})
	recorder := httptest.NewRecorder()
	handler.ClearSlot(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid slot index")
}

func TestBooksHandler_ClearSlot_BackendError(t *testing.T) {
	mockBW, handler := setupBookTest(t)
	mockBW.ClearSlotError = errMock

	req := httptest.NewRequest("DELETE", "/api/v1/pages/p1/slots/0", nil)
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

	req := httptest.NewRequest("GET", "/api/v1/photos/photo1/books", nil)
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

	req := httptest.NewRequest("GET", "/api/v1/photos/photo1/books", nil)
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

	req := httptest.NewRequest("GET", "/api/v1/photos/photo1/books", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "photo1"})
	recorder := httptest.NewRecorder()
	handler.GetPhotoBookMemberships(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to get book memberships")
}

// --- Writer not available ---

func TestBooksHandler_WriterNotAvailable(t *testing.T) {
	// Deregister the book writer but keep postgres initialized
	database.RegisterPostgresBackend(nil, nil, nil)
	database.RegisterBookWriter(nil)
	t.Cleanup(func() {
		database.ResetForTesting()
	})

	handler := NewBooksHandler(testConfig(), nil)

	req := httptest.NewRequest("GET", "/api/v1/books", nil)
	recorder := httptest.NewRecorder()
	handler.ListBooks(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "book storage not available")
}
