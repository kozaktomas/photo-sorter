package mcp

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/mark3labs/mcp-go/mcp"
)

// --- helpers ---

func requiredInt(args map[string]any, key string) (int, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return 0, fmt.Errorf("missing required parameter: %s", key)
	}
	f, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("parameter %s must be a number", key)
	}
	return int(f), nil
}

func requiredFloat(args map[string]any, key string) (float64, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return 0, fmt.Errorf("missing required parameter: %s", key)
	}
	f, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("parameter %s must be a number", key)
	}
	return f, nil
}

func optionalFloat(args map[string]any, key string) (float64, bool) {
	v, ok := args[key]
	if !ok || v == nil {
		return 0, false
	}
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	return f, true
}

// parsePageIDAndSlot extracts and validates page_id and slot_index from args.
func (s *Server) parsePageIDAndSlot(
	args map[string]any,
) (string, int, error) {
	pageID, err := requiredStr(args, "page_id")
	if err != nil {
		return "", 0, err
	}
	slotIndex, err := requiredInt(args, "slot_index")
	if err != nil {
		return "", 0, err
	}
	if _, err := s.validateSlotIndex(pageID, slotIndex); err != nil {
		return "", 0, err
	}
	return pageID, slotIndex, nil
}

const (
	createPageDesc = "Create a page in a book. " +
		"Format determines slot count: " +
		"4_landscape=4, 2l_1p=3, 1p_2l=3, 2_portrait=2, 1_fullscreen=1"
	formatDesc = "Page format: 4_landscape (4 slots), " +
		"2l_1p (3 slots), 1p_2l (3 slots), " +
		"2_portrait (2 slots), 1_fullscreen (1 slot)"
	invalidFormatMsg = "invalid format %q — valid: " +
		"4_landscape, 2l_1p, 1p_2l, 2_portrait, 1_fullscreen"
)

// registerPageTools registers page CRUD + reorder tools.
func (s *Server) registerPageTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("create_page",
			mcp.WithDescription(createPageDesc),
			mcp.WithString("book_id", mcp.Required(),
				mcp.Description("Book ID (UUID)")),
			mcp.WithString("section_id", mcp.Required(),
				mcp.Description("Section ID (UUID) this page belongs to")),
			mcp.WithString("format", mcp.Required(),
				mcp.Description(formatDesc)),
		),
		s.handleCreatePage,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("update_page",
			mcp.WithDescription(
				"Update page format, section, description, or split position"),
			mcp.WithString("page_id", mcp.Required(),
				mcp.Description("Page ID (UUID)")),
			mcp.WithString("format",
				mcp.Description("New format: 4_landscape, 2l_1p, "+
					"1p_2l, 2_portrait, 1_fullscreen")),
			mcp.WithString("section_id",
				mcp.Description("New section ID (UUID)")),
			mcp.WithString("description",
				mcp.Description("Page description")),
			mcp.WithNumber("split_position",
				mcp.Description("Column split ratio (0.2-0.8), "+
					"only for 2l_1p/1p_2l formats")),
		),
		s.handleUpdatePage,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("delete_page",
			mcp.WithDescription("Delete a page and all its slots"),
			mcp.WithString("page_id", mcp.Required(),
				mcp.Description("Page ID (UUID)")),
		),
		s.handleDeletePage,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("reorder_pages",
			mcp.WithDescription("Reorder pages in a book"),
			mcp.WithString("book_id", mcp.Required(),
				mcp.Description("Book ID (UUID)")),
			mcp.WithArray("page_ids", mcp.Required(),
				mcp.Description("Page IDs (UUIDs) in new order")),
		),
		s.handleReorderPages,
	)
}

// registerSlotTools registers slot assignment and management tools.
func (s *Server) registerSlotTools() {
	s.registerSlotAssignTools()
	s.registerSlotManageTools()
}

func (s *Server) registerSlotAssignTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("assign_photo_to_slot",
			mcp.WithDescription("Assign a photo to a page slot"),
			mcp.WithString("page_id", mcp.Required(),
				mcp.Description("Page ID (UUID)")),
			mcp.WithNumber("slot_index", mcp.Required(),
				mcp.Description("0-based slot index")),
			mcp.WithString("photo_uid", mcp.Required(),
				mcp.Description("Photo UID to assign")),
		),
		s.handleAssignPhotoToSlot,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("assign_text_to_slot",
			mcp.WithDescription("Assign markdown text to a page slot"),
			mcp.WithString("page_id", mcp.Required(),
				mcp.Description("Page ID (UUID)")),
			mcp.WithNumber("slot_index", mcp.Required(),
				mcp.Description("0-based slot index")),
			mcp.WithString("text_content", mcp.Required(),
				mcp.Description("Markdown text content")),
		),
		s.handleAssignTextToSlot,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("clear_slot",
			mcp.WithDescription("Clear a page slot (remove photo or text)"),
			mcp.WithString("page_id", mcp.Required(),
				mcp.Description("Page ID (UUID)")),
			mcp.WithNumber("slot_index", mcp.Required(),
				mcp.Description("0-based slot index to clear")),
		),
		s.handleClearSlot,
	)
}

