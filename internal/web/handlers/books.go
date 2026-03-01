package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/latex"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// BooksHandler handles photo book endpoints.
type BooksHandler struct {
	config         *config.Config
	sessionManager *middleware.SessionManager
}

// NewBooksHandler creates a new books handler.
func NewBooksHandler(cfg *config.Config, sm *middleware.SessionManager) *BooksHandler {
	return &BooksHandler{config: cfg, sessionManager: sm}
}

func getBookWriter(r *http.Request, w http.ResponseWriter) database.BookWriter {
	writer, err := database.GetBookWriter(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "book storage not available")
		return nil
	}
	return writer
}

// --- Book responses ---

type bookResponse struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	SectionCount int    `json:"section_count"`
	PageCount    int    `json:"page_count"`
	PhotoCount   int    `json:"photo_count"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type bookDetailResponse struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Chapters    []chapterResponse `json:"chapters"`
	Sections    []sectionResponse `json:"sections"`
	Pages       []pageResponse    `json:"pages"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
}

type chapterResponse struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	SortOrder int    `json:"sort_order"`
}

type sectionResponse struct {
	ID         string `json:"id"`
	ChapterID  string `json:"chapter_id"`
	Title      string `json:"title"`
	SortOrder  int    `json:"sort_order"`
	PhotoCount int    `json:"photo_count"`
}

type sectionPhotoResponse struct {
	PhotoUID    string `json:"photo_uid"`
	Description string `json:"description"`
	Note        string `json:"note"`
	AddedAt     string `json:"added_at"`
}

type pageResponse struct {
	ID            string         `json:"id"`
	SectionID     string         `json:"section_id"`
	Format        string         `json:"format"`
	Style         string         `json:"style"`
	Description   string         `json:"description"`
	SplitPosition *float64       `json:"split_position"`
	SortOrder     int            `json:"sort_order"`
	Slots         []slotResponse `json:"slots"`
}

type slotResponse struct {
	SlotIndex   int     `json:"slot_index"`
	PhotoUID    string  `json:"photo_uid"`
	TextContent string  `json:"text_content"`
	CropX       float64 `json:"crop_x"`
	CropY       float64 `json:"crop_y"`
	CropScale   float64 `json:"crop_scale"`
}

// --- Photo Book Memberships ---

type photoBookMembershipResponse struct {
	BookID       string `json:"book_id"`
	BookTitle    string `json:"book_title"`
	SectionID    string `json:"section_id"`
	SectionTitle string `json:"section_title"`
}

// GetPhotoBookMemberships handles GET /api/v1/photos/:uid/books and returns book/section memberships for a photo.
func (h *BooksHandler) GetPhotoBookMemberships(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	photoUID := chi.URLParam(r, "uid")
	memberships, err := bw.GetPhotoBookMemberships(r.Context(), photoUID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get book memberships")
		return
	}
	result := make([]photoBookMembershipResponse, len(memberships))
	for i, m := range memberships {
		result[i] = photoBookMembershipResponse{
			BookID:       m.BookID,
			BookTitle:    m.BookTitle,
			SectionID:    m.SectionID,
			SectionTitle: m.SectionTitle,
		}
	}
	respondJSON(w, http.StatusOK, result)
}

// --- Books CRUD ---

