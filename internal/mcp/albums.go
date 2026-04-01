package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerAlbumTools registers album CRUD tools.
func (s *Server) registerAlbumTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("list_albums",
			mcp.WithDescription("List albums from PhotoPrism"),
			mcp.WithNumber("count", mcp.Description("Number of albums to return (default 50, max 500)")),
			mcp.WithNumber("offset", mcp.Description("Offset for pagination (default 0)")),
			mcp.WithString("order", mcp.Description("Sort order (e.g. 'name', 'newest', 'oldest', 'favorites')")),
			mcp.WithString("query", mcp.Description("Search query to filter albums")),
			mcp.WithString("type", mcp.Description(
				"Album type: 'album' (manual), 'folder', 'moment', 'month', 'state', or empty for all")),
		),
		s.handleListAlbums,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("get_album",
			mcp.WithDescription("Get album details by UID"),
			mcp.WithString("album_uid", mcp.Required(), mcp.Description("Album UID")),
		),
		s.handleGetAlbum,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("create_album",
			mcp.WithDescription("Create a new album"),
			mcp.WithString("title", mcp.Required(), mcp.Description("Album title")),
		),
		s.handleCreateAlbum,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("get_album_photos",
			mcp.WithDescription("Get photos in an album"),
			mcp.WithString("album_uid", mcp.Required(), mcp.Description("Album UID")),
			mcp.WithNumber("count", mcp.Description("Number of photos to return (default 50, max 500)")),
			mcp.WithNumber("offset", mcp.Description("Offset for pagination (default 0)")),
		),
		s.handleGetAlbumPhotos,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("add_photos_to_album",
			mcp.WithDescription("Add photos to an album"),
			mcp.WithString("album_uid", mcp.Required(), mcp.Description("Album UID")),
			mcp.WithArray("photo_uids", mcp.Required(), mcp.Description("Photo UIDs to add")),
		),
		s.handleAddPhotosToAlbum,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("remove_photos_from_album",
			mcp.WithDescription("Remove photos from an album (keeps them in library)"),
			mcp.WithString("album_uid", mcp.Required(), mcp.Description("Album UID")),
			mcp.WithArray("photo_uids", mcp.Required(), mcp.Description("Photo UIDs to remove")),
		),
		s.handleRemovePhotosFromAlbum,
	)
}

// --- Album handlers ---

func (s *Server) handleListAlbums(
	_ context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	count := clampInt(optionalInt(args, "count", 50), 1, 500)
	offset := optionalInt(args, "offset", 0)
	order := optionalStr(args, "order")
	query := optionalStr(args, "query")
	albumType := optionalStr(args, "type")

	albums, err := s.pp.GetAlbums(count, offset, order, query, albumType)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list albums: %v", err)), nil
	}

	type albumItem struct {
		UID        string `json:"uid"`
		Title      string `json:"title"`
		Type       string `json:"type"`
		PhotoCount int    `json:"photo_count"`
		Favorite   bool   `json:"favorite"`
		CreatedAt  string `json:"created_at"`
	}

	result := make([]albumItem, len(albums))
	for i, a := range albums {
		result[i] = albumItem{
			UID:        a.UID,
			Title:      a.Title,
			Type:       a.Type,
			PhotoCount: a.PhotoCount,
			Favorite:   a.Favorite,
			CreatedAt:  a.CreatedAt,
		}
	}
	return jsonResult(result)
}

func (s *Server) handleGetAlbum(
	_ context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	albumUID, err := requiredStr(args, "album_uid")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	album, err := s.pp.GetAlbum(albumUID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get album: %v", err)), nil
	}

	result := map[string]any{
		"uid":         album.UID,
		"title":       album.Title,
		"type":        album.Type,
		"description": album.Description,
		"favorite":    album.Favorite,
		"photo_count": album.PhotoCount,
		"created_at":  album.CreatedAt,
		"updated_at":  album.UpdatedAt,
	}
	return jsonResult(result)
}

func (s *Server) handleCreateAlbum(
	_ context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	title, err := requiredStr(args, "title")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	album, err := s.pp.CreateAlbum(title)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create album: %v", err)), nil
	}

	result := map[string]any{
		"uid":        album.UID,
		"title":      album.Title,
		"type":       album.Type,
		"created_at": album.CreatedAt,
	}
	return jsonResult(result)
}

func (s *Server) handleGetAlbumPhotos(
	_ context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	albumUID, err := requiredStr(args, "album_uid")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	count := clampInt(optionalInt(args, "count", 50), 1, 500)
	offset := optionalInt(args, "offset", 0)

	photos, err := s.pp.GetAlbumPhotos(albumUID, count, offset)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get album photos: %v", err)), nil
	}

	type photoItem struct {
		UID         string `json:"uid"`
		Title       string `json:"title"`
		Description string `json:"description,omitempty"`
		TakenAt     string `json:"taken_at"`
		Type        string `json:"type"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		Favorite    bool   `json:"favorite"`
	}

	result := make([]photoItem, len(photos))
	for i, p := range photos {
		result[i] = photoItem{
			UID:         p.UID,
			Title:       p.Title,
			Description: p.Description,
			TakenAt:     p.TakenAt,
			Type:        p.Type,
			Width:       p.Width,
			Height:      p.Height,
			Favorite:    p.Favorite,
		}
	}
	return jsonResult(map[string]any{
		"album_uid": albumUID,
		"photos":    result,
		"count":     len(result),
	})
}

func (s *Server) handleAddPhotosToAlbum(
	_ context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	albumUID, err := requiredStr(args, "album_uid")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	photoUIDs, err := requiredStrArray(args, "photo_uids")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := s.pp.AddPhotosToAlbum(albumUID, photoUIDs); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to add photos to album: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"success":    true,
		"album_uid":  albumUID,
		"added":      len(photoUIDs),
		"photo_uids": photoUIDs,
	})
}

func (s *Server) handleRemovePhotosFromAlbum(
	_ context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	albumUID, err := requiredStr(args, "album_uid")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	photoUIDs, err := requiredStrArray(args, "photo_uids")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := s.pp.RemovePhotosFromAlbum(albumUID, photoUIDs); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to remove photos from album: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"success":    true,
		"album_uid":  albumUID,
		"removed":    len(photoUIDs),
		"photo_uids": photoUIDs,
	})
}
