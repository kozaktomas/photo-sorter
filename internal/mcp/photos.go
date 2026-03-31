package mcp

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerPhotoTools registers read-only photo tools for the book workflow.
func (s *Server) registerPhotoTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("get_photo",
			mcp.WithDescription("Get photo metadata (title, description, date, GPS, camera, faces, labels)"),
			mcp.WithString("photo_uid", mcp.Required(), mcp.Description("Photo UID")),
		),
		s.handleGetPhoto,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("get_photo_thumbnail",
			mcp.WithDescription("Get a photo thumbnail as base64-encoded JPEG image"),
			mcp.WithString("photo_uid", mcp.Required(), mcp.Description("Photo UID")),
			mcp.WithString("size", mcp.Description(
				"Thumbnail size (default: fit_720). Options: fit_720, fit_1280, fit_2048, tile_500, tile_224")),
		),
		s.handleGetPhotoThumbnail,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("find_similar_photos",
			mcp.WithDescription(
				"Find visually similar photos using CLIP embeddings with book placement info"),
			mcp.WithString("photo_uid", mcp.Required(),
				mcp.Description("Photo UID to find similar photos for")),
			mcp.WithNumber("limit", mcp.Description("Max results (default 10, max 50)")),
			mcp.WithString("scope_section_id",
				mcp.Description("Limit results to photos in this section (section UUID)")),
			mcp.WithString("scope_book_id",
				mcp.Description("Limit results to photos in this book (book UUID)")),
		),
		s.handleFindSimilarPhotos,
	)
}

// handleGetPhoto returns photo metadata including faces and labels.
func (s *Server) handleGetPhoto(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	photoUID, err := requiredStr(args, "photo_uid")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Get basic photo data.
	photos, err := s.pp.GetPhotosWithQuery(1, 0, "uid:"+photoUID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get photo: %v", err)), nil
	}
	if len(photos) == 0 {
		return mcp.NewToolResultError("photo " + photoUID + " not found"), nil
	}
	photo := photos[0]

	// Get detailed info (labels, camera make, lens).
	details, err := s.pp.GetPhotoDetails(photoUID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get photo details: %v", err)), nil
	}

	// Get face markers.
	markers, _ := s.pp.GetPhotoMarkers(photoUID)
	faces := buildFaceList(markers)

	result := map[string]any{
		"uid":            photo.UID,
		"title":          photo.Title,
		"description":    photo.Description,
		"taken_at":       photo.TakenAt,
		"taken_at_local": photo.TakenAtLocal,
		"width":          photo.Width,
		"height":         photo.Height,
		"lat":            photo.Lat,
		"lng":            photo.Lng,
		"camera_make":    mapStringFromDetails(details, "CameraMake"),
		"camera_model":   photo.CameraModel,
		"lens_model":     mapStringFromDetails(details, "LensModel"),
		"favorite":       photo.Favorite,
		"private":        photo.Private,
		"type":           photo.Type,
		"faces":          faces,
		"labels":         extractLabels(details),
	}
	return jsonResult(result)
}

// handleGetPhotoThumbnail returns a base64-encoded thumbnail image.
func (s *Server) handleGetPhotoThumbnail(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	photoUID, err := requiredStr(args, "photo_uid")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	size := optionalStr(args, "size")
	if size == "" {
		size = "fit_720"
	}

	validSizes := map[string]bool{
		"fit_720": true, "fit_1280": true, "fit_2048": true,
		"tile_500": true, "tile_224": true,
	}
	if !validSizes[size] {
		return mcp.NewToolResultError(
			"invalid size: must be one of fit_720, fit_1280, fit_2048, tile_500, tile_224"), nil
	}

	// Get photo hash for thumbnail URL.
	photos, err := s.pp.GetPhotosWithQuery(1, 0, "uid:"+photoUID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get photo: %v", err)), nil
	}
	if len(photos) == 0 {
		return mcp.NewToolResultError("photo " + photoUID + " not found"), nil
	}
	hash := photos[0].Hash
	if hash == "" {
		return mcp.NewToolResultError("photo has no thumbnail"), nil
	}

	// Download thumbnail bytes.
	data, contentType, err := s.pp.GetPhotoThumbnail(hash, size)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get thumbnail: %v", err)), nil
	}

	if contentType == "" {
		contentType = "image/jpeg"
	}

	w, h := estimateThumbnailSize(photos[0].Width, photos[0].Height, size)

	result := map[string]any{
		"mime_type": contentType,
		"data":      base64.StdEncoding.EncodeToString(data),
		"width":     w,
		"height":    h,
	}
	return jsonResult(result)
}

