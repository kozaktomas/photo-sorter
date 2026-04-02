package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os/exec"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/latex"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
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
	Color     string `json:"color"`
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
	Title       string `json:"title"`
	FileName    string `json:"file_name"`
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
	Title       string  `json:"title"`
	FileName    string  `json:"file_name"`
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

	resp := buildBookDetailResponse(book, chapters, sections, pages)

	// Enrich slot responses with photo names
	allPhotoUIDs := collectSlotPhotoUIDs(pages)
	if len(allPhotoUIDs) > 0 {
		pp := middleware.MustGetPhotoPrism(r.Context(), w)
		if pp == nil {
			return
		}
		enrichSlotPhotoNames(&resp, fetchPhotoNames(pp, allPhotoUIDs))
	}

	respondJSON(w, http.StatusOK, resp)
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
			Color:     c.Color,
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

// enrichSlotPhotoNames populates Title and FileName on slot responses from a photo name lookup.
func enrichSlotPhotoNames(resp *bookDetailResponse, names map[string]photoNameInfo) {
	for i := range resp.Pages {
		for j := range resp.Pages[i].Slots {
			uid := resp.Pages[i].Slots[j].PhotoUID
			if info, ok := names[uid]; ok {
				resp.Pages[i].Slots[j].Title = info.Title
				resp.Pages[i].Slots[j].FileName = info.FileName
			}
		}
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
		Color:     chapter.Color,
		SortOrder: chapter.SortOrder,
	})
}

