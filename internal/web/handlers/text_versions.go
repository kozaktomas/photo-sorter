package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/database"
)

// TextVersionsHandler handles text version history endpoints.
type TextVersionsHandler struct{}

// NewTextVersionsHandler creates a new text versions handler.
func NewTextVersionsHandler() *TextVersionsHandler {
	return &TextVersionsHandler{}
}

func getTextVersionStore(w http.ResponseWriter, r *http.Request) database.TextVersionStore {
	store, err := database.GetTextVersionStore(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "text version storage not available")
		return nil
	}
	return store
}

// List handles GET /api/v1/text-versions and returns version history for a text field.
func (h *TextVersionsHandler) List(w http.ResponseWriter, r *http.Request) {
	store := getTextVersionStore(w, r)
	if store == nil {
		return
	}

	sourceType := r.URL.Query().Get("source_type")
	sourceID := r.URL.Query().Get("source_id")
	field := r.URL.Query().Get("field")

	if sourceType == "" || sourceID == "" || field == "" {
		respondError(w, http.StatusBadRequest, "source_type, source_id, and field are required")
		return
	}

	versions, err := store.ListTextVersions(r.Context(), sourceType, sourceID, field, 20)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list versions")
		return
	}

	type versionResponse struct {
		ID        int    `json:"id"`
		Content   string `json:"content"`
		ChangedBy string `json:"changed_by"`
		CreatedAt string `json:"created_at"`
	}

	result := make([]versionResponse, 0, len(versions))
	for _, v := range versions {
		result = append(result, versionResponse{
			ID:        v.ID,
			Content:   v.Content,
			ChangedBy: v.ChangedBy,
			CreatedAt: v.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	respondJSON(w, http.StatusOK, result)
}

// Restore handles POST /api/v1/text-versions/:id/restore.
// It saves the current text as a version, then updates the current text to the restored version's content.
func (h *TextVersionsHandler) Restore(w http.ResponseWriter, r *http.Request) {
	store := getTextVersionStore(w, r)
	if store == nil {
		return
	}

	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid version id")
		return
	}

	version, err := store.GetTextVersion(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "version not found")
		return
	}

	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}

	if err := restoreVersion(r, store, bw, version); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to restore version")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"content": version.Content})
}

// restoreVersion saves the current value as a version and applies the restored content.
func restoreVersion(
	r *http.Request, store database.TextVersionStore,
	bw database.BookWriter, version *database.TextVersion,
) error {
	currentContent, err := getCurrentField(r, bw, version)
	if err != nil {
		return err
	}

	// Save current value as a version if different.
	if currentContent != version.Content {
		_ = store.SaveTextVersion(r.Context(), &database.TextVersion{
			SourceType: version.SourceType,
			SourceID:   version.SourceID,
			Field:      version.Field,
			Content:    currentContent,
			ChangedBy:  "user",
		})
	}

	return applyRestore(r, bw, version)
}

// getCurrentField retrieves the current value of a text field based on source type.
func getCurrentField(
	r *http.Request, bw database.BookWriter, version *database.TextVersion,
) (string, error) {
	if version.SourceType == "page_slot" {
		return getCurrentPageSlotField(r, bw, version.SourceID)
	}
	return getCurrentSectionPhotoField(r, bw, version.SourceID, version.Field)
}

// applyRestore applies the restored content based on source type.
func applyRestore(
	r *http.Request, bw database.BookWriter, version *database.TextVersion,
) error {
	if version.SourceType == "page_slot" {
		return applyPageSlotRestore(r, bw, version)
	}
	return applySectionPhotoRestore(r, bw, version)
}

// getCurrentSectionPhotoField retrieves the current value of a section photo field.
// sourceID format: "sectionID:photoUID".
func getCurrentSectionPhotoField(
	r *http.Request, bw database.BookWriter, sourceID, field string,
) (string, error) {
	sectionID, photoUID := splitSourceID(sourceID)
	photos, err := bw.GetSectionPhotos(r.Context(), sectionID)
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
// sourceID format: "pageID:slotIndex".
func getCurrentPageSlotField(
	r *http.Request, bw database.BookWriter, sourceID string,
) (string, error) {
	pageID, slotIdxStr := splitSourceID(sourceID)
	slotIndex, _ := strconv.Atoi(slotIdxStr)
	slots, err := bw.GetPageSlots(r.Context(), pageID)
	if err != nil {
		return "", fmt.Errorf("get page slots: %w", err)
	}
	for _, s := range slots {
		if s.SlotIndex == slotIndex {
			return s.TextContent, nil
		}
	}
	return "", nil
}

// applySectionPhotoRestore applies a restored version to a section photo field.
func applySectionPhotoRestore(
	r *http.Request, bw database.BookWriter, version *database.TextVersion,
) error {
	sectionID, photoUID := splitSourceID(version.SourceID)
	photos, err := bw.GetSectionPhotos(r.Context(), sectionID)
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
			if err := bw.UpdateSectionPhoto(r.Context(), sectionID, photoUID, desc, note); err != nil {
				return fmt.Errorf("update section photo: %w", err)
			}
			return nil
		}
	}
	return nil
}

// applyPageSlotRestore applies a restored version to a page slot text_content.
func applyPageSlotRestore(
	r *http.Request, bw database.BookWriter, version *database.TextVersion,
) error {
	pageID, slotIdxStr := splitSourceID(version.SourceID)
	slotIndex, _ := strconv.Atoi(slotIdxStr)
	if err := bw.AssignTextSlot(r.Context(), pageID, slotIndex, version.Content); err != nil {
		return fmt.Errorf("assign text slot: %w", err)
	}
	return nil
}

// splitSourceID splits "a:b" into two parts.
func splitSourceID(sourceID string) (string, string) {
	for i := len(sourceID) - 1; i >= 0; i-- {
		if sourceID[i] == ':' {
			return sourceID[:i], sourceID[i+1:]
		}
	}
	return sourceID, ""
}