// handleFindSimilarPhotos finds visually similar photos using CLIP embeddings.
func (s *Server) handleFindSimilarPhotos(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	photoUID, err := requiredStr(args, "photo_uid")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	limit := clampInt(optionalInt(args, "limit", 10), 1, 50)
	scopeSectionID := optionalStr(args, "scope_section_id")
	scopeBookID := optionalStr(args, "scope_book_id")

	if s.embeddingReader == nil {
		return mcp.NewToolResultError("embedding reader not available"), nil
	}

	bgCtx := s.ctx()

	// Get source embedding.
	sourceEmb, err := s.embeddingReader.Get(bgCtx, photoUID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get embedding: %v", err)), nil
	}
	if sourceEmb == nil {
		return mcp.NewToolResultError("no embedding found for photo " + photoUID), nil
	}

	// Build scope filter if requested.
	scopeUIDs, err := s.resolveScopeUIDs(bgCtx, scopeSectionID, scopeBookID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Search for more candidates than needed to account for filtering.
	searchLimit := limit + 1
	if scopeUIDs != nil {
		searchLimit = max(limit*5, 100)
	}

	similar, distances, err := s.embeddingReader.FindSimilarWithDistance(
		bgCtx, sourceEmb.Embedding, searchLimit, 1.0,
	)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to find similar: %v", err)), nil
	}

	enrichment := s.resolveBookEnrichment(bgCtx, scopeBookID, scopeSectionID)

	results := buildSimilarResults(similar, distances, photoUID, scopeUIDs, enrichment, limit)

	return jsonResult(map[string]any{
		"source_photo_uid": photoUID,
		"results":          results,
		"count":            len(results),
	})
}

// bookEnrichment holds precomputed book placement data.
type bookEnrichment struct {
	pagePlaced map[string]bool   // photo UID -> placed on a page
	sectionMap map[string]string // photo UID -> section title
}

// resolveScopeUIDs returns the set of photo UIDs to filter by, or nil if no scope.
//
//nolint:nilnil // nil means "no scope filter" — callers check for nil
func (s *Server) resolveScopeUIDs(
	ctx context.Context, scopeSectionID, scopeBookID string,
) (map[string]bool, error) {
	if scopeSectionID != "" {
		return s.getSectionPhotoUIDs(ctx, scopeSectionID)
	}
	if scopeBookID != "" {
		return s.getBookPhotoUIDs(ctx, scopeBookID)
	}
	return nil, nil
}

// resolveBookEnrichment builds book enrichment data if a scope is set.
func (s *Server) resolveBookEnrichment(
	ctx context.Context, scopeBookID, scopeSectionID string,
) bookEnrichment {
	e := bookEnrichment{
		pagePlaced: make(map[string]bool),
		sectionMap: make(map[string]string),
	}

	bookID := scopeBookID
	if bookID == "" && scopeSectionID != "" {
		sec, err := s.bookWriter.GetSection(ctx, scopeSectionID)
		if err == nil && sec != nil {
			bookID = sec.BookID
		}
	}
	if bookID == "" {
		return e
	}

	e.pagePlaced, e.sectionMap = s.buildBookEnrichment(ctx, bookID)
	return e
}

// similarResult is a single similar photo entry in the response.
type similarResult struct {
	UID        string  `json:"uid"`
	Similarity float64 `json:"similarity"`
	InBook     bool    `json:"in_book"`
	InSection  string  `json:"in_section"`
}

// buildSimilarResults filters and enriches similarity search results.
func buildSimilarResults(
	similar []database.StoredEmbedding, distances []float64,
	sourceUID string, scopeUIDs map[string]bool,
	enrichment bookEnrichment, limit int,
) []similarResult {
	results := make([]similarResult, 0, limit)
	for i, emb := range similar {
		if emb.PhotoUID == sourceUID {
			continue
		}
		if scopeUIDs != nil && !scopeUIDs[emb.PhotoUID] {
			continue
		}

		similarity := 1 - distances[i]
		if similarity < 0 {
			similarity = 0
		}

		results = append(results, similarResult{
			UID:        emb.PhotoUID,
			Similarity: similarity,
			InBook:     enrichment.pagePlaced[emb.PhotoUID],
			InSection:  enrichment.sectionMap[emb.PhotoUID],
		})

		if len(results) >= limit {
			break
		}
	}
	return results
}

// getSectionPhotoUIDs returns a set of photo UIDs in a section.
func (s *Server) getSectionPhotoUIDs(
	ctx context.Context, sectionID string,
) (map[string]bool, error) {
	photos, err := s.bookWriter.GetSectionPhotos(ctx, sectionID)
	if err != nil {
		return nil, fmt.Errorf("get section photos: %w", err)
	}
	uids := make(map[string]bool, len(photos))
	for _, p := range photos {
		uids[p.PhotoUID] = true
	}
	return uids, nil
}

