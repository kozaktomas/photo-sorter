package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/kozaktomas/photo-sorter/internal/ai"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/mark3labs/mcp-go/mcp"
)

// usdToCZK is the approximate USD to CZK conversion rate.
const usdToCZK = 23.5

// textModel is the model used for text operations.
const textModel = ai.TextModel

// computeCostCZK calculates cost in CZK from token usage and model pricing.
func (s *Server) computeCostCZK(usage ai.TokenUsage) float64 {
	pricing := s.config.GetModelPricing(textModel)
	inputCostUSD := float64(usage.PromptTokens) / 1_000_000 * pricing.Standard.Input
	outputCostUSD := float64(usage.CompletionTokens) / 1_000_000 * pricing.Standard.Output
	return (inputCostUSD + outputCostUSD) * usdToCZK
}

// registerTextTools registers AI text and text version tools.
func (s *Server) registerTextTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("check_text",
			mcp.WithDescription("AI-powered text check for spelling, grammar, and diacritics (Czech)"),
			mcp.WithString("text", mcp.Required(), mcp.Description("Text to check")),
			mcp.WithString("source_type", mcp.Description("Source type for persistence: slot, section_photo, page_slot")),
			mcp.WithString("source_id",
				mcp.Description("Source ID for persistence (sectionID:photoUID or pageID:slotIndex)")),
			mcp.WithString("field", mcp.Description("Field name for persistence: description, note, or text_content")),
		),
		s.handleCheckText,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("rewrite_text",
			mcp.WithDescription("AI-powered text rewrite for length adjustment (Czech)"),
			mcp.WithString("text", mcp.Required(), mcp.Description("Text to rewrite")),
			mcp.WithString("target_length", mcp.Required(),
				mcp.Description("Target length: much_shorter, shorter, longer, much_longer")),
		),
		s.handleRewriteText,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("check_consistency",
			mcp.WithDescription("AI-powered style consistency check across all book texts"),
			mcp.WithString("book_id", mcp.Required(), mcp.Description("Book ID (UUID)")),
		),
		s.handleCheckConsistency,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("list_text_versions",
			mcp.WithDescription("List version history for a text field"),
			mcp.WithString("source_type", mcp.Required(), mcp.Description("Source type: section_photo or page_slot")),
			mcp.WithString("source_id", mcp.Required(),
				mcp.Description("Source ID (sectionID:photoUID or pageID:slotIndex)")),
			mcp.WithString("field", mcp.Required(), mcp.Description("Field name: description, note, or text_content")),
		),
		s.handleListTextVersions,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("restore_text_version",
			mcp.WithNumber("version_id", mcp.Required(), mcp.Description("Version ID to restore")),
		),
		s.handleRestoreTextVersion,
	)
}

// handleCheckText runs an AI text check and optionally persists the result.
func (s *Server) handleCheckText(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.config.OpenAI.Token == "" {
		return mcp.NewToolResultError("OpenAI not configured"), nil
	}

	args := req.GetArguments()
	text, err := requiredStr(args, "text")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result, err := ai.CheckText(s.ctx(), s.config.OpenAI.Token, text)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("text check failed: %v", err)), nil
	}

	costCZK := s.computeCostCZK(result.Usage)

	resp := map[string]any{
		"corrected_text":    result.CorrectedText,
		"readability_score": result.ReadabilityScore,
		"changes":           result.Changes,
		"cost_czk":          costCZK,
	}

	// Persist if source info is provided.
	sourceType := optionalStr(args, "source_type")
	sourceID := optionalStr(args, "source_id")
	field := optionalStr(args, "field")

	if sourceType != "" && sourceID != "" && field != "" {
		status := "clean"
		if len(result.Changes) > 0 {
			status = "has_errors"
		}

		h := sha256.Sum256([]byte(text))
		contentHash := hex.EncodeToString(h[:])

		dbResult := &database.TextCheckResult{
			SourceType:       sourceType,
			SourceID:         sourceID,
			Field:            field,
			ContentHash:      contentHash,
			Status:           status,
			ReadabilityScore: &result.ReadabilityScore,
			CorrectedText:    result.CorrectedText,
			Changes:          result.Changes,
			CostCZK:          costCZK,
		}
		if saveErr := s.textCheckStore.SaveTextCheckResult(s.ctx(), dbResult); saveErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("check succeeded but failed to save: %v", saveErr)), nil
		}
		resp["status"] = status
		resp["content_hash"] = contentHash
		resp["persisted"] = true
	}

	return jsonResult(resp)
}

