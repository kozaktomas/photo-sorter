package sorter

import (
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/photoprism"
)

func TestPhotoToMetadata_BasicFields(t *testing.T) {
	photo := photoprism.Photo{
		UID:          "photo123",
		OriginalName: "IMG_0001.jpg",
		FileName:     "2024/01/photo.jpg",
		TakenAt:      "2024-01-15T10:30:00Z",
		Year:         2024,
		Month:        1,
		Day:          15,
		Country:      "cz",
		Lat:          50.0,
		Lng:          14.0,
		Width:        4032,
		Height:       3024,
	}

	metadata := photoToMetadata(photo, false)

	if metadata.OriginalName != "IMG_0001.jpg" {
		t.Errorf("expected OriginalName 'IMG_0001.jpg', got '%s'", metadata.OriginalName)
	}

	if metadata.FileName != "2024/01/photo.jpg" {
		t.Errorf("expected FileName '2024/01/photo.jpg', got '%s'", metadata.FileName)
	}

	if metadata.TakenAt != "2024-01-15T10:30:00Z" {
		t.Errorf("expected TakenAt '2024-01-15T10:30:00Z', got '%s'", metadata.TakenAt)
	}

	if metadata.Year != 2024 {
		t.Errorf("expected Year 2024, got %d", metadata.Year)
	}

	if metadata.Month != 1 {
		t.Errorf("expected Month 1, got %d", metadata.Month)
	}

	if metadata.Day != 15 {
		t.Errorf("expected Day 15, got %d", metadata.Day)
	}
}

func TestPhotoToMetadata_Location(t *testing.T) {
	photo := photoprism.Photo{
		Country: "cz",
		Lat:     49.1951,
		Lng:     16.6068,
	}

	metadata := photoToMetadata(photo, false)

	if metadata.Country != "cz" {
		t.Errorf("expected Country 'cz', got '%s'", metadata.Country)
	}

	if metadata.Lat != 49.1951 {
		t.Errorf("expected Lat 49.1951, got %f", metadata.Lat)
	}

	if metadata.Lng != 16.6068 {
		t.Errorf("expected Lng 16.6068, got %f", metadata.Lng)
	}
}

func TestPhotoToMetadata_Dimensions(t *testing.T) {
	photo := photoprism.Photo{
		Width:  1920,
		Height: 1080,
	}

	metadata := photoToMetadata(photo, false)

	if metadata.Width != 1920 {
		t.Errorf("expected Width 1920, got %d", metadata.Width)
	}

	if metadata.Height != 1080 {
		t.Errorf("expected Height 1080, got %d", metadata.Height)
	}
}

func TestPhotoToMetadata_ClearDateFalse(t *testing.T) {
	photo := photoprism.Photo{
		TakenAt: "2024-01-15T10:30:00Z",
		Year:    2024,
		Month:   1,
		Day:     15,
	}

	metadata := photoToMetadata(photo, false)

	// Date fields should be preserved
	if metadata.TakenAt != "2024-01-15T10:30:00Z" {
		t.Errorf("expected TakenAt preserved, got '%s'", metadata.TakenAt)
	}

	if metadata.Year != 2024 {
		t.Errorf("expected Year 2024, got %d", metadata.Year)
	}

	if metadata.Month != 1 {
		t.Errorf("expected Month 1, got %d", metadata.Month)
	}

	if metadata.Day != 15 {
		t.Errorf("expected Day 15, got %d", metadata.Day)
	}
}

