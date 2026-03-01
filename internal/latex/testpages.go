package latex

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

//go:embed templates/testpage.tex
var testTemplateFS embed.FS

// testColumn holds data for a single grid column in the test overlay.
type testColumn struct {
	LeftX   float64
	RightX  float64
	CenterX float64
	Number  int
}

// testFormatSlot holds data for a format sample slot visualization.
type testFormatSlot struct {
	X, Y, W, H float64
	CX, CY     float64 // center
	Color      string
}

// testFormat holds data for a complete format sample page.
type testFormat struct {
	Name      string
	SlotCount int
	Slots     []testFormatSlot
}

// testPageData is the root data passed to the test page template.
type testPageData struct {
	// Grid overlay.
	Columns       []testColumn
	ContentLeftX  float64
	ContentRightX float64
	ContentW      float64
	ColumnW       float64
	GutterW       float64
	CanvasTopY    float64
	CanvasBottomY float64
	HeaderTopY    float64
	FooterBottomY float64
	// Baseline.
	BaselineUnit float64
	BaselineYs   []float64
	// Formats.
	Formats []testFormat
	// Gutter-safe.
	GutterSafeMM       float64
	GutterSafeRightX   float64
	SampleMarkerX      float64
	SampleMarkerY      float64
	SampleMarkerCX     float64
	SampleMarkerCY     float64
	SampleMarkerLabelX float64
}

var slotColors = []string{"red", "blue", "green!60!black", "orange"}

// GenerateTestPDF creates a diagnostic PDF showing grid, baseline, format samples, and gutter-safe zones.
func GenerateTestPDF(ctx context.Context) ([]byte, error) {
	config := DefaultLayoutConfig()
	data := buildTestData(config)

	tmpDir, err := os.MkdirTemp("", "book-test-pdf-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	funcMap := template.FuncMap{
		"latexEscape":   latexEscape,
		"addFloat":      func(a, b float64) float64 { return a + b },
		"subtractFloat": func(a, b float64) float64 { return a - b },
		"mulFloat":      func(a, b float64) float64 { return a * b },
		"divFloat":      func(a, b float64) float64 { return a / b },
	}

	tmpl, err := template.New("testpage.tex").Funcs(funcMap).ParseFS(testTemplateFS, "templates/testpage.tex")
	if err != nil {
		return nil, fmt.Errorf("failed to parse test template: %w", err)
	}

	texPath := filepath.Join(tmpDir, "testpage.tex")
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to execute test template: %w", err)
	}
	if err := os.WriteFile(texPath, buf.Bytes(), 0600); err != nil {
		return nil, fmt.Errorf("failed to write test tex file: %w", err)
	}

	// Run lualatex twice for remember picture.
	for pass := range 2 {
		cmd := exec.CommandContext(ctx, "lualatex", //nolint:gosec
			"-interaction=nonstopmode",
			"-output-directory="+tmpDir,
			texPath,
		)
		cmd.Dir = tmpDir
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("lualatex pass %d failed: %w\n%s", pass+1, err, string(output))
		}
	}

	pdfPath := filepath.Join(tmpDir, "testpage.pdf")
	pdfData, err := os.ReadFile(pdfPath) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("failed to read test PDF: %w", err)
	}
	return pdfData, nil
}

//nolint:funlen // Test data construction with many page formats.
func buildTestData(config LayoutConfig) testPageData {
	// Use recto layout (inside margin on left).
	contentLeftX := config.InsideMarginMM
	contentW := config.ContentWidth()
	contentRightX := contentLeftX + contentW

	topEdge := PageH - config.TopMarginMM
	canvasTopY := topEdge - config.HeaderHeightMM
	canvasBottomY := canvasTopY - config.CanvasHeightMM
	headerTopY := topEdge
	footerBottomY := config.BottomMarginMM

	// Build column data.
	columns := make([]testColumn, config.GridColumns)
	colW := config.ColumnWidth()
	for i := range config.GridColumns {
		leftX := contentLeftX + config.ColOffset(i)
		rightX := leftX + colW
		columns[i] = testColumn{
			LeftX:   leftX,
			RightX:  rightX,
			CenterX: (leftX + rightX) / 2,
			Number:  i + 1,
		}
	}

	// Build baseline Y positions.
	var baselineYs []float64
	for y := canvasBottomY; y <= canvasTopY; y += config.BaselineUnitMM {
		baselineYs = append(baselineYs, y)
	}

	// Build format samples.
	formatNames := []string{"1_fullscreen", "2_portrait", "4_landscape", "2l_1p", "1p_2l"}
	formats := make([]testFormat, 0, len(formatNames))
	for _, name := range formatNames {
		slots := FormatSlotsGrid(name, config)
		testSlots := make([]testFormatSlot, 0, len(slots))
		for i, s := range slots {
			// Convert canvas-relative to page coords.
			x := contentLeftX + s.X
			y := canvasTopY - s.Y - s.H
			color := slotColors[i%len(slotColors)]
			testSlots = append(testSlots, testFormatSlot{
				X: x, Y: y, W: s.W, H: s.H,
				CX: x + s.W/2, CY: y + s.H/2,
				Color: color,
			})
		}
		formats = append(formats, testFormat{
			Name:      name,
			SlotCount: len(slots),
			Slots:     testSlots,
		})
	}

	// Gutter-safe visualization (recto: binding on left).
	gutterSafeRightX := contentLeftX + config.GutterSafeMM
	// Sample marker in top-right of a hypothetical 2_portrait slot 1 (outside edge for recto).
	sampleSlotRightX := contentRightX
	sampleSlotTopY := canvasTopY
	markerSize := config.BaselineUnitMM
	markerInset := markerSize / 2.0
	sampleMarkerX := sampleSlotRightX - markerInset - markerSize
	sampleMarkerY := sampleSlotTopY - markerInset - markerSize

	return testPageData{
		Columns:            columns,
		ContentLeftX:       contentLeftX,
		ContentRightX:      contentRightX,
		ContentW:           contentW,
		ColumnW:            colW,
		GutterW:            config.ColumnGutterMM,
		CanvasTopY:         canvasTopY,
		CanvasBottomY:      canvasBottomY,
		HeaderTopY:         headerTopY,
		FooterBottomY:      footerBottomY,
		BaselineUnit:       config.BaselineUnitMM,
		BaselineYs:         baselineYs,
		Formats:            formats,
		GutterSafeMM:       config.GutterSafeMM,
		GutterSafeRightX:   gutterSafeRightX,
		SampleMarkerX:      sampleMarkerX,
		SampleMarkerY:      sampleMarkerY,
		SampleMarkerCX:     sampleMarkerX + markerSize/2,
		SampleMarkerCY:     sampleMarkerY + markerSize/2,
		SampleMarkerLabelX: sampleMarkerX + markerSize + 2,
	}
}