// UpdateChapter handles PUT /api/v1/chapters/:id and updates a chapter's title and/or color.
// Supports partial updates: only fields present in the request body are modified.
func (h *BooksHandler) UpdateChapter(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Title *string `json:"title"`
		Color *string `json:"color"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	chapter, err := bw.GetChapter(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "chapter not found")
		return
	}
	if req.Title != nil {
		chapter.Title = *req.Title
	}
	if req.Color != nil {
		chapter.Color = *req.Color
	}
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

	// Fetch photo names from PhotoPrism
	var photoNames map[string]photoNameInfo
	if len(photos) > 0 {
		pp := middleware.MustGetPhotoPrism(r.Context(), w)
		if pp == nil {
			return
		}
		uids := make([]string, len(photos))
		for i, p := range photos {
			uids[i] = p.PhotoUID
		}
		photoNames = fetchPhotoNames(pp, uids)
	}

	result := make([]sectionPhotoResponse, len(photos))
	for i, p := range photos {
		resp := sectionPhotoResponse{
			PhotoUID:    p.PhotoUID,
			Description: p.Description,
			Note:        p.Note,
			AddedAt:     p.AddedAt.Format("2006-01-02T15:04:05Z"),
		}
		if info, ok := photoNames[p.PhotoUID]; ok {
			resp.Title = info.Title
			resp.FileName = info.FileName
		}
		result[i] = resp
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

	// Save previous values as versions before updating.
	saveTextVersionsForSectionPhoto(r, bw, sectionID, photoUID, req.Description, req.Note)

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
		// Save previous text content as a version before overwriting.
		saveTextVersionForPageSlot(r, bw, pageID, slotIndex, req.TextContent)

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

// --- Auto-Layout ---

type autoLayoutRequest struct {
	PreferFormats []string `json:"prefer_formats"`
	MaxPages      int      `json:"max_pages"`
}

type autoLayoutResponse struct {
	PagesCreated int            `json:"pages_created"`
	PhotosPlaced int            `json:"photos_placed"`
	Pages        []pageResponse `json:"pages"`
}

// pageSpec describes a page to be created by the auto-layout algorithm.
type pageSpec struct {
	format string
	photos []string
}

// computeAutoLayout runs the layout algorithm on classified landscape/portrait photo lists.
// Returns page specs describing which pages to create and which photos to assign to each.
func computeAutoLayout(landscapes, portraits []string, allowed map[string]bool, maxPages int) []pageSpec {
	var specs []pageSpec

	// Step 1: 4 landscapes → 4_landscape
	specs, landscapes = layoutQuadLandscapes(specs, landscapes, allowed)

	// Step 2: 2 landscapes + 1 portrait → alternate 2l_1p / 1p_2l
	specs, landscapes, portraits = layoutMixedPages(specs, landscapes, portraits, allowed)

	// Step 3: 2 portraits → 2_portrait
	specs, portraits = layoutPairedPortraits(specs, portraits, allowed)

	// Step 4+5: remaining singles
	specs = layoutRemaining(specs, landscapes, portraits, allowed)

	if maxPages > 0 && len(specs) > maxPages {
		specs = specs[:maxPages]
	}
	return specs
}

// layoutQuadLandscapes groups sets of 4 landscapes into 4_landscape pages.
func layoutQuadLandscapes(specs []pageSpec, landscapes []string, allowed map[string]bool) ([]pageSpec, []string) {
	if !allowed["4_landscape"] {
		return specs, landscapes
	}
	for len(landscapes) >= 4 {
		specs = append(specs, pageSpec{"4_landscape", landscapes[:4]})
		landscapes = landscapes[4:]
	}
	return specs, landscapes
}

// layoutPairedPortraits groups pairs of portraits into 2_portrait pages.
func layoutPairedPortraits(specs []pageSpec, portraits []string, allowed map[string]bool) ([]pageSpec, []string) {
	if !allowed["2_portrait"] {
		return specs, portraits
	}
	for len(portraits) >= 2 {
		specs = append(specs, pageSpec{"2_portrait", portraits[:2]})
		portraits = portraits[2:]
	}
	return specs, portraits
}

// layoutRemaining handles leftover landscapes and portraits as pairs or fullscreen singles.
func layoutRemaining(specs []pageSpec, landscapes, portraits []string, allowed map[string]bool) []pageSpec {
	for len(landscapes) > 0 {
		if len(portraits) > 0 && allowed["2_portrait"] {
			specs = append(specs, pageSpec{"2_portrait", []string{landscapes[0], portraits[0]}})
			landscapes = landscapes[1:]
			portraits = portraits[1:]
		} else if allowed["1_fullscreen"] {
			specs = append(specs, pageSpec{"1_fullscreen", []string{landscapes[0]}})
			landscapes = landscapes[1:]
		} else {
			break
		}
	}
	if allowed["1_fullscreen"] {
		for len(portraits) > 0 {
			specs = append(specs, pageSpec{"1_fullscreen", []string{portraits[0]}})
			portraits = portraits[1:]
		}
	}
	return specs
}

const (
	format2L1P = "2l_1p"
	format1P2L = "1p_2l"
)

// layoutMixedPages creates alternating 2l_1p / 1p_2l pages from landscapes and portraits.
func layoutMixedPages(
	specs []pageSpec, landscapes, portraits []string, allowed map[string]bool,
) ([]pageSpec, []string, []string) {
	alternate := true
	for len(landscapes) >= 2 && len(portraits) >= 1 {
		format := pickMixedFormat(alternate, allowed)
		if format == "" {
			break
		}
		if format == format2L1P {
			specs = append(specs, pageSpec{format, []string{landscapes[0], landscapes[1], portraits[0]}})
		} else {
			specs = append(specs, pageSpec{format, []string{portraits[0], landscapes[0], landscapes[1]}})
		}
		landscapes = landscapes[2:]
		portraits = portraits[1:]
		alternate = !alternate
	}
	return specs, landscapes, portraits
}

// pickMixedFormat selects 2l_1p or 1p_2l based on alternation and allowed formats.
func pickMixedFormat(prefer2l1p bool, allowed map[string]bool) string {
	if prefer2l1p && allowed[format2L1P] {
		return format2L1P
	}
	if !prefer2l1p && allowed[format1P2L] {
		return format1P2L
	}
	if allowed[format2L1P] {
		return format2L1P
	}
	if allowed[format1P2L] {
		return format1P2L
	}
	return ""
}

// parseAutoLayoutFormats validates and returns the allowed formats set.
func parseAutoLayoutFormats(preferFormats []string) (map[string]bool, string) {
	if len(preferFormats) == 0 {
		return map[string]bool{
			"4_landscape": true, format2L1P: true, format1P2L: true,
			"2_portrait": true, "1_fullscreen": true,
		}, ""
	}
	allowed := make(map[string]bool, len(preferFormats))
	for _, f := range preferFormats {
		if database.PageFormatSlotCount(f) == 0 {
			return nil, "invalid format: " + f
		}
		allowed[f] = true
	}
	return allowed, ""
}

// AutoLayout handles POST /api/v1/books/{id}/sections/{sectionId}/auto-layout
// and generates pages with optimal format choices based on photo orientations.
func (h *BooksHandler) AutoLayout(w http.ResponseWriter, r *http.Request) {
	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}

	bookID := chi.URLParam(r, "id")
	sectionID := chi.URLParam(r, "sectionId")

	var req autoLayoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}

	allowedFormats, errMsg := parseAutoLayoutFormats(req.PreferFormats)
	if errMsg != "" {
		respondError(w, http.StatusBadRequest, errMsg)
		return
	}

	// Get unassigned photo UIDs for this section.
	unassigned, err := getUnassignedPhotos(r, bw, bookID, sectionID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(unassigned) == 0 {
		respondJSON(w, http.StatusOK, autoLayoutResponse{Pages: []pageResponse{}})
		return
	}

	// Classify each photo as landscape or portrait.
	landscapes, portraits := classifyPhotos(pp, unassigned)

	// Run layout algorithm and create pages.
	specs := computeAutoLayout(landscapes, portraits, allowedFormats, req.MaxPages)
	result := createAutoLayoutPages(r, bw, bookID, sectionID, specs)
	respondJSON(w, http.StatusOK, result)
}

// getUnassignedPhotos returns photo UIDs in the section that are not yet assigned to page slots.
func getUnassignedPhotos(
	r *http.Request, bw database.BookWriter, bookID, sectionID string,
) ([]string, error) {
	sectionPhotos, err := bw.GetSectionPhotos(r.Context(), sectionID)
	if err != nil {
		return nil, errors.New("failed to get section photos")
	}
	pages, err := bw.GetPages(r.Context(), bookID)
	if err != nil {
		return nil, errors.New("failed to get pages")
	}

	assignedUIDs := make(map[string]bool)
	for i := range pages {
		if pages[i].SectionID != sectionID {
			continue
		}
		for _, slot := range pages[i].Slots {
			if slot.PhotoUID != "" {
				assignedUIDs[slot.PhotoUID] = true
			}
		}
	}

	var unassigned []string
	for _, sp := range sectionPhotos {
		if !assignedUIDs[sp.PhotoUID] {
			unassigned = append(unassigned, sp.PhotoUID)
		}
	}
	return unassigned, nil
}

// photoQuerier abstracts the PhotoPrism photo query method for classifyPhotos.
type photoQuerier interface {
	GetPhotosWithQuery(count int, offset int, query string, quality ...int) ([]photoprism.Photo, error)
}

// classifyPhotos queries PhotoPrism for each photo's dimensions and splits into landscape/portrait lists.
func classifyPhotos(pp photoQuerier, uids []string) (landscapes, portraits []string) {
	for _, uid := range uids {
		photos, err := pp.GetPhotosWithQuery(1, 0, "uid:"+uid)
		if err != nil || len(photos) == 0 {
			log.Printf("auto-layout: skipping photo %s: %v", sanitizeForLog(uid), err)
			continue
		}
		if photos[0].Width >= photos[0].Height {
			landscapes = append(landscapes, uid)
		} else {
			portraits = append(portraits, uid)
		}
	}
	return
}

// createAutoLayoutPages creates pages and assigns slots based on the layout specs.
func createAutoLayoutPages(
	r *http.Request, bw database.BookWriter, bookID, sectionID string, specs []pageSpec,
) autoLayoutResponse {
	var createdPages []pageResponse
	photosPlaced := 0
	for _, spec := range specs {
		page := &database.BookPage{BookID: bookID, SectionID: sectionID, Format: spec.format, Style: "modern"}
		if err := bw.CreatePage(r.Context(), page); err != nil {
			log.Printf("auto-layout: failed to create page: %v", err)
			continue
		}
		slots := make([]slotResponse, 0, len(spec.photos))
		for i, uid := range spec.photos {
			if err := bw.AssignSlot(r.Context(), page.ID, i, uid); err != nil {
				log.Printf("auto-layout: failed to assign slot %d on page %s: %v", i, sanitizeForLog(page.ID), err)
				continue
			}
			slots = append(slots, slotResponse{SlotIndex: i, PhotoUID: uid, CropX: 0.5, CropY: 0.5, CropScale: 1.0})
			photosPlaced++
		}
		createdPages = append(createdPages, pageResponse{
			ID:        page.ID,
			SectionID: page.SectionID,
			Format:    page.Format,
			Style:     page.Style,
			SortOrder: page.SortOrder,
			Slots:     slots,
		})
	}
	return autoLayoutResponse{
		PagesCreated: len(createdPages),
		PhotosPlaced: photosPlaced,
		Pages:        createdPages,
	}
}

// --- Preflight Check ---

type preflightIssue struct {
	Type       string `json:"type"`
	PageNumber int    `json:"page_number,omitempty"`
	Section    string `json:"section,omitempty"`
	SlotIndex  int    `json:"slot_index,omitempty"`
	PhotoUID   string `json:"photo_uid,omitempty"`
	DPI        int    `json:"dpi,omitempty"`
	Count      int    `json:"count,omitempty"`
}

type preflightSummary struct {
	TotalPages  int `json:"total_pages"`
	TotalPhotos int `json:"total_photos"`
	FilledSlots int `json:"filled_slots"`
	TotalSlots  int `json:"total_slots"`
}

type preflightResponse struct {
	OK       bool             `json:"ok"`
	Errors   []preflightIssue `json:"errors"`
	Warnings []preflightIssue `json:"warnings"`
	Info     []preflightIssue `json:"info"`
	Summary  preflightSummary `json:"summary"`
}

// preflightData holds the loaded book data needed for preflight checks.
type preflightData struct {
	sections    []database.BookSection
	pages       []database.BookPage
	sectionByID map[string]string
	photoDims   map[string][2]int
}

// preflightResult accumulates the results of preflight checks.
type preflightResult struct {
	warnings     []preflightIssue
	info         []preflightIssue
	totalSlots   int
	filledSlots  int
	uniquePhotos map[string]bool
}

// Preflight handles GET /api/v1/books/:id/preflight and validates a book before PDF export.
func (h *BooksHandler) Preflight(w http.ResponseWriter, r *http.Request) {
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

	data, errMsg := loadPreflightData(r, bw, pp, id)
	if errMsg != "" {
		respondError(w, http.StatusInternalServerError, errMsg)
		return
	}

	result := &preflightResult{uniquePhotos: make(map[string]bool)}
	checkPageSlots(data, result)
	checkSections(r, bw, data, result)
	checkMissingCaptions(r, bw, data, result)

	warnings := result.warnings
	info := result.info
	if warnings == nil {
		warnings = []preflightIssue{}
	}
	if info == nil {
		info = []preflightIssue{}
	}

	respondJSON(w, http.StatusOK, preflightResponse{
		OK:       len(warnings) == 0,
		Errors:   []preflightIssue{},
		Warnings: warnings,
		Info:     info,
		Summary: preflightSummary{
			TotalPages:  len(data.pages),
			TotalPhotos: len(result.uniquePhotos),
			FilledSlots: result.filledSlots,
			TotalSlots:  result.totalSlots,
		},
	})
}

// loadPreflightData loads all book data needed for preflight checks.
func loadPreflightData(
	r *http.Request, bw database.BookWriter, pp *photoprism.PhotoPrism, bookID string,
) (*preflightData, string) {
	sections, err := bw.GetSections(r.Context(), bookID)
	if err != nil {
		return nil, "failed to get sections"
	}
	pages, err := bw.GetPages(r.Context(), bookID)
	if err != nil {
		return nil, "failed to get pages"
	}

	sectionByID := make(map[string]string, len(sections))
	for _, s := range sections {
		sectionByID[s.ID] = s.Title
	}

	allPhotoUIDs := collectSlotPhotoUIDs(pages)
	var photoDims map[string][2]int
	if len(allPhotoUIDs) > 0 {
		photoDims = fetchPhotoDimensions(pp, allPhotoUIDs)
	} else {
		photoDims = make(map[string][2]int)
	}

	return &preflightData{
		sections:    sections,
		pages:       pages,
		sectionByID: sectionByID,
		photoDims:   photoDims,
	}, ""
}

// collectSlotPhotoUIDs returns deduplicated photo UIDs from all page slots.
func collectSlotPhotoUIDs(pages []database.BookPage) []string {
	seen := make(map[string]bool)
	var uids []string
	for i := range pages {
		for _, slot := range pages[i].Slots {
			if slot.PhotoUID != "" && !seen[slot.PhotoUID] {
				uids = append(uids, slot.PhotoUID)
				seen[slot.PhotoUID] = true
			}
		}
	}
	return uids
}

// checkPageSlots checks all pages for empty slots and low DPI photos.
func checkPageSlots(data *preflightData, result *preflightResult) {
	layoutConfig := latex.DefaultLayoutConfig()
	for pageIdx, page := range data.pages {
		checkSinglePage(page, pageIdx+1, data, layoutConfig, result)
	}
}

// checkSinglePage checks one page for empty slots and low DPI.
func checkSinglePage(
	page database.BookPage, pageNum int, data *preflightData,
	layoutConfig latex.LayoutConfig, result *preflightResult,
) {
	sectionTitle := data.sectionByID[page.SectionID]
	slotRects := latex.FormatSlotsGridWithSplit(page.Format, layoutConfig, page.SplitPosition)
	expectedSlots := database.PageFormatSlotCount(page.Format)
	result.totalSlots += expectedSlots

	filledByIndex := indexFilledSlots(page.Slots, result)

	for slotIdx := range expectedSlots {
		slot, filled := filledByIndex[slotIdx]
		if !filled {
			result.warnings = append(result.warnings, preflightIssue{
				Type: "empty_slot", PageNumber: pageNum, Section: sectionTitle, SlotIndex: slotIdx,
			})
			continue
		}
		if w := checkSlotDPI(slot, slotIdx, slotRects, data.photoDims, pageNum, sectionTitle); w != nil {
			result.warnings = append(result.warnings, *w)
		}
	}
}

// indexFilledSlots builds a lookup of non-empty slots and updates the result counters.
func indexFilledSlots(slots []database.PageSlot, result *preflightResult) map[int]database.PageSlot {
	m := make(map[int]database.PageSlot)
	for _, slot := range slots {
		if !slot.IsEmpty() {
			m[slot.SlotIndex] = slot
			result.filledSlots++
			if slot.PhotoUID != "" {
				result.uniquePhotos[slot.PhotoUID] = true
			}
		}
	}
	return m
}

// checkSlotDPI checks a single slot for low DPI and returns a warning if applicable.
func checkSlotDPI(
	slot database.PageSlot, slotIdx int, slotRects []latex.SlotRect,
	photoDims map[string][2]int, pageNum int, sectionTitle string,
) *preflightIssue {
	if slot.PhotoUID == "" || slotIdx >= len(slotRects) {
		return nil
	}
	dims, ok := photoDims[slot.PhotoUID]
	if !ok {
		return nil
	}
	rect := slotRects[slotIdx]
	dpi := computeEffectiveDPI(dims[0], dims[1], rect.W, rect.H)
	if dpi >= 200 {
		return nil
	}
	return &preflightIssue{
		Type: "low_dpi", PageNumber: pageNum, Section: sectionTitle,
		SlotIndex: slotIdx, PhotoUID: slot.PhotoUID, DPI: dpi,
	}
}

// buildAssignedPhotosIndex builds per-section assigned photo UID sets
// and tracks which sections have pages.
func buildAssignedPhotosIndex(
	pages []database.BookPage,
) (assignedBySection map[string]map[string]bool, pagesPerSection map[string]bool) {
	assignedBySection = make(map[string]map[string]bool)
	pagesPerSection = make(map[string]bool)
	for _, p := range pages {
		pagesPerSection[p.SectionID] = true
		if assignedBySection[p.SectionID] == nil {
			assignedBySection[p.SectionID] = make(map[string]bool)
		}
		for _, slot := range p.Slots {
			if slot.PhotoUID != "" {
				assignedBySection[p.SectionID][slot.PhotoUID] = true
			}
		}
	}
	return
}

// checkSections checks for empty sections and unplaced photos.
func checkSections(
	r *http.Request, bw database.BookWriter, data *preflightData, result *preflightResult,
) {
	assignedBySection, pagesPerSection := buildAssignedPhotosIndex(data.pages)

	for _, section := range data.sections {
		if !pagesPerSection[section.ID] {
			result.warnings = append(result.warnings, preflightIssue{
				Type: "empty_section", Section: section.Title,
			})
		}

		sectionPhotos, err := bw.GetSectionPhotos(r.Context(), section.ID)
		if err != nil {
			continue
		}
		unplaced := countUnplaced(sectionPhotos, assignedBySection[section.ID])
		if unplaced > 0 {
			result.info = append(result.info, preflightIssue{
				Type: "unplaced_photos", Section: section.Title, Count: unplaced,
			})
		}
	}
}

// countUnplaced counts section photos not present in the assigned set.
func countUnplaced(sectionPhotos []database.SectionPhoto, assigned map[string]bool) int {
	count := 0
	for _, sp := range sectionPhotos {
		if !assigned[sp.PhotoUID] {
			count++
		}
	}
	return count
}

// sectionPhotoKey identifies a photo within a section.
type sectionPhotoKey struct{ sectionID, photoUID string }

// buildDescribedPhotos returns a set of section-photo pairs that have descriptions.
func buildDescribedPhotos(
	r *http.Request, bw database.BookWriter, sections []database.BookSection,
) map[sectionPhotoKey]bool {
	described := make(map[sectionPhotoKey]bool)
	for _, section := range sections {
		sectionPhotos, err := bw.GetSectionPhotos(r.Context(), section.ID)
		if err != nil {
			continue
		}
		for _, sp := range sectionPhotos {
			if sp.Description != "" {
				described[sectionPhotoKey{section.ID, sp.PhotoUID}] = true
			}
		}
	}
	return described
}

// checkMissingCaptions counts photo slots without descriptions and appends to result.
func checkMissingCaptions(
	r *http.Request, bw database.BookWriter, data *preflightData, result *preflightResult,
) {
	described := buildDescribedPhotos(r, bw, data.sections)
	count := 0
	for _, page := range data.pages {
		for _, slot := range page.Slots {
			if slot.PhotoUID != "" && !described[sectionPhotoKey{page.SectionID, slot.PhotoUID}] {
				count++
			}
		}
	}
	if count > 0 {
		result.info = append(result.info, preflightIssue{
			Type: "missing_captions", Count: count,
		})
	}
}

// fetchPhotoDimensions batch-fetches photo dimensions from PhotoPrism.
func fetchPhotoDimensions(pp *photoprism.PhotoPrism, uids []string) map[string][2]int {
	dims := make(map[string][2]int, len(uids))
	const batchSize = 100
	for i := 0; i < len(uids); i += batchSize {
		end := min(i+batchSize, len(uids))
		batch := uids[i:end]
		query := "uid:" + strings.Join(batch, "|uid:")
		photos, err := pp.GetPhotosWithQuery(len(batch), 0, query, 0)
		if err != nil {
			log.Printf("preflight: failed to fetch photo dimensions: %v", err)
			continue
		}
		for _, p := range photos {
			dims[p.UID] = [2]int{p.Width, p.Height}
		}
	}
	return dims
}

// photoNameInfo holds title and filename for a photo.
type photoNameInfo struct {
	Title    string
	FileName string
}

// fetchPhotoNames batch-fetches photo title and filename from PhotoPrism.
func fetchPhotoNames(pp *photoprism.PhotoPrism, uids []string) map[string]photoNameInfo {
	names := make(map[string]photoNameInfo, len(uids))
	const batchSize = 100
	for i := 0; i < len(uids); i += batchSize {
		end := min(i+batchSize, len(uids))
		batch := uids[i:end]
		query := "uid:" + strings.Join(batch, "|uid:")
		photos, err := pp.GetPhotosWithQuery(len(batch), 0, query, 0)
		if err != nil {
			log.Printf("fetchPhotoNames: failed to fetch photos: %v", err)
			continue
		}
		for _, p := range photos {
			names[p.UID] = photoNameInfo{Title: p.Title, FileName: p.FileName}
		}
	}
	return names
}

// computeEffectiveDPI calculates the effective DPI for a photo at a given slot size.
func computeEffectiveDPI(photoW, photoH int, slotWmm, slotHmm float64) int {
	if slotWmm <= 0 || slotHmm <= 0 {
		return 0
	}
	dpiW := float64(photoW) / slotWmm * 25.4
	dpiH := float64(photoH) / slotHmm * 25.4
	return int(math.Min(dpiW, dpiH))
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

// resolveChapterColor follows section -> chapter -> color chain to find the chapter color.
func resolveChapterColor(ctx context.Context, bw database.BookWriter, sectionID string) string {
	if sectionID == "" {
		return ""
	}
	section, err := bw.GetSection(ctx, sectionID)
	if err != nil || section == nil || section.ChapterID == "" {
		return ""
	}
	chapter, err := bw.GetChapter(ctx, section.ChapterID)
	if err != nil || chapter == nil || chapter.Color == "" {
		return ""
	}
	return strings.TrimPrefix(chapter.Color, "#")
}

// buildSectionCaptions builds a CaptionMap for a single section's photo descriptions.
func buildSectionCaptions(ctx context.Context, bw database.BookWriter, sectionID string) latex.CaptionMap {
	captions := make(latex.CaptionMap)
	if sectionID == "" {
		return captions
	}
	sectionPhotos, err := bw.GetSectionPhotos(ctx, sectionID)
	if err != nil {
		return captions
	}
	m := make(map[string]string, len(sectionPhotos))
	for _, p := range sectionPhotos {
		if p.Description != "" {
			m[p.PhotoUID] = p.Description
		}
	}
	if len(m) > 0 {
		captions[sectionID] = m
	}
	return captions
}

// ExportPagePDF handles GET /api/v1/pages/:id/export-pdf and exports a single page as PDF.
func (h *BooksHandler) ExportPagePDF(w http.ResponseWriter, r *http.Request) {
	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}

	pageID := chi.URLParam(r, "id")
	page, err := bw.GetPage(r.Context(), pageID)
	if err != nil || page == nil {
		respondError(w, http.StatusNotFound, "page not found")
		return
	}

	if _, err := exec.LookPath("lualatex"); err != nil {
		respondError(w, http.StatusServiceUnavailable, "lualatex is not installed on the server")
		return
	}

	book, err := bw.GetBook(r.Context(), page.BookID)
	if err != nil || book == nil {
		respondError(w, http.StatusNotFound, "book not found")
		return
	}

	pdfData, err := latex.GenerateSinglePagePDF(r.Context(), pp, latex.SinglePageInput{
		Page:         *page,
		BookTitle:    book.Title,
		ChapterColor: resolveChapterColor(r.Context(), bw, page.SectionID),
		Captions:     buildSectionCaptions(r.Context(), bw, page.SectionID),
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("PDF generation failed: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Content-Length", strconv.Itoa(len(pdfData)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	io.Copy(w, bytes.NewReader(pdfData))
}

// saveTextVersionsForSectionPhoto saves previous description/note as versions if they changed.
func saveTextVersionsForSectionPhoto(
	r *http.Request, bw database.BookWriter,
	sectionID, photoUID, newDesc, newNote string,
) {
	store, err := database.GetTextVersionStore(r.Context())
	if err != nil {
		return
	}
	photos, err := bw.GetSectionPhotos(r.Context(), sectionID)
	if err != nil {
		return
	}
	sourceID := sectionID + ":" + photoUID
	for _, p := range photos {
		if p.PhotoUID == photoUID {
			if p.Description != newDesc && p.Description != "" {
				_ = store.SaveTextVersion(r.Context(), &database.TextVersion{
					SourceType: "section_photo",
					SourceID:   sourceID,
					Field:      "description",
					Content:    p.Description,
					ChangedBy:  "user",
				})
			}
			if p.Note != newNote && p.Note != "" {
				_ = store.SaveTextVersion(r.Context(), &database.TextVersion{
					SourceType: "section_photo",
					SourceID:   sourceID,
					Field:      "note",
					Content:    p.Note,
					ChangedBy:  "user",
				})
			}
			return
		}
	}
}

// saveTextVersionForPageSlot saves the previous text content as a version if it changed.
func saveTextVersionForPageSlot(
	r *http.Request, bw database.BookWriter,
	pageID string, slotIndex int, newText string,
) {
	store, err := database.GetTextVersionStore(r.Context())
	if err != nil {
		return
	}
	slots, err := bw.GetPageSlots(r.Context(), pageID)
	if err != nil {
		return
	}
	for _, s := range slots {
		if s.SlotIndex == slotIndex && s.TextContent != newText && s.TextContent != "" {
			sourceID := pageID + ":" + strconv.Itoa(slotIndex)
			_ = store.SaveTextVersion(r.Context(), &database.TextVersion{
				SourceType: "page_slot",
				SourceID:   sourceID,
				Field:      "text_content",
				Content:    s.TextContent,
				ChangedBy:  "user",
			})
			return
		}
	}
}
