package handlers

import (
	"encoding/json"
	"fmt"
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

// BooksHandler handles photo book endpoints
type BooksHandler struct {
	config         *config.Config
	sessionManager *middleware.SessionManager
}

// NewBooksHandler creates a new books handler
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
	Sections    []sectionResponse `json:"sections"`
	Pages       []pageResponse    `json:"pages"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
}

type sectionResponse struct {
	ID         string `json:"id"`
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

	sectionResps := make([]sectionResponse, len(sections))
	for i, s := range sections {
		sectionResps[i] = sectionResponse{
			ID:         s.ID,
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
			slots[j] = slotResponse{SlotIndex: p.Slots[j].SlotIndex, PhotoUID: p.Slots[j].PhotoUID, TextContent: p.Slots[j].TextContent, CropX: p.Slots[j].CropX, CropY: p.Slots[j].CropY, CropScale: p.Slots[j].CropScale}
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

	respondJSON(w, http.StatusOK, bookDetailResponse{
		ID:          book.ID,
		Title:       book.Title,
		Description: book.Description,
		Sections:    sectionResps,
		Pages:       pageResps,
		CreatedAt:   book.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   book.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

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

// --- Sections ---

func (h *BooksHandler) CreateSection(w http.ResponseWriter, r *http.Request) {
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
	section := &database.BookSection{BookID: bookID, Title: req.Title}
	if err := bw.CreateSection(r.Context(), section); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create section")
		return
	}
	respondJSON(w, http.StatusCreated, sectionResponse{
		ID:        section.ID,
		Title:     section.Title,
		SortOrder: section.SortOrder,
	})
}

func (h *BooksHandler) UpdateSection(w http.ResponseWriter, r *http.Request) {
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
	section := &database.BookSection{ID: id, Title: req.Title}
	if err := bw.UpdateSection(r.Context(), section); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update section")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"id": id})
}

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

// applyPageUpdates applies the request fields to the page, returning an error message if validation fails
func applyPageUpdates(page *database.BookPage, req updatePageRequest) string {
	if req.Format != nil {
		if database.PageFormatSlotCount(*req.Format) == 0 {
			return "invalid format"
		}
		page.Format = *req.Format
		// Clear split_position when switching to 1_fullscreen
		if *req.Format == "1_fullscreen" {
			page.SplitPosition = nil
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
		if *req.Style != "modern" && *req.Style != "archival" {
			return "style must be 'modern' or 'archival'"
		}
		page.Style = *req.Style
	}
	if req.SplitPosition != nil {
		raw := req.SplitPosition
		if string(raw) == "null" {
			page.SplitPosition = nil
		} else {
			var sp float64
			if err := json.Unmarshal(raw, &sp); err != nil {
				return "split_position must be a number or null"
			}
			if sp < 0.2 || sp > 0.8 {
				return "split_position must be between 0.2 and 0.8"
			}
			page.SplitPosition = &sp
		}
	}
	return ""
}

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

	// Fetch existing page to preserve fields not being updated
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

	// If format changed to fewer slots, clear excess slots
	if req.Format != nil {
		newSlotCount := database.PageFormatSlotCount(*req.Format)
		for i := newSlotCount; i < oldSlotCount; i++ {
			if err := bw.ClearSlot(r.Context(), id, i); err != nil {
				log.Printf("warning: failed to clear excess slot %d on page %s: %v", i, id, err)
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]string{"id": id})
}

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
	if req.CropX < 0 || req.CropX > 1 || req.CropY < 0 || req.CropY > 1 {
		respondError(w, http.StatusBadRequest, "crop_x and crop_y must be between 0.0 and 1.0")
		return
	}
	cropScale := 1.0
	if req.CropScale != nil {
		cropScale = *req.CropScale
	}
	if cropScale < 0.1 || cropScale > 1.0 {
		respondError(w, http.StatusBadRequest, "crop_scale must be between 0.1 and 1.0")
		return
	}
	if err := bw.UpdateSlotCrop(r.Context(), pageID, slotIndex, req.CropX, req.CropY, cropScale); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update slot crop")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

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

	// Check lualatex availability
	if _, err := exec.LookPath("lualatex"); err != nil {
		respondError(w, http.StatusServiceUnavailable, "lualatex is not installed on the server")
		return
	}

	// Diagnostic test PDF
	if r.URL.Query().Get("format") == "test" {
		testPDF, err := latex.GenerateTestPDF(r.Context())
		if err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("test PDF generation failed: %v", err))
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-test.pdf"`, book.Title))
		w.Header().Set("Content-Length", strconv.Itoa(len(testPDF)))
		w.Write(testPDF)
		return
	}

	// Debug overlay PDF
	if r.URL.Query().Get("format") == "debug" {
		pdfData, report, err := latex.GeneratePDFWithOptions(r.Context(), pp, bw, id, true)
		if err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("PDF generation failed: %v", err))
			return
		}
		if report != nil && len(report.Warnings) > 0 {
			w.Header().Set("X-Export-Warnings", strconv.Itoa(len(report.Warnings)))
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-debug.pdf"`, book.Title))
		w.Header().Set("Content-Length", strconv.Itoa(len(pdfData)))
		w.Write(pdfData)
		return
	}

	pdfData, report, err := latex.GeneratePDF(r.Context(), pp, bw, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("PDF generation failed: %v", err))
		return
	}

	// Return JSON report instead of PDF if requested
	if r.URL.Query().Get("format") == "report" {
		respondJSON(w, http.StatusOK, report)
		return
	}

	if report != nil && len(report.Warnings) > 0 {
		w.Header().Set("X-Export-Warnings", strconv.Itoa(len(report.Warnings)))
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.pdf"`, book.Title))
	w.Header().Set("Content-Length", strconv.Itoa(len(pdfData)))
	w.Write(pdfData)
}
