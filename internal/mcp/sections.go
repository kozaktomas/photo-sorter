package mcp

import (
	"context"
	"fmt"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerSectionTools registers section CRUD and reorder tools.
func (s *Server) registerSectionTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("create_section",
			mcp.WithDescription("Create a section in a book"),
			mcp.WithString("book_id", mcp.Required(), mcp.Description("Book ID (UUID)")),
			mcp.WithString("title", mcp.Required(), mcp.Description("Section title")),
			mcp.WithString("chapter_id", mcp.Description("Chapter ID (UUID) to assign to")),
		),
		s.handleCreateSection,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("update_section",
			mcp.WithDescription("Update section title or chapter assignment"),
			mcp.WithString("section_id", mcp.Required(), mcp.Description("Section ID (UUID)")),
			mcp.WithString("title", mcp.Description("New title")),
			mcp.WithString("chapter_id",
				mcp.Description("Chapter ID (UUID), or empty string to unassign")),
		),
		s.handleUpdateSection,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("delete_section",
			mcp.WithDescription("Delete a section"),
			mcp.WithString("section_id", mcp.Required(), mcp.Description("Section ID (UUID)")),
		),
		s.handleDeleteSection,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("reorder_sections",
			mcp.WithDescription("Reorder sections in a book"),
			mcp.WithString("book_id", mcp.Required(), mcp.Description("Book ID (UUID)")),
			mcp.WithArray("section_ids", mcp.Required(),
				mcp.Description("Section IDs (UUIDs) in new order")),
		),
		s.handleReorderSections,
	)
}

// registerSectionPhotoTools registers photo management tools within sections.
func (s *Server) registerSectionPhotoTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("list_section_photos",
			mcp.WithDescription("List photos assigned to a section"),
			mcp.WithString("section_id", mcp.Required(), mcp.Description("Section ID (UUID)")),
		),
		s.handleListSectionPhotos,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("add_photos_to_section",
			mcp.WithDescription("Add photos to a section"),
			mcp.WithString("section_id", mcp.Required(), mcp.Description("Section ID (UUID)")),
			mcp.WithArray("photo_uids", mcp.Required(), mcp.Description("Photo UIDs to add")),
		),
		s.handleAddPhotosToSection,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("remove_photos_from_section",
			mcp.WithDescription("Remove photos from a section"),
			mcp.WithString("section_id", mcp.Required(), mcp.Description("Section ID (UUID)")),
			mcp.WithArray("photo_uids", mcp.Required(),
				mcp.Description("Photo UIDs to remove")),
		),
		s.handleRemoveSectionPhotos,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("update_section_photo",
			mcp.WithDescription("Update a photo's description or note within a section"),
			mcp.WithString("section_id", mcp.Required(), mcp.Description("Section ID (UUID)")),
			mcp.WithString("photo_uid", mcp.Required(), mcp.Description("Photo UID")),
			mcp.WithString("description", mcp.Description("New description")),
			mcp.WithString("note", mcp.Description("New note")),
		),
		s.handleUpdateSectionPhoto,
	)
}

// --- Section handlers ---

func (s *Server) handleCreateSection(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	bookID, err := requiredStr(args, "book_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	title, err := requiredStr(args, "title")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	section := &database.BookSection{
		BookID:    bookID,
		Title:     title,
		ChapterID: optionalStr(args, "chapter_id"),
	}
	if err := s.bookWriter.CreateSection(s.ctx(), section); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create section: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"id":         section.ID,
		"book_id":    section.BookID,
		"title":      section.Title,
		"chapter_id": section.ChapterID,
		"sort_order": section.SortOrder,
	})
}

func (s *Server) handleUpdateSection(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	sectionID, err := requiredStr(args, "section_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	existing, err := s.bookWriter.GetSection(s.ctx(), sectionID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get section: %v", err)), nil
	}
	if existing == nil {
		return mcp.NewToolResultError(fmt.Sprintf("section %s not found", sectionID)), nil
	}

	if t := optionalStr(args, "title"); t != "" {
		existing.Title = t
	}
	if _, ok := args["chapter_id"]; ok {
		existing.ChapterID, _ = args["chapter_id"].(string)
	}

	if err := s.bookWriter.UpdateSection(s.ctx(), existing); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to update section: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"id":         existing.ID,
		"title":      existing.Title,
		"chapter_id": existing.ChapterID,
		"sort_order": existing.SortOrder,
	})
}

func (s *Server) handleDeleteSection(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	sectionID, err := requiredStr(args, "section_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := s.bookWriter.DeleteSection(s.ctx(), sectionID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to delete section: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"success": true,
		"message": fmt.Sprintf("section %s deleted", sectionID),
	})
}

func (s *Server) handleReorderSections(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	bookID, err := requiredStr(args, "book_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	ids, err := requiredStrArray(args, "section_ids")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := s.bookWriter.ReorderSections(s.ctx(), bookID, ids); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to reorder sections: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"success": true,
		"message": "sections reordered",
	})
}

// --- Section photo handlers ---

func (s *Server) handleListSectionPhotos(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	sectionID, err := requiredStr(args, "section_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	photos, err := s.bookWriter.GetSectionPhotos(s.ctx(), sectionID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list section photos: %v", err)), nil
	}

	type photoItem struct {
		PhotoUID    string `json:"photo_uid"`
		Description string `json:"description,omitempty"`
		Note        string `json:"note,omitempty"`
		Position    int64  `json:"position"`
	}

	result := make([]photoItem, len(photos))
	for i, p := range photos {
		result[i] = photoItem{
			PhotoUID:    p.PhotoUID,
			Description: p.Description,
			Note:        p.Note,
			Position:    p.ID,
		}
	}
	return jsonResult(result)
}

func (s *Server) handleAddPhotosToSection(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	sectionID, err := requiredStr(args, "section_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	photoUIDs, err := requiredStrArray(args, "photo_uids")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := s.bookWriter.AddSectionPhotos(s.ctx(), sectionID, photoUIDs); err != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("failed to add photos to section: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"success": true,
		"message": fmt.Sprintf("added %d photos to section %s", len(photoUIDs), sectionID),
		"count":   len(photoUIDs),
	})
}

func (s *Server) handleRemoveSectionPhotos(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	sectionID, err := requiredStr(args, "section_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	photoUIDs, err := requiredStrArray(args, "photo_uids")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := s.bookWriter.RemoveSectionPhotos(s.ctx(), sectionID, photoUIDs); err != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("failed to remove photos from section: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"success": true,
		"message": fmt.Sprintf("removed %d photos from section %s", len(photoUIDs), sectionID),
	})
}

func (s *Server) handleUpdateSectionPhoto(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	sectionID, err := requiredStr(args, "section_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	photoUID, err := requiredStr(args, "photo_uid")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	description := optionalStr(args, "description")
	note := optionalStr(args, "note")

	if err := s.bookWriter.UpdateSectionPhoto(
		s.ctx(), sectionID, photoUID, description, note,
	); err != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("failed to update section photo: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"section_id":  sectionID,
		"photo_uid":   photoUID,
		"description": description,
		"note":        note,
	})
}
