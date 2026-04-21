package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/latex"
	"github.com/mark3labs/mcp-go/mcp"
)

// --- helpers ---

func requiredStr(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return "", fmt.Errorf("missing required parameter: %s", key)
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("parameter %s must be a non-empty string", key)
	}
	return s, nil
}

func optionalStr(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func requiredStrArray(args map[string]any, key string) ([]string, error) {
	raw, ok := args[key]
	if !ok {
		return nil, fmt.Errorf("missing required parameter: %s", key)
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array", key)
	}
	if len(arr) == 0 {
		return nil, fmt.Errorf("%s must not be empty", key)
	}
	result := make([]string, len(arr))
	for i, v := range arr {
		str, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a string", key, i)
		}
		result[i] = str
	}
	return result, nil
}

func jsonResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal response: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// --- Book handlers ---

func (s *Server) handleListBooks(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	books, err := s.bookWriter.ListBooks(s.ctx())
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list books: %v", err)), nil
	}

	type bookItem struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		CreatedAt   string `json:"created_at"`
	}

	result := make([]bookItem, len(books))
	for i, b := range books {
		result[i] = bookItem{
			ID:          b.ID,
			Title:       b.Title,
			Description: b.Description,
			CreatedAt:   b.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}
	return jsonResult(result)
}

// bookDetailResult holds the JSON response for get_book.
type bookDetailResult struct {
	ID          string              `json:"id"`
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Chapters    []chapterDetailItem `json:"chapters"`
	Sections    []sectionDetailItem `json:"sections"`
	Pages       []pageDetailItem    `json:"pages"`
	CreatedAt   string              `json:"created_at"`
	UpdatedAt   string              `json:"updated_at"`
}

type chapterDetailItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Color     string `json:"color,omitempty"`
	SortOrder int    `json:"sort_order"`
}

type sectionDetailItem struct {
	ID         string `json:"id"`
	ChapterID  string `json:"chapter_id,omitempty"`
	Title      string `json:"title"`
	SortOrder  int    `json:"sort_order"`
	PhotoCount int    `json:"photo_count"`
}

type pageDetailItem struct {
	ID             string           `json:"id"`
	SectionID      string           `json:"section_id,omitempty"`
	Format         string           `json:"format"`
	Style          string           `json:"style"`
	Description    string           `json:"description,omitempty"`
	SplitPosition  *float64         `json:"split_position,omitempty"`
	HidePageNumber bool             `json:"hide_page_number,omitempty"`
	SortOrder      int              `json:"sort_order"`
	Slots          []slotDetailItem `json:"slots"`
}

type slotDetailItem struct {
	SlotIndex      int     `json:"slot_index"`
	PhotoUID       string  `json:"photo_uid,omitempty"`
	TextContent    string  `json:"text_content,omitempty"`
	IsCaptionsSlot bool    `json:"is_captions_slot,omitempty"`
	CropX          float64 `json:"crop_x"`
	CropY          float64 `json:"crop_y"`
	CropScale      float64 `json:"crop_scale"`
}

func convertPages(pages []database.BookPage) []pageDetailItem {
	items := make([]pageDetailItem, len(pages))
	for i, p := range pages {
		slots := make([]slotDetailItem, len(p.Slots))
		for j, sl := range p.Slots {
			slots[j] = slotDetailItem{
				SlotIndex: sl.SlotIndex, PhotoUID: sl.PhotoUID,
				TextContent:    sl.TextContent,
				IsCaptionsSlot: sl.IsCaptionsSlot,
				CropX:          sl.CropX, CropY: sl.CropY, CropScale: sl.CropScale,
			}
		}
		items[i] = pageDetailItem{
			ID: p.ID, SectionID: p.SectionID, Format: p.Format,
			Style: p.Style, Description: p.Description,
			SplitPosition:  p.SplitPosition,
			HidePageNumber: p.HidePageNumber,
			SortOrder:      p.SortOrder,
			Slots:          slots,
		}
	}
	return items
}