func TestPhotoToMetadata_ClearDateTrue(t *testing.T) {
	photo := photoprism.Photo{
		TakenAt:      "2024-01-15T10:30:00Z",
		Year:         2024,
		Month:        1,
		Day:          15,
		OriginalName: "test.jpg", // Should still be preserved
	}

	metadata := photoToMetadata(photo, true)

	// Date fields should be cleared
	if metadata.TakenAt != "" {
		t.Errorf("expected TakenAt cleared, got '%s'", metadata.TakenAt)
	}

	if metadata.Year != 0 {
		t.Errorf("expected Year 0, got %d", metadata.Year)
	}

	if metadata.Month != 0 {
		t.Errorf("expected Month 0, got %d", metadata.Month)
	}

	if metadata.Day != 0 {
		t.Errorf("expected Day 0, got %d", metadata.Day)
	}

	// Non-date fields should still be preserved
	if metadata.OriginalName != "test.jpg" {
		t.Errorf("expected OriginalName preserved, got '%s'", metadata.OriginalName)
	}
}

func TestPhotoToMetadata_EmptyPhoto(t *testing.T) {
	photo := photoprism.Photo{}

	metadata := photoToMetadata(photo, false)

	// Should not panic, should return zero values
	if metadata == nil {
		t.Fatal("expected non-nil metadata")
		return
	}

	if metadata.OriginalName != "" {
		t.Errorf("expected empty OriginalName, got '%s'", metadata.OriginalName)
	}

	if metadata.Year != 0 {
		t.Errorf("expected Year 0, got %d", metadata.Year)
	}
}

func TestPhotoToMetadata_ZeroCoordinates(t *testing.T) {
	photo := photoprism.Photo{
		Lat: 0.0,
		Lng: 0.0,
	}

	metadata := photoToMetadata(photo, false)

	// Zero coordinates should be preserved (location at 0,0 is valid)
	if metadata.Lat != 0.0 {
		t.Errorf("expected Lat 0.0, got %f", metadata.Lat)
	}

	if metadata.Lng != 0.0 {
		t.Errorf("expected Lng 0.0, got %f", metadata.Lng)
	}
}

func TestPhotoToMetadata_NegativeCoordinates(t *testing.T) {
	photo := photoprism.Photo{
		Lat: -33.8688, // Sydney
		Lng: 151.2093,
	}

	metadata := photoToMetadata(photo, false)

	if metadata.Lat != -33.8688 {
		t.Errorf("expected Lat -33.8688, got %f", metadata.Lat)
	}

	if metadata.Lng != 151.2093 {
		t.Errorf("expected Lng 151.2093, got %f", metadata.Lng)
	}
}

func TestPhotoToMetadata_PreservesNonDateFieldsWhenClearing(t *testing.T) {
	photo := photoprism.Photo{
		OriginalName: "vacation.jpg",
		FileName:     "2024/vacation.jpg",
		Country:      "es",
		Lat:          40.4168,
		Lng:          -3.7038,
		Width:        4000,
		Height:       3000,
		TakenAt:      "2024-06-15T14:00:00Z",
		Year:         2024,
		Month:        6,
		Day:          15,
	}

	metadata := photoToMetadata(photo, true)

	// Non-date fields should be preserved
	if metadata.OriginalName != "vacation.jpg" {
		t.Errorf("expected OriginalName 'vacation.jpg', got '%s'", metadata.OriginalName)
	}

	if metadata.FileName != "2024/vacation.jpg" {
		t.Errorf("expected FileName '2024/vacation.jpg', got '%s'", metadata.FileName)
	}

	if metadata.Country != "es" {
		t.Errorf("expected Country 'es', got '%s'", metadata.Country)
	}

	if metadata.Lat != 40.4168 {
		t.Errorf("expected Lat 40.4168, got %f", metadata.Lat)
	}

	if metadata.Lng != -3.7038 {
		t.Errorf("expected Lng -3.7038, got %f", metadata.Lng)
	}

	if metadata.Width != 4000 {
		t.Errorf("expected Width 4000, got %d", metadata.Width)
	}

	if metadata.Height != 3000 {
		t.Errorf("expected Height 3000, got %d", metadata.Height)
	}

	// Date fields should be cleared
	if metadata.TakenAt != "" {
		t.Errorf("expected TakenAt cleared")
	}

	if metadata.Year != 0 {
		t.Errorf("expected Year cleared")
	}
}

