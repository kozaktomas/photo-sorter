package mcp

import (
	"context"
	"fmt"

	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerLabelTools registers label CRUD tools.
func (s *Server) registerLabelTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("list_labels",
			mcp.WithDescription("List labels from PhotoPrism"),
			mcp.WithNumber("count", mcp.Description("Number of labels to return (default 100, max 5000)")),
			mcp.WithNumber("offset", mcp.Description("Offset for pagination (default 0)")),
			mcp.WithBoolean("all", mcp.Description("Include all labels including those without photos (default false)")),
		),
		s.handleListLabels,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("get_label",
			mcp.WithDescription("Get label details by UID"),
			mcp.WithString("label_uid", mcp.Required(), mcp.Description("Label UID")),
		),
		s.handleGetLabel,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("update_label",
			mcp.WithDescription("Update a label (rename, change description, etc.)"),
			mcp.WithString("label_uid", mcp.Required(), mcp.Description("Label UID")),
			mcp.WithString("name", mcp.Description("New label name")),
			mcp.WithString("description", mcp.Description("New description")),
			mcp.WithString("notes", mcp.Description("New notes")),
			mcp.WithNumber("priority", mcp.Description("New priority")),
			mcp.WithBoolean("favorite", mcp.Description("Set favorite status")),
		),
		s.handleUpdateLabel,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("delete_labels",
			mcp.WithDescription("Delete labels by UIDs"),
			mcp.WithArray("label_uids", mcp.Required(), mcp.Description("Label UIDs to delete")),
		),
		s.handleDeleteLabels,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("add_photo_label",
			mcp.WithDescription("Add a label to a photo"),
			mcp.WithString("photo_uid", mcp.Required(), mcp.Description("Photo UID")),
			mcp.WithString("name", mcp.Required(), mcp.Description("Label name")),
			mcp.WithNumber("uncertainty", mcp.Description("Uncertainty 0-100 (default 0 = certain)")),
			mcp.WithNumber("priority", mcp.Description("Priority (default 0)")),
		),
		s.handleAddPhotoLabel,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("remove_photo_label",
			mcp.WithDescription("Remove a label from a photo"),
			mcp.WithString("photo_uid", mcp.Required(), mcp.Description("Photo UID")),
			mcp.WithString("label_id", mcp.Required(), mcp.Description("Label ID (numeric)")),
		),
		s.handleRemovePhotoLabel,
	)
}

// --- Label handlers ---

func (s *Server) handleListLabels(
	_ context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	count := clampInt(optionalInt(args, "count", 100), 5000)
	offset := optionalInt(args, "offset", 0)
	all := false
	if v, ok := optionalBool(args, "all"); ok {
		all = v
	}

	labels, err := s.pp.GetLabels(count, offset, all)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list labels: %v", err)), nil
	}

	type labelItem struct {
		UID        string `json:"uid"`
		Name       string `json:"name"`
		Slug       string `json:"slug"`
		PhotoCount int    `json:"photo_count"`
		Favorite   bool   `json:"favorite"`
		Priority   int    `json:"priority"`
		CreatedAt  string `json:"created_at"`
	}

	result := make([]labelItem, len(labels))
	for i, l := range labels {
		result[i] = labelItem{
			UID:        l.UID,
			Name:       l.Name,
			Slug:       l.Slug,
			PhotoCount: l.PhotoCount,
			Favorite:   l.Favorite,
			Priority:   l.Priority,
			CreatedAt:  l.CreatedAt,
		}
	}
	return jsonResult(result)
}

func (s *Server) handleGetLabel(
	_ context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	labelUID, err := requiredStr(args, "label_uid")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// PhotoPrism has no single-label GET endpoint, so fetch all and find by UID.
	labels, err := s.pp.GetLabels(5000, 0, true)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get labels: %v", err)), nil
	}

	for _, l := range labels {
		if l.UID == labelUID {
			result := map[string]any{
				"uid":         l.UID,
				"name":        l.Name,
				"slug":        l.Slug,
				"description": l.Description,
				"notes":       l.Notes,
				"photo_count": l.PhotoCount,
				"favorite":    l.Favorite,
				"priority":    l.Priority,
				"created_at":  l.CreatedAt,
			}
			return jsonResult(result)
		}
	}

	return mcp.NewToolResultError(fmt.Sprintf("label %s not found", labelUID)), nil
}

func (s *Server) handleUpdateLabel(
	_ context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	labelUID, err := requiredStr(args, "label_uid")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	update := photoprism.LabelUpdate{}
	if n := optionalStr(args, "name"); n != "" {
		update.Name = &n
	}
	if d, ok := args["description"]; ok {
		desc, _ := d.(string)
		update.Description = &desc
	}
	if n, ok := args["notes"]; ok {
		notes, _ := n.(string)
		update.Notes = &notes
	}
	if p, ok := optionalFloat(args, "priority"); ok {
		pInt := int(p)
		update.Priority = &pInt
	}
	if fav, ok := optionalBool(args, "favorite"); ok {
		update.Favorite = &fav
	}

	label, err := s.pp.UpdateLabel(labelUID, update)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to update label: %v", err)), nil
	}

	result := map[string]any{
		"uid":         label.UID,
		"name":        label.Name,
		"slug":        label.Slug,
		"description": label.Description,
		"photo_count": label.PhotoCount,
		"favorite":    label.Favorite,
		"priority":    label.Priority,
	}
	return jsonResult(result)
}

func (s *Server) handleDeleteLabels(
	_ context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	labelUIDs, err := requiredStrArray(args, "label_uids")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := s.pp.DeleteLabels(labelUIDs); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to delete labels: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"success": true,
		"deleted": len(labelUIDs),
	})
}

func (s *Server) handleAddPhotoLabel(
	_ context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	photoUID, err := requiredStr(args, "photo_uid")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	name, err := requiredStr(args, "name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	label := photoprism.PhotoLabel{
		Name:        name,
		Uncertainty: optionalInt(args, "uncertainty", 0),
		Priority:    optionalInt(args, "priority", 0),
	}

	photo, err := s.pp.AddPhotoLabel(photoUID, label)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to add label: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"success":   true,
		"photo_uid": photo.UID,
		"label":     name,
	})
}

func (s *Server) handleRemovePhotoLabel(
	_ context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	photoUID, err := requiredStr(args, "photo_uid")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	labelID, err := requiredStr(args, "label_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	_, err = s.pp.RemovePhotoLabel(photoUID, labelID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to remove label: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"success":   true,
		"photo_uid": photoUID,
		"label_id":  labelID,
	})
}