func (s *Server) buildBookDetail(bookID string) (*bookDetailResult, error) {
	book, err := s.bookWriter.GetBook(s.ctx(), bookID)
	if err != nil {
		return nil, fmt.Errorf("failed to get book: %w", err)
	}
	if book == nil {
		return nil, fmt.Errorf("book %s not found", bookID)
	}

	chapters, err := s.bookWriter.GetChapters(s.ctx(), bookID)
	if err != nil {
		return nil, fmt.Errorf("failed to get chapters: %w", err)
	}

	sections, err := s.bookWriter.GetSections(s.ctx(), bookID)
	if err != nil {
		return nil, fmt.Errorf("failed to get sections: %w", err)
	}

	pages, err := s.bookWriter.GetPages(s.ctx(), bookID)
	if err != nil {
		return nil, fmt.Errorf("failed to get pages: %w", err)
	}

	chapterItems := make([]chapterDetailItem, len(chapters))
	for i, ch := range chapters {
		chapterItems[i] = chapterDetailItem{
			ID: ch.ID, Title: ch.Title, Color: ch.Color, SortOrder: ch.SortOrder,
		}
	}

	sectionItems := make([]sectionDetailItem, len(sections))
	for i, sec := range sections {
		count, _ := s.bookWriter.CountSectionPhotos(s.ctx(), sec.ID)
		sectionItems[i] = sectionDetailItem{
			ID: sec.ID, ChapterID: sec.ChapterID, Title: sec.Title,
			SortOrder: sec.SortOrder, PhotoCount: count,
		}
	}

	return &bookDetailResult{
		ID: book.ID, Title: book.Title, Description: book.Description,
		Chapters:  chapterItems,
		Sections:  sectionItems,
		Pages:     convertPages(pages),
		CreatedAt: book.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt: book.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}, nil
}

func (s *Server) handleGetBook(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	bookID, err := requiredStr(args, "book_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result, err := s.buildBookDetail(bookID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(result)
}

func (s *Server) handleCreateBook(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	title, err := requiredStr(args, "title")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	book := &database.PhotoBook{
		Title:       title,
		Description: optionalStr(args, "description"),
	}
	if err := s.bookWriter.CreateBook(s.ctx(), book); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create book: %v", err)), nil
	}

	result := struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		CreatedAt   string `json:"created_at"`
	}{
		ID:          book.ID,
		Title:       book.Title,
		Description: book.Description,
		CreatedAt:   book.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
	return jsonResult(result)
}

// applyBookTypography applies optional typography updates to a book. Returns
// an error message if validation fails, or empty string on success. Mirrors
// the validation ranges enforced by the web handler
// (internal/web/handlers/books.go applySizes/applyFonts) so the two paths
// stay in sync.
func applyBookTypography(
	book *database.PhotoBook, args map[string]any,
) string {
	if f := optionalStr(args, "body_font"); f != "" {
		if !latex.ValidateFont(f) {
			return fmt.Sprintf("invalid body_font: %q", f)
		}
		book.BodyFont = f
	}
	if f := optionalStr(args, "heading_font"); f != "" {
		if !latex.ValidateFont(f) {
			return fmt.Sprintf("invalid heading_font: %q", f)
		}
		book.HeadingFont = f
	}
	ranges := []struct {
		key    string
		lo, hi float64
		target *float64
	}{
		{"body_font_size", 6, 36, &book.BodyFontSize},
		{"body_line_height", 8, 48, &book.BodyLineHeight},
		{"h1_font_size", 6, 36, &book.H1FontSize},
		{"h2_font_size", 6, 36, &book.H2FontSize},
		{"caption_opacity", 0, 1, &book.CaptionOpacity},
		{"caption_font_size", 6, 36, &book.CaptionFontSize},
		{"heading_color_bleed", 0, 20, &book.HeadingColorBleed},
		{"caption_badge_size", 2, 12, &book.CaptionBadgeSize},
		{"body_text_pad_mm", 0, 10, &book.BodyTextPadMM},
	}
	for _, r := range ranges {
		v, ok := optionalFloat(args, r.key)
		if !ok {
			continue
		}
		if v < r.lo || v > r.hi {
			return fmt.Sprintf(
				"%s must be between %g and %g", r.key, r.lo, r.hi)
		}
		*r.target = v
	}
	return ""
}

func (s *Server) handleUpdateBook(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	bookID, err := requiredStr(args, "book_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	book, err := s.bookWriter.GetBook(s.ctx(), bookID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get book: %v", err)), nil
	}
	if book == nil {
		return mcp.NewToolResultError(fmt.Sprintf("book %s not found", bookID)), nil
	}

	if t := optionalStr(args, "title"); t != "" {
		book.Title = t
	}
	if d, ok := args["description"]; ok {
		book.Description, _ = d.(string)
	}
	if errMsg := applyBookTypography(book, args); errMsg != "" {
		return mcp.NewToolResultError(errMsg), nil
	}

	if err := s.bookWriter.UpdateBook(s.ctx(), book); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to update book: %v", err)), nil
	}
	return jsonResult(buildUpdateBookResponse(book))
}