// handleRewriteText rewrites text to a target length using AI.
func (s *Server) handleRewriteText(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.config.OpenAI.Token == "" {
		return mcp.NewToolResultError("OpenAI not configured"), nil
	}

	args := req.GetArguments()
	text, err := requiredStr(args, "text")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	targetLength, err := requiredStr(args, "target_length")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	validLengths := map[string]bool{
		"much_shorter": true,
		"shorter":      true,
		"longer":       true,
		"much_longer":  true,
	}
	if !validLengths[targetLength] {
		return mcp.NewToolResultError("target_length must be one of: much_shorter, shorter, longer, much_longer"), nil
	}

	result, err := ai.RewriteText(s.ctx(), s.config.OpenAI.Token, text, targetLength)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("text rewrite failed: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"rewritten_text": result.RewrittenText,
		"cost_czk":       s.computeCostCZK(result.Usage),
	})
}

// handleCheckConsistency gathers all texts from a book and checks style consistency.
func (s *Server) handleCheckConsistency(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.config.OpenAI.Token == "" {
		return mcp.NewToolResultError("OpenAI not configured"), nil
	}

	args := req.GetArguments()
	bookID, err := requiredStr(args, "book_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	texts, err := s.collectBookTexts(bookID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to collect book texts: %v", err)), nil
	}

	if len(texts) < 2 {
		return mcp.NewToolResultError("at least 2 texts are required for consistency check"), nil
	}

	result, err := ai.CheckConsistency(s.ctx(), s.config.OpenAI.Token, texts)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("consistency check failed: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"consistency_score": result.ConsistencyScore,
		"tone":              result.Tone,
		"issues":            result.Issues,
		"cost_czk":          s.computeCostCZK(result.Usage),
	})
}

// collectBookTexts gathers all text entries from a book for consistency checking.
func (s *Server) collectBookTexts(bookID string) ([]ai.ConsistencyTextEntry, error) {
	ctx := s.ctx()
	var texts []ai.ConsistencyTextEntry

	sectionTexts, err := s.collectSectionPhotoTexts(ctx, bookID)
	if err != nil {
		return nil, err
	}
	texts = append(texts, sectionTexts...)

	slotTexts, err := s.collectPageSlotTexts(ctx, bookID)
	if err != nil {
		return nil, err
	}
	texts = append(texts, slotTexts...)

	return texts, nil
}

// collectSectionPhotoTexts gathers text entries from section photo descriptions.
func (s *Server) collectSectionPhotoTexts(
	ctx context.Context, bookID string,
) ([]ai.ConsistencyTextEntry, error) {
	sections, err := s.bookWriter.GetSections(ctx, bookID)
	if err != nil {
		return nil, fmt.Errorf("get sections: %w", err)
	}
	var texts []ai.ConsistencyTextEntry
	for _, section := range sections {
		photos, photosErr := s.bookWriter.GetSectionPhotos(ctx, section.ID)
		if photosErr != nil {
			return nil, fmt.Errorf("get section photos: %w", photosErr)
		}
		for _, photo := range photos {
			if strings.TrimSpace(photo.Description) != "" {
				texts = append(texts, ai.ConsistencyTextEntry{
					ID:      fmt.Sprintf("section_photo:%s:%s:description", section.ID, photo.PhotoUID),
					Source:  "photo description",
					Content: photo.Description,
				})
			}
		}
	}
	return texts, nil
}

// collectPageSlotTexts gathers text entries from page text slots.
func (s *Server) collectPageSlotTexts(
	ctx context.Context, bookID string,
) ([]ai.ConsistencyTextEntry, error) {
	pages, err := s.bookWriter.GetPages(ctx, bookID)
	if err != nil {
		return nil, fmt.Errorf("get pages: %w", err)
	}
	var texts []ai.ConsistencyTextEntry
	for _, page := range pages {
		for _, slot := range page.Slots {
			if slot.IsTextSlot() {
				texts = append(texts, ai.ConsistencyTextEntry{
					ID:      fmt.Sprintf("page_slot:%s:%d:text_content", page.ID, slot.SlotIndex),
					Source:  "text slot",
					Content: slot.TextContent,
				})
			}
		}
	}
	return texts, nil
}