// ListBooks handles GET /api/v1/books and returns all photo books with counts.
func (h *BooksHandler) ListBooks(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	books, err := bw.ListBooksWithCounts(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list books")
		return
	}

	result := make([]bookResponse, len(books))
	for i, b := range books {
		result[i] = bookResponse{
			ID:           b.ID,
			Title:        b.Title,
			Description:  b.Description,
			SectionCount: b.SectionCount,
			PageCount:    b.PageCount,
			PhotoCount:   b.PhotoCount,
			CreatedAt:    b.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt:    b.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}
	respondJSON(w, http.StatusOK, result)
}

// CreateBook handles POST /api/v1/books and creates a new photo book.
func (h *BooksHandler) CreateBook(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if req.Title == "" {
		respondError(w, http.StatusBadRequest, "title is required")
		return
	}

	book := &database.PhotoBook{Title: req.Title, Description: req.Description}
	if err := bw.CreateBook(r.Context(), book); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create book")
		return
	}
	respondJSON(w, http.StatusCreated, bookResponse{
		ID:          book.ID,
		Title:       book.Title,
		Description: book.Description,
		CreatedAt:   book.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   book.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// GetBook handles GET /api/v1/books/:id and returns a book with its chapters, sections, and pages.
func (h *BooksHandler) GetBook(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	id := chi.URLParam(r, "id")
	book, err := bw.GetBook(r.Context(), id)
	if err != nil || book == nil {
		respondError(w, http.StatusNotFound, "book not found")
		return
	}

	chapters, err2 := bw.GetChapters(r.Context(), id)
	if err2 != nil {
		respondError(w, http.StatusInternalServerError, "failed to get chapters")
		return
	}
	sections, err2 := bw.GetSections(r.Context(), id)
	if err2 != nil {
		respondError(w, http.StatusInternalServerError, "failed to get sections")
		return
	}
	pages, err2 := bw.GetPages(r.Context(), id)
	if err2 != nil {
		respondError(w, http.StatusInternalServerError, "failed to get pages")
		return
	}

	respondJSON(w, http.StatusOK, buildBookDetailResponse(book, chapters, sections, pages))
}

func buildBookDetailResponse(
	book *database.PhotoBook, chapters []database.BookChapter,
	sections []database.BookSection, pages []database.BookPage,
) bookDetailResponse {
	chapterResps := make([]chapterResponse, len(chapters))
	for i, c := range chapters {
		chapterResps[i] = chapterResponse{
			ID:        c.ID,
			Title:     c.Title,
			SortOrder: c.SortOrder,
		}
	}

	sectionResps := make([]sectionResponse, len(sections))
	for i, s := range sections {
		sectionResps[i] = sectionResponse{
			ID:         s.ID,
			ChapterID:  s.ChapterID,
			Title:      s.Title,
			SortOrder:  s.SortOrder,
			PhotoCount: s.PhotoCount,
		}
	}

	pageResps := make([]pageResponse, len(pages))
	for i := range pages {
		p := &pages[i]
		slots := make([]slotResponse, len(p.Slots))
		for j := range p.Slots {
			slots[j] = slotResponse{
				SlotIndex:   p.Slots[j].SlotIndex,
				PhotoUID:    p.Slots[j].PhotoUID,
				TextContent: p.Slots[j].TextContent,
				CropX:       p.Slots[j].CropX,
				CropY:       p.Slots[j].CropY,
				CropScale:   p.Slots[j].CropScale,
			}
		}
		pageResps[i] = pageResponse{
			ID:            p.ID,
			SectionID:     p.SectionID,
			Format:        p.Format,
			Style:         p.Style,
			Description:   p.Description,
			SplitPosition: p.SplitPosition,
			SortOrder:     p.SortOrder,
			Slots:         slots,
		}
	}

	return bookDetailResponse{
		ID:          book.ID,
		Title:       book.Title,
		Description: book.Description,
		Chapters:    chapterResps,
		Sections:    sectionResps,
		Pages:       pageResps,
		CreatedAt:   book.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   book.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// UpdateBook handles PUT /api/v1/books/:id and updates a book's title and description.
func (h *BooksHandler) UpdateBook(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	id := chi.URLParam(r, "id")
	book, err := bw.GetBook(r.Context(), id)
	if err != nil || book == nil {
		respondError(w, http.StatusNotFound, "book not found")
		return
	}
	var req struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if req.Title != nil {
		book.Title = *req.Title
	}
	if req.Description != nil {
		book.Description = *req.Description
	}
	if err := bw.UpdateBook(r.Context(), book); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update book")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"id": book.ID})
}

// DeleteBook handles DELETE /api/v1/books/:id and deletes a book and all its contents.
func (h *BooksHandler) DeleteBook(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	id := chi.URLParam(r, "id")
	if err := bw.DeleteBook(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete book")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// --- Chapters ---

// CreateChapter handles POST /api/v1/books/:id/chapters and creates a chapter in a book.
func (h *BooksHandler) CreateChapter(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	bookID := chi.URLParam(r, "id")
	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if req.Title == "" {
		respondError(w, http.StatusBadRequest, "title is required")
		return
	}
	chapter := &database.BookChapter{BookID: bookID, Title: req.Title}
	if err := bw.CreateChapter(r.Context(), chapter); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create chapter")
		return
	}
	respondJSON(w, http.StatusCreated, chapterResponse{
		ID:        chapter.ID,
		Title:     chapter.Title,
		SortOrder: chapter.SortOrder,
	})
}

// UpdateChapter handles PUT /api/v1/chapters/:id and updates a chapter's title.
func (h *BooksHandler) UpdateChapter(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	chapter := &database.BookChapter{ID: id, Title: req.Title}
	if err := bw.UpdateChapter(r.Context(), chapter); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update chapter")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"id": id})
}

// DeleteChapter handles DELETE /api/v1/chapters/:id and deletes a chapter.
func (h *BooksHandler) DeleteChapter(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	id := chi.URLParam(r, "id")
	if err := bw.DeleteChapter(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete chapter")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ReorderChapters handles PUT /api/v1/books/:id/chapters/reorder and reorders chapters in a book.
func (h *BooksHandler) ReorderChapters(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	bookID := chi.URLParam(r, "id")
	var req struct {
		ChapterIDs []string `json:"chapter_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if err := bw.ReorderChapters(r.Context(), bookID, req.ChapterIDs); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to reorder chapters")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"reordered": true})
}

// --- Sections ---

// CreateSection handles POST /api/v1/books/:id/sections and creates a section in a book.
func (h *BooksHandler) CreateSection(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	bookID := chi.URLParam(r, "id")
	var req struct {
		Title     string `json:"title"`
		ChapterID string `json:"chapter_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if req.Title == "" {
		respondError(w, http.StatusBadRequest, "title is required")
		return
	}
	section := &database.BookSection{BookID: bookID, Title: req.Title, ChapterID: req.ChapterID}
	if err := bw.CreateSection(r.Context(), section); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create section")
		return
	}
	respondJSON(w, http.StatusCreated, sectionResponse{
		ID:        section.ID,
		ChapterID: section.ChapterID,
		Title:     section.Title,
		SortOrder: section.SortOrder,
	})
}

// UpdateSection handles PUT /api/v1/sections/:id and updates a section's title and chapter assignment.
func (h *BooksHandler) UpdateSection(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Title     *string `json:"title"`
		ChapterID *string `json:"chapter_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	// Read existing section to preserve fields not being updated.
	section, err := bw.GetSection(r.Context(), id)
	if err != nil || section == nil {
		respondError(w, http.StatusNotFound, "section not found")
		return
	}
	if req.Title != nil {
		section.Title = *req.Title
	}
	if req.ChapterID != nil {
		section.ChapterID = *req.ChapterID // empty string = unassign from chapter
	}
	if err := bw.UpdateSection(r.Context(), section); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update section")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"id": id})
}

// DeleteSection handles DELETE /api/v1/sections/:id and deletes a section.
func (h *BooksHandler) DeleteSection(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	id := chi.URLParam(r, "id")
	if err := bw.DeleteSection(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete section")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ReorderSections handles PUT /api/v1/books/:id/sections/reorder and reorders sections in a book.
func (h *BooksHandler) ReorderSections(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	bookID := chi.URLParam(r, "id")
	var req struct {
		SectionIDs []string `json:"section_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if err := bw.ReorderSections(r.Context(), bookID, req.SectionIDs); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to reorder sections")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"reordered": true})
}

// --- Section Photos ---

// GetSectionPhotos handles GET /api/v1/sections/:id/photos and returns photos in a section.
func (h *BooksHandler) GetSectionPhotos(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	sectionID := chi.URLParam(r, "id")
	photos, err := bw.GetSectionPhotos(r.Context(), sectionID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get section photos")
		return
	}
	result := make([]sectionPhotoResponse, len(photos))
	for i, p := range photos {
		result[i] = sectionPhotoResponse{
			PhotoUID:    p.PhotoUID,
			Description: p.Description,
			Note:        p.Note,
			AddedAt:     p.AddedAt.Format("2006-01-02T15:04:05Z"),
		}
	}
	respondJSON(w, http.StatusOK, result)
}

// AddSectionPhotos handles POST /api/v1/sections/:id/photos and adds photos to a section.
func (h *BooksHandler) AddSectionPhotos(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	sectionID := chi.URLParam(r, "id")
	var req struct {
		PhotoUIDs []string `json:"photo_uids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if len(req.PhotoUIDs) == 0 {
		respondError(w, http.StatusBadRequest, "photo_uids is required")
		return
	}
	if err := bw.AddSectionPhotos(r.Context(), sectionID, req.PhotoUIDs); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to add photos")
		return
	}
	respondJSON(w, http.StatusOK, map[string]int{"added": len(req.PhotoUIDs)})
}

// RemoveSectionPhotos handles DELETE /api/v1/sections/:id/photos and removes photos from a section.
func (h *BooksHandler) RemoveSectionPhotos(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	sectionID := chi.URLParam(r, "id")
	var req struct {
		PhotoUIDs []string `json:"photo_uids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if err := bw.RemoveSectionPhotos(r.Context(), sectionID, req.PhotoUIDs); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to remove photos")
		return
	}
	respondJSON(w, http.StatusOK, map[string]int{"removed": len(req.PhotoUIDs)})
}

// UpdatePhotoDescription handles PUT /api/v1/sections/:id/photos/:photoUid/description
// and updates a photo's description and note in a section.
func (h *BooksHandler) UpdatePhotoDescription(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	sectionID := chi.URLParam(r, "id")
	photoUID := chi.URLParam(r, "photoUid")
	var req struct {
		Description string `json:"description"`
		Note        string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if err := bw.UpdateSectionPhoto(r.Context(), sectionID, photoUID, req.Description, req.Note); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update photo")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

// --- Pages ---

// CreatePage handles POST /api/v1/books/:id/pages and creates a page in a book.
func (h *BooksHandler) CreatePage(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	bookID := chi.URLParam(r, "id")
	var req struct {
		Format    string `json:"format"`
		SectionID string `json:"section_id"`
		Style     string `json:"style"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if database.PageFormatSlotCount(req.Format) == 0 {
		respondError(w, http.StatusBadRequest, "invalid format")
		return
	}
	if req.SectionID == "" {
		respondError(w, http.StatusBadRequest, "section_id is required")
		return
	}
	if req.Style != "" && req.Style != "modern" && req.Style != "archival" {
		respondError(w, http.StatusBadRequest, "style must be 'modern' or 'archival'")
		return
	}
	page := &database.BookPage{BookID: bookID, SectionID: req.SectionID, Format: req.Format, Style: req.Style}
	if err := bw.CreatePage(r.Context(), page); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create page")
		return
	}
	respondJSON(w, http.StatusCreated, pageResponse{
		ID:            page.ID,
		SectionID:     page.SectionID,
		Format:        page.Format,
		Style:         page.Style,
		Description:   page.Description,
		SplitPosition: page.SplitPosition,
		SortOrder:     page.SortOrder,
		Slots:         []slotResponse{},
	})
}

// updatePageRequest holds the parsed update page request fields.
// SplitPosition uses json.RawMessage to distinguish "not sent" (nil) from "sent as null" ([]byte("null")).
type updatePageRequest struct {
	Format        *string         `json:"format"`
	SectionID     *string         `json:"section_id"`
	Description   *string         `json:"description"`
	Style         *string         `json:"style"`
	SplitPosition json.RawMessage `json:"split_position"`
}

// applyPageUpdates applies the request fields to the page, returning an error message if validation fails.
func applyPageUpdates(page *database.BookPage, req updatePageRequest) string {
	if req.Format != nil {
		if errMsg := applyFormatUpdate(page, *req.Format); errMsg != "" {
			return errMsg
		}
	}
	if req.SectionID != nil {
		if *req.SectionID == "" {
			return "section_id is required"
		}
		page.SectionID = *req.SectionID
	}
	if req.Description != nil {
		page.Description = *req.Description
	}
	if req.Style != nil {
		if errMsg := applyStyleUpdate(page, *req.Style); errMsg != "" {
			return errMsg
		}
	}
	if req.SplitPosition != nil {
		if errMsg := applySplitPosition(page, req.SplitPosition); errMsg != "" {
			return errMsg
		}
	}
	return ""
}

func applyFormatUpdate(page *database.BookPage, format string) string {
	if database.PageFormatSlotCount(format) == 0 {
		return "invalid format"
	}
	page.Format = format
	if format == "1_fullscreen" {
		page.SplitPosition = nil
	}
	return ""
}

func applyStyleUpdate(page *database.BookPage, style string) string {
	if style != "modern" && style != "archival" {
		return "style must be 'modern' or 'archival'"
	}
	page.Style = style
	return ""
}

func applySplitPosition(page *database.BookPage, raw json.RawMessage) string {
	if string(raw) == "null" {
		page.SplitPosition = nil
		return ""
	}
	var sp float64
	if err := json.Unmarshal(raw, &sp); err != nil {
		return "split_position must be a number or null"
	}
	if sp < 0.2 || sp > 0.8 {
		return "split_position must be between 0.2 and 0.8"
	}
	page.SplitPosition = &sp
	return ""
}

// UpdatePage handles PUT /api/v1/pages/:id and updates a page's format,
// section, style, description, and split position.
func (h *BooksHandler) UpdatePage(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	id := chi.URLParam(r, "id")
	var req updatePageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}

	// Fetch existing page to preserve fields not being updated.
	page, err := bw.GetPage(r.Context(), id)
	if err != nil || page == nil {
		respondError(w, http.StatusNotFound, "page not found")
		return
	}

	oldSlotCount := database.PageFormatSlotCount(page.Format)

	if errMsg := applyPageUpdates(page, req); errMsg != "" {
		respondError(w, http.StatusBadRequest, errMsg)
		return
	}
	if err := bw.UpdatePage(r.Context(), page); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update page")
		return
	}

	// If format changed to fewer slots, clear excess slots.
	if req.Format != nil {
		newSlotCount := database.PageFormatSlotCount(*req.Format)
		for i := newSlotCount; i < oldSlotCount; i++ {
			if err := bw.ClearSlot(r.Context(), id, i); err != nil {
				log.Printf("warning: failed to clear excess slot %d on page %s: %v", i, sanitizeForLog(id), err)
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]string{"id": id})
}

// DeletePage handles DELETE /api/v1/pages/:id and deletes a page.
func (h *BooksHandler) DeletePage(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	id := chi.URLParam(r, "id")
	if err := bw.DeletePage(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete page")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ReorderPages handles PUT /api/v1/books/:id/pages/reorder and reorders pages in a book.
func (h *BooksHandler) ReorderPages(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	bookID := chi.URLParam(r, "id")
	var req struct {
		PageIDs []string `json:"page_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if err := bw.ReorderPages(r.Context(), bookID, req.PageIDs); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to reorder pages")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"reordered": true})
}

// --- Slots ---

type assignSlotRequest struct {
	PhotoUID    string `json:"photo_uid"`
	TextContent string `json:"text_content"`
}

func (r assignSlotRequest) validate() string {
	if r.PhotoUID != "" && r.TextContent != "" {
		return "slot must have either photo_uid or text_content, not both"
	}
	if r.PhotoUID == "" && r.TextContent == "" {
		return "photo_uid or text_content is required"
	}
	return ""
}

// AssignSlot handles PUT /api/v1/pages/:id/slots/:index and assigns a photo or text content to a page slot.
func (h *BooksHandler) AssignSlot(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	pageID := chi.URLParam(r, "id")
	slotIndex, err := strconv.Atoi(chi.URLParam(r, "index"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid slot index")
		return
	}
	var req assignSlotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if errMsg := req.validate(); errMsg != "" {
		respondError(w, http.StatusBadRequest, errMsg)
		return
	}
	if req.TextContent != "" {
		if err := bw.AssignTextSlot(r.Context(), pageID, slotIndex, req.TextContent); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to assign text slot")
			return
		}
	} else {
		if err := bw.AssignSlot(r.Context(), pageID, slotIndex, req.PhotoUID); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to assign slot")
			return
		}
	}
	respondJSON(w, http.StatusOK, map[string]bool{"assigned": true})
}

// SwapSlots handles POST /api/v1/pages/:id/slots/swap and swaps two page slots atomically.
func (h *BooksHandler) SwapSlots(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	pageID := chi.URLParam(r, "id")
	var req struct {
		SlotA int `json:"slot_a"`
		SlotB int `json:"slot_b"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if req.SlotA == req.SlotB {
		respondError(w, http.StatusBadRequest, "slots must be different")
		return
	}
	if err := bw.SwapSlots(r.Context(), pageID, req.SlotA, req.SlotB); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to swap slots")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"swapped": true})
}

func validateCropParams(cropX, cropY float64, cropScalePtr *float64) (float64, string) {
	if cropX < 0 || cropX > 1 || cropY < 0 || cropY > 1 {
		return 0, "crop_x and crop_y must be between 0.0 and 1.0"
	}
	cropScale := 1.0
	if cropScalePtr != nil {
		cropScale = *cropScalePtr
	}
	if cropScale < 0.1 || cropScale > 1.0 {
		return 0, "crop_scale must be between 0.1 and 1.0"
	}
	return cropScale, ""
}

// UpdateSlotCrop handles PUT /api/v1/pages/:id/slots/:index/crop
// and updates the crop position and scale for a page slot.
func (h *BooksHandler) UpdateSlotCrop(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	pageID := chi.URLParam(r, "id")
	slotIndex, err := strconv.Atoi(chi.URLParam(r, "index"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid slot index")
		return
	}
	var req struct {
		CropX     float64  `json:"crop_x"`
		CropY     float64  `json:"crop_y"`
		CropScale *float64 `json:"crop_scale"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	cropScale, errMsg := validateCropParams(req.CropX, req.CropY, req.CropScale)
	if errMsg != "" {
		respondError(w, http.StatusBadRequest, errMsg)
		return
	}
	if err := bw.UpdateSlotCrop(r.Context(), pageID, slotIndex, req.CropX, req.CropY, cropScale); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update slot crop")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

// ClearSlot handles DELETE /api/v1/pages/:id/slots/:index and clears a page slot.
func (h *BooksHandler) ClearSlot(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	pageID := chi.URLParam(r, "id")
	slotIndex, err := strconv.Atoi(chi.URLParam(r, "index"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid slot index")
		return
	}
	if err := bw.ClearSlot(r.Context(), pageID, slotIndex); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to clear slot")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"cleared": true})
}

// --- PDF Export ---

func writePDFResponse(w http.ResponseWriter, pdfData []byte, filename string, report *latex.ExportReport) {
	if report != nil && len(report.Warnings) > 0 {
		w.Header().Set("X-Export-Warnings", strconv.Itoa(len(report.Warnings)))
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.pdf"`, filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(pdfData)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	io.Copy(w, bytes.NewReader(pdfData))
}

func handleTestExport(w http.ResponseWriter, r *http.Request, bookTitle string) {
	testPDF, err := latex.GenerateTestPDF(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("test PDF generation failed: %v", err))
		return
	}
	writePDFResponse(w, testPDF, bookTitle+"-test", nil)
}

// ExportPDF handles GET /api/v1/books/:id/export-pdf and exports a book as a PDF via LaTeX.
func (h *BooksHandler) ExportPDF(w http.ResponseWriter, r *http.Request) {
	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}

	id := chi.URLParam(r, "id")
	book, err := bw.GetBook(r.Context(), id)
	if err != nil || book == nil {
		respondError(w, http.StatusNotFound, "book not found")
		return
	}

	if _, err := exec.LookPath("lualatex"); err != nil {
		respondError(w, http.StatusServiceUnavailable, "lualatex is not installed on the server")
		return
	}

	format := r.URL.Query().Get("format")
	if format == "test" {
		handleTestExport(w, r, book.Title)
		return
	}

	// Debug and normal exports both use GeneratePDFWithOptions.
	pdfData, report, err := latex.GeneratePDFWithOptions(r.Context(), pp, bw, id, format == "debug")
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("PDF generation failed: %v", err))
		return
	}

	if format == "report" {
		respondJSON(w, http.StatusOK, report)
		return
	}

	suffix := ""
	if format == "debug" {
		suffix = "-debug"
	}
	writePDFResponse(w, pdfData, book.Title+suffix, report)
}