// updateBookResponse mirrors the typography payload accepted by `update_book`
// so MCP clients see the resolved values after validation/clamping.
type updateBookResponse struct {
	ID                string  `json:"id"`
	Title             string  `json:"title"`
	Description       string  `json:"description"`
	BodyFont          string  `json:"body_font"`
	HeadingFont       string  `json:"heading_font"`
	BodyFontSize      float64 `json:"body_font_size"`
	BodyLineHeight    float64 `json:"body_line_height"`
	H1FontSize        float64 `json:"h1_font_size"`
	H2FontSize        float64 `json:"h2_font_size"`
	CaptionOpacity    float64 `json:"caption_opacity"`
	CaptionFontSize   float64 `json:"caption_font_size"`
	HeadingColorBleed float64 `json:"heading_color_bleed"`
	CaptionBadgeSize  float64 `json:"caption_badge_size"`
	BodyTextPadMM     float64 `json:"body_text_pad_mm"`
	UpdatedAt         string  `json:"updated_at"`
}

func buildUpdateBookResponse(book *database.PhotoBook) updateBookResponse {
	return updateBookResponse{
		ID:                book.ID,
		Title:             book.Title,
		Description:       book.Description,
		BodyFont:          book.BodyFont,
		HeadingFont:       book.HeadingFont,
		BodyFontSize:      book.BodyFontSize,
		BodyLineHeight:    book.BodyLineHeight,
		H1FontSize:        book.H1FontSize,
		H2FontSize:        book.H2FontSize,
		CaptionOpacity:    book.CaptionOpacity,
		CaptionFontSize:   book.CaptionFontSize,
		HeadingColorBleed: book.HeadingColorBleed,
		CaptionBadgeSize:  book.CaptionBadgeSize,
		BodyTextPadMM:     book.BodyTextPadMM,
		UpdatedAt:         book.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

func (s *Server) handleDeleteBook(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	bookID, err := requiredStr(args, "book_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := s.bookWriter.DeleteBook(s.ctx(), bookID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to delete book: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"success": true,
		"message": fmt.Sprintf("book %s deleted", bookID),
	})
}

// --- Chapter handlers ---

func (s *Server) handleCreateChapter(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	bookID, err := requiredStr(args, "book_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	title, err := requiredStr(args, "title")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	chapter := &database.BookChapter{
		BookID: bookID,
		Title:  title,
		Color:  optionalStr(args, "color"),
	}
	if err := s.bookWriter.CreateChapter(s.ctx(), chapter); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create chapter: %v", err)), nil
	}

	result := struct {
		ID        string `json:"id"`
		BookID    string `json:"book_id"`
		Title     string `json:"title"`
		Color     string `json:"color,omitempty"`
		SortOrder int    `json:"sort_order"`
	}{
		ID:        chapter.ID,
		BookID:    chapter.BookID,
		Title:     chapter.Title,
		Color:     chapter.Color,
		SortOrder: chapter.SortOrder,
	}
	return jsonResult(result)
}

func (s *Server) handleUpdateChapter(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	chapterID, err := requiredStr(args, "chapter_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	chapter, err := s.bookWriter.GetChapter(s.ctx(), chapterID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("get chapter %s: %v", chapterID, err)), nil
	}
	if chapter == nil {
		return mcp.NewToolResultError("chapter not found: " + chapterID), nil
	}

	if t := optionalStr(args, "title"); t != "" {
		chapter.Title = t
	}
	if c := optionalStr(args, "color"); c != "" {
		chapter.Color = c
	}
	if v, ok := args["hide_from_toc"].(bool); ok {
		chapter.HideFromTOC = v
	}

	if err := s.bookWriter.UpdateChapter(s.ctx(), chapter); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to update chapter: %v", err)), nil
	}

	result := struct {
		ID          string `json:"id"`
		Title       string `json:"title,omitempty"`
		Color       string `json:"color,omitempty"`
		HideFromTOC bool   `json:"hide_from_toc"`
	}{
		ID:          chapter.ID,
		Title:       chapter.Title,
		Color:       chapter.Color,
		HideFromTOC: chapter.HideFromTOC,
	}
	return jsonResult(result)
}

func (s *Server) handleDeleteChapter(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	chapterID, err := requiredStr(args, "chapter_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := s.bookWriter.DeleteChapter(s.ctx(), chapterID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to delete chapter: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"success": true,
		"message": fmt.Sprintf("chapter %s deleted", chapterID),
	})
}

func (s *Server) handleReorderChapters(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	bookID, err := requiredStr(args, "book_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	ids, err := requiredStrArray(args, "chapter_ids")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := s.bookWriter.ReorderChapters(s.ctx(), bookID, ids); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to reorder chapters: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"success": true,
		"message": "chapters reordered",
	})
}