// getBookPhotoUIDs returns a set of photo UIDs across all sections of a book.
func (s *Server) getBookPhotoUIDs(
	ctx context.Context, bookID string,
) (map[string]bool, error) {
	sections, err := s.bookWriter.GetSections(ctx, bookID)
	if err != nil {
		return nil, fmt.Errorf("get book sections: %w", err)
	}
	uids := make(map[string]bool)
	for _, sec := range sections {
		photos, err := s.bookWriter.GetSectionPhotos(ctx, sec.ID)
		if err != nil {
			continue
		}
		for _, p := range photos {
			uids[p.PhotoUID] = true
		}
	}
	return uids, nil
}

// buildBookEnrichment builds sets for page placement and section membership.
func (s *Server) buildBookEnrichment(
	ctx context.Context, bookID string,
) (map[string]bool, map[string]string) {
	pagePlaced := s.collectPagePlacedUIDs(ctx, bookID)
	sectionMap := s.collectSectionMembership(ctx, bookID)
	return pagePlaced, sectionMap
}

// collectPagePlacedUIDs returns a set of photo UIDs placed on pages in a book.
func (s *Server) collectPagePlacedUIDs(ctx context.Context, bookID string) map[string]bool {
	placed := make(map[string]bool)
	pages, err := s.bookWriter.GetPages(ctx, bookID)
	if err != nil {
		return placed
	}
	for _, page := range pages {
		for _, slot := range page.Slots {
			if slot.PhotoUID != "" {
				placed[slot.PhotoUID] = true
			}
		}
	}
	return placed
}

// collectSectionMembership maps photo UIDs to their section titles in a book.
func (s *Server) collectSectionMembership(ctx context.Context, bookID string) map[string]string {
	sectionMap := make(map[string]string)
	sections, err := s.bookWriter.GetSections(ctx, bookID)
	if err != nil {
		return sectionMap
	}
	for _, sec := range sections {
		photos, secErr := s.bookWriter.GetSectionPhotos(ctx, sec.ID)
		if secErr != nil {
			continue
		}
		for _, p := range photos {
			sectionMap[p.PhotoUID] = sec.Title
		}
	}
	return sectionMap
}

// --- helpers ---

// optionalInt extracts an integer from the args map, returning defaultVal if absent.
func optionalInt(args map[string]any, key string, defaultVal int) int {
	v, ok := args[key]
	if !ok || v == nil {
		return defaultVal
	}
	f, ok := v.(float64)
	if !ok {
		return defaultVal
	}
	return int(f)
}

// clampInt clamps n to [lo, hi].
func clampInt(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// buildFaceList extracts face names from markers.
func buildFaceList(markers []photoprism.Marker) []map[string]any {
	faces := make([]map[string]any, 0)
	for _, m := range markers {
		if m.Type != "face" {
			continue
		}
		face := map[string]any{"name": m.Name}
		if m.SubjUID != "" {
			face["subject_uid"] = m.SubjUID
		}
		faces = append(faces, face)
	}
	return faces
}

// extractLabels pulls label names from the photo details response.
func extractLabels(details map[string]any) []string {
	labelsRaw, ok := details["Labels"].([]any)
	if !ok {
		return nil
	}
	labels := make([]string, 0, len(labelsRaw))
	for _, raw := range labelsRaw {
		labelMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		labelObj, ok := labelMap["Label"].(map[string]any)
		if !ok {
			continue
		}
		if name, ok := labelObj["Name"].(string); ok && name != "" {
			labels = append(labels, name)
		}
	}
	return labels
}

// mapStringFromDetails extracts a string field from the photo details map.
func mapStringFromDetails(details map[string]any, key string) string {
	if v, ok := details[key].(string); ok {
		return v
	}
	return ""
}

// estimateThumbnailSize estimates thumbnail dimensions based on original size and format.
func estimateThumbnailSize(origW, origH int, size string) (int, int) {
	if origW == 0 || origH == 0 {
		return 0, 0
	}
	maxDim := thumbMaxDim(size)
	if maxDim == 0 {
		// Tile sizes are square crops.
		return thumbTileSize(size), thumbTileSize(size)
	}
	return scaleToDim(origW, origH, maxDim)
}

// thumbMaxDim returns the max dimension for fit_ sizes, 0 for tile_ sizes.
func thumbMaxDim(size string) int {
	switch size {
	case "fit_720":
		return 720
	case "fit_1280":
		return 1280
	case "fit_2048":
		return 2048
	default:
		return 0
	}
}

// thumbTileSize returns the tile dimension for tile_ sizes.
func thumbTileSize(size string) int {
	switch size {
	case "tile_224":
		return 224
	case "tile_500":
		return 500
	default:
		return 0
	}
}

// scaleToDim scales dimensions to fit within maxDim, preserving aspect ratio.
func scaleToDim(w, h, maxDim int) (int, int) {
	if w >= h {
		if w <= maxDim {
			return w, h
		}
		return maxDim, h * maxDim / w
	}
	if h <= maxDim {
		return w, h
	}
	return w * maxDim / h, maxDim
}