// handleListTextVersions returns version history for a text field.
func (s *Server) handleListTextVersions(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	sourceType, err := requiredStr(args, "source_type")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	sourceID, err := requiredStr(args, "source_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	field, err := requiredStr(args, "field")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	versions, err := s.textVersionStore.ListTextVersions(s.ctx(), sourceType, sourceID, field, 20)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list versions: %v", err)), nil
	}

	type versionItem struct {
		ID        int    `json:"id"`
		Content   string `json:"content"`
		ChangedBy string `json:"changed_by"`
		CreatedAt string `json:"created_at"`
	}

	result := make([]versionItem, len(versions))
	for i, v := range versions {
		result[i] = versionItem{
			ID:        v.ID,
			Content:   v.Content,
			ChangedBy: v.ChangedBy,
			CreatedAt: v.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}
	return jsonResult(result)
}

// handleRestoreTextVersion restores a previous text version.
func (s *Server) handleRestoreTextVersion(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	versionID, err := requiredInt(args, "version_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	ctx := s.ctx()

	version, err := s.textVersionStore.GetTextVersion(ctx, versionID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("version not found: %v", err)), nil
	}

	// Save current value as a version before restoring.
	currentContent, err := s.getCurrentField(ctx, version)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get current value: %v", err)), nil
	}

	if currentContent != version.Content {
		_ = s.textVersionStore.SaveTextVersion(ctx, &database.TextVersion{
			SourceType: version.SourceType,
			SourceID:   version.SourceID,
			Field:      version.Field,
			Content:    currentContent,
			ChangedBy:  "user",
		})
	}

	// Apply the restore.
	if err := s.applyRestore(ctx, version); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to restore: %v", err)), nil
	}

	return jsonResult(map[string]any{
		"content": version.Content,
		"message": "version restored",
	})
}

// getCurrentField retrieves the current value of a text field.
func (s *Server) getCurrentField(ctx context.Context, version *database.TextVersion) (string, error) {
	if version.SourceType == "page_slot" {
		return s.getCurrentPageSlotField(ctx, version.SourceID)
	}
	return s.getCurrentSectionPhotoField(ctx, version.SourceID, version.Field)
}

// getCurrentSectionPhotoField retrieves the current value of a section photo field.
func (s *Server) getCurrentSectionPhotoField(ctx context.Context, sourceID, field string) (string, error) {
	sectionID, photoUID := splitSourceID(sourceID)
	photos, err := s.bookWriter.GetSectionPhotos(ctx, sectionID)
	if err != nil {
		return "", fmt.Errorf("get section photos: %w", err)
	}
	for _, p := range photos {
		if p.PhotoUID == photoUID {
			if field == "note" {
				return p.Note, nil
			}
			return p.Description, nil
		}
	}
	return "", nil
}

// getCurrentPageSlotField retrieves the current text_content of a page slot.
func (s *Server) getCurrentPageSlotField(ctx context.Context, sourceID string) (string, error) {
	pageID, slotIdxStr := splitSourceID(sourceID)
	slotIndex, _ := strconv.Atoi(slotIdxStr)
	slots, err := s.bookWriter.GetPageSlots(ctx, pageID)
	if err != nil {
		return "", fmt.Errorf("get page slots: %w", err)
	}
	for _, sl := range slots {
		if sl.SlotIndex == slotIndex {
			return sl.TextContent, nil
		}
	}
	return "", nil
}

// applyRestore applies the restored content based on source type.
func (s *Server) applyRestore(ctx context.Context, version *database.TextVersion) error {
	if version.SourceType == "page_slot" {
		pageID, slotIdxStr := splitSourceID(version.SourceID)
		slotIndex, _ := strconv.Atoi(slotIdxStr)
		if err := s.bookWriter.AssignTextSlot(ctx, pageID, slotIndex, version.Content); err != nil {
			return fmt.Errorf("assign text slot: %w", err)
		}
		return nil
	}

	// section_photo
	sectionID, photoUID := splitSourceID(version.SourceID)
	photos, err := s.bookWriter.GetSectionPhotos(ctx, sectionID)
	if err != nil {
		return fmt.Errorf("get section photos: %w", err)
	}
	for _, p := range photos {
		if p.PhotoUID == photoUID {
			desc, note := p.Description, p.Note
			if version.Field == "description" {
				desc = version.Content
			} else {
				note = version.Content
			}
			if err := s.bookWriter.UpdateSectionPhoto(ctx, sectionID, photoUID, desc, note); err != nil {
				return fmt.Errorf("update section photo: %w", err)
			}
			return nil
		}
	}
	return nil
}

// splitSourceID splits "a:b" into two parts (splitting on the last colon).
func splitSourceID(sourceID string) (string, string) {
	for i := len(sourceID) - 1; i >= 0; i-- {
		if sourceID[i] == ':' {
			return sourceID[:i], sourceID[i+1:]
		}
	}
	return sourceID, ""
}