func (s *Server) registerSlotManageTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("swap_slots",
			mcp.WithDescription("Swap two slots on a page"),
			mcp.WithString("page_id", mcp.Required(),
				mcp.Description("Page ID (UUID)")),
			mcp.WithNumber("slot_a", mcp.Required(),
				mcp.Description("First slot index (0-based)")),
			mcp.WithNumber("slot_b", mcp.Required(),
				mcp.Description("Second slot index (0-based)")),
		),
		s.handleSwapSlots,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("update_slot_crop",
			mcp.WithDescription("Update crop position and zoom for a slot"),
			mcp.WithString("page_id", mcp.Required(),
				mcp.Description("Page ID (UUID)")),
			mcp.WithNumber("slot_index", mcp.Required(),
				mcp.Description("0-based slot index")),
			mcp.WithNumber("crop_x", mcp.Required(),
				mcp.Description("Horizontal crop (0.0-1.0)")),
			mcp.WithNumber("crop_y", mcp.Required(),
				mcp.Description("Vertical crop (0.0-1.0)")),
			mcp.WithNumber("crop_scale",
				mcp.Description("Zoom (0.1-1.0, default 1.0)")),
		),
		s.handleUpdateSlotCrop,
	)
}

// --- Page handlers ---

func (s *Server) handleCreatePage(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	bookID, err := requiredStr(args, "book_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	sectionID, err := requiredStr(args, "section_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	format, err := requiredStr(args, "format")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if database.PageFormatSlotCount(format) == 0 {
		return mcp.NewToolResultError(
			fmt.Sprintf(invalidFormatMsg, format)), nil
	}

	page := &database.BookPage{
		BookID:    bookID,
		SectionID: sectionID,
		Format:    format,
	}
	if err := s.bookWriter.CreatePage(s.ctx(), page); err != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("failed to create page: %v", err)), nil
	}

	created, err := s.bookWriter.GetPage(s.ctx(), page.ID)
	if err != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("created but failed to fetch: %v", err)), nil
	}

	return jsonResult(convertPages([]database.BookPage{*created})[0])
}

// applyPageUpdates applies optional update fields to a page.
// Returns an error message if validation fails.
func applyPageUpdates(
	page *database.BookPage, args map[string]any,
) string {
	if f := optionalStr(args, "format"); f != "" {
		if database.PageFormatSlotCount(f) == 0 {
			return fmt.Sprintf(invalidFormatMsg, f)
		}
		page.Format = f
	}
	if sid := optionalStr(args, "section_id"); sid != "" {
		page.SectionID = sid
	}
	if d, ok := args["description"]; ok {
		page.Description, _ = d.(string)
	}
	if sp, ok := optionalFloat(args, "split_position"); ok {
		if sp < 0.2 || sp > 0.8 {
			return "split_position must be between 0.2 and 0.8"
		}
		if page.Format != "2l_1p" && page.Format != "1p_2l" {
			return "split_position only applies to 2l_1p/1p_2l"
		}
		page.SplitPosition = &sp
	}
	return ""
}

func (s *Server) handleUpdatePage(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	pageID, err := requiredStr(args, "page_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	page, err := s.bookWriter.GetPage(s.ctx(), pageID)
	if err != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("failed to get page: %v", err)), nil
	}
	if page == nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("page %s not found", pageID)), nil
	}

	oldSlotCount := database.PageFormatSlotCount(page.Format)

	if errMsg := applyPageUpdates(page, args); errMsg != "" {
		return mcp.NewToolResultError(errMsg), nil
	}

	if err := s.bookWriter.UpdatePage(s.ctx(), page); err != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("failed to update page: %v", err)), nil
	}

	// Clear excess slots if format changed to fewer slots.
	newSlotCount := database.PageFormatSlotCount(page.Format)
	for i := newSlotCount; i < oldSlotCount; i++ {
		_ = s.bookWriter.ClearSlot(s.ctx(), pageID, i)
	}

	updated, err := s.bookWriter.GetPage(s.ctx(), pageID)
	if err != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("updated but failed to fetch: %v", err)), nil
	}

	return jsonResult(convertPages([]database.BookPage{*updated})[0])
}

func (s *Server) handleDeletePage(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	pageID, err := requiredStr(args, "page_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := s.bookWriter.DeletePage(s.ctx(), pageID); err != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("failed to delete page: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"success": true,
		"message": fmt.Sprintf("page %s deleted", pageID),
	})
}