func TestPhotoToMetadata_ReturnsPointer(t *testing.T) {
	photo := photoprism.Photo{
		OriginalName: "test.jpg",
	}

	metadata := photoToMetadata(photo, false)

	if metadata == nil {
		t.Fatal("expected non-nil pointer")
		return
	}

	// Modifying the returned metadata should not affect the original photo
	metadata.OriginalName = "modified.jpg"

	if photo.OriginalName != "test.jpg" {
		t.Error("modifying metadata should not affect original photo")
	}
}

func TestPhotoToMetadata_SpecialCharacters(t *testing.T) {
	photo := photoprism.Photo{
		OriginalName: "фото 2024 (копия).jpg",
		FileName:     "путь/к/файлу.jpg",
	}

	metadata := photoToMetadata(photo, false)

	if metadata.OriginalName != "фото 2024 (копия).jpg" {
		t.Errorf("expected unicode OriginalName preserved, got '%s'", metadata.OriginalName)
	}

	if metadata.FileName != "путь/к/файлу.jpg" {
		t.Errorf("expected unicode FileName preserved, got '%s'", metadata.FileName)
	}
}

func TestPhotoToMetadata_LongFilename(t *testing.T) {
	longName := "very_long_filename_that_exceeds_normal_limits_" +
		"with_additional_text_and_numbers_12345678901234567890.jpg"

	photo := photoprism.Photo{
		OriginalName: longName,
	}

	metadata := photoToMetadata(photo, false)

	if metadata.OriginalName != longName {
		t.Errorf("expected long filename preserved")
	}
}

func TestNew(t *testing.T) {
	// New() should not panic with nil arguments
	// (actual usage would require real PhotoPrism client and AI provider)
	sorter := New(nil, nil)

	if sorter == nil {
		t.Error("expected non-nil sorter")
	}
}

func TestSortOptions_Defaults(t *testing.T) {
	opts := SortOptions{}

	// Default values
	if opts.DryRun != false {
		t.Error("expected DryRun default to be false")
	}

	if opts.Limit != 0 {
		t.Error("expected Limit default to be 0")
	}

	if opts.IndividualDates != false {
		t.Error("expected IndividualDates default to be false")
	}

	if opts.BatchMode != false {
		t.Error("expected BatchMode default to be false")
	}

	if opts.ForceDate != false {
		t.Error("expected ForceDate default to be false")
	}

	if opts.Concurrency != 0 {
		t.Error("expected Concurrency default to be 0")
	}
}

func TestSortResult_Defaults(t *testing.T) {
	result := SortResult{}

	if result.ProcessedCount != 0 {
		t.Error("expected ProcessedCount default to be 0")
	}

	if result.SortedCount != 0 {
		t.Error("expected SortedCount default to be 0")
	}

	if result.AlbumDate != "" {
		t.Error("expected AlbumDate default to be empty")
	}

	if result.DateReasoning != "" {
		t.Error("expected DateReasoning default to be empty")
	}

	if result.Errors != nil {
		t.Error("expected Errors default to be nil")
	}

	if result.Suggestions != nil {
		t.Error("expected Suggestions default to be nil")
	}
}

func TestProgressInfo_Fields(t *testing.T) {
	info := ProgressInfo{
		Phase:    "analyzing",
		Current:  5,
		Total:    10,
		PhotoUID: "photo123",
		Message:  "Processing photo",
	}

	if info.Phase != "analyzing" {
		t.Errorf("expected Phase 'analyzing', got '%s'", info.Phase)
	}

	if info.Current != 5 {
		t.Errorf("expected Current 5, got %d", info.Current)
	}

	if info.Total != 10 {
		t.Errorf("expected Total 10, got %d", info.Total)
	}

	if info.PhotoUID != "photo123" {
		t.Errorf("expected PhotoUID 'photo123', got '%s'", info.PhotoUID)
	}

	if info.Message != "Processing photo" {
		t.Errorf("expected Message 'Processing photo', got '%s'", info.Message)
	}
}