func (s *Server) handleReorderPages(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	bookID, err := requiredStr(args, "book_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	ids, err := requiredStrArray(args, "page_ids")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := s.bookWriter.ReorderPages(s.ctx(), bookID, ids); err != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("failed to reorder pages: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"success": true,
		"message": "pages reordered",
	})
}

// --- Slot handlers ---

// validateSlotIndex checks that the slot index is valid for the page.
func (s *Server) validateSlotIndex(
	pageID string, slotIndex int,
) (*database.BookPage, error) {
	page, err := s.bookWriter.GetPage(s.ctx(), pageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get page: %w", err)
	}
	if page == nil {
		return nil, fmt.Errorf("page %s not found", pageID)
	}
	maxSlots := database.PageFormatSlotCount(page.Format)
	if slotIndex < 0 || slotIndex >= maxSlots {
		return nil, fmt.Errorf(
			"slot_index %d out of range — format %q has %d slots (0-%d)",
			slotIndex, page.Format, maxSlots, maxSlots-1)
	}
	return page, nil
}

func (s *Server) handleAssignPhotoToSlot(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	pageID, slotIndex, err := s.parsePageIDAndSlot(args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	photoUID, err := requiredStr(args, "photo_uid")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := s.bookWriter.AssignSlot(
		s.ctx(), pageID, slotIndex, photoUID,
	); err != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("failed to assign photo: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"page_id":    pageID,
		"slot_index": slotIndex,
		"photo_uid":  photoUID,
		"assigned":   true,
	})
}

func (s *Server) handleAssignTextToSlot(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	pageID, slotIndex, err := s.parsePageIDAndSlot(args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	textContent, err := requiredStr(args, "text_content")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := s.bookWriter.AssignTextSlot(
		s.ctx(), pageID, slotIndex, textContent,
	); err != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("failed to assign text: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"page_id":      pageID,
		"slot_index":   slotIndex,
		"text_content": textContent,
		"assigned":     true,
	})
}

func (s *Server) handleClearSlot(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	pageID, slotIndex, err := s.parsePageIDAndSlot(args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := s.bookWriter.ClearSlot(
		s.ctx(), pageID, slotIndex,
	); err != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("failed to clear slot: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"success": true,
		"message": fmt.Sprintf(
			"slot %d on page %s cleared", slotIndex, pageID),
	})
}

func (s *Server) handleSwapSlots(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	pageID, err := requiredStr(args, "page_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	slotA, err := requiredInt(args, "slot_a")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	slotB, err := requiredInt(args, "slot_b")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	page, err := s.validateSlotIndex(pageID, slotA)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	maxSlots := database.PageFormatSlotCount(page.Format)
	if slotB < 0 || slotB >= maxSlots {
		return mcp.NewToolResultError(fmt.Sprintf(
			"slot_b %d out of range — format %q has %d slots (0-%d)",
			slotB, page.Format, maxSlots, maxSlots-1)), nil
	}

	if err := s.bookWriter.SwapSlots(
		s.ctx(), pageID, slotA, slotB,
	); err != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("failed to swap slots: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"success": true,
		"message": fmt.Sprintf(
			"swapped slots %d and %d on page %s", slotA, slotB, pageID),
	})
}

// parseCropParams extracts and validates crop parameters from args.
func parseCropParams(args map[string]any) (
	cropX, cropY, cropScale float64, err error,
) {
	cropX, err = requiredFloat(args, "crop_x")
	if err != nil {
		return 0, 0, 0, err
	}
	cropY, err = requiredFloat(args, "crop_y")
	if err != nil {
		return 0, 0, 0, err
	}
	if cropX < 0 || cropX > 1 {
		return 0, 0, 0, errors.New("crop_x must be between 0.0 and 1.0")
	}
	if cropY < 0 || cropY > 1 {
		return 0, 0, 0, errors.New("crop_y must be between 0.0 and 1.0")
	}
	cropScale = 1.0
	if cs, ok := optionalFloat(args, "crop_scale"); ok {
		if cs < 0.1 || cs > 1.0 {
			return 0, 0, 0, errors.New(
				"crop_scale must be between 0.1 and 1.0")
		}
		cropScale = cs
	}
	return cropX, cropY, cropScale, nil
}

func (s *Server) handleUpdateSlotCrop(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	pageID, slotIndex, err := s.parsePageIDAndSlot(args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	cropX, cropY, cropScale, err := parseCropParams(args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := s.bookWriter.UpdateSlotCrop(
		s.ctx(), pageID, slotIndex, cropX, cropY, cropScale,
	); err != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("failed to update slot crop: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"page_id":    pageID,
		"slot_index": slotIndex,
		"crop_x":     cropX,
		"crop_y":     cropY,
		"crop_scale": math.Round(cropScale*100) / 100,
		"updated":    true,
	})
}
