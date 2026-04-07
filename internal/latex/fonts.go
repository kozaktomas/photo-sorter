package latex

import "sort"

// FontEntry describes a font available for book typography.
type FontEntry struct {
	ID           string `json:"id"`
	DisplayName  string `json:"display_name"`
	Category     string `json:"category"`      // "serif" or "sans-serif"
	LatexName    string `json:"-"`             // fontspec name for LuaLaTeX
	GoogleFamily string `json:"google_family"` // URL-safe Google Fonts family
	GoogleSpec   string `json:"google_spec"`   // Google Fonts weight/style spec
}

const (
	// DefaultBodyFont is the default body text font ID.
	DefaultBodyFont = "pt-serif"
	// DefaultHeadingFont is the default heading font ID.
	DefaultHeadingFont = "source-sans-3"
	// DefaultBodyFontSize is the default body text size in pt.
	DefaultBodyFontSize = 11.0
	// DefaultBodyLineHeight is the default body line height in pt.
	DefaultBodyLineHeight = 15.0
	// DefaultH1FontSize is the default H1 heading size in pt.
	DefaultH1FontSize = 18.0
	// DefaultH2FontSize is the default H2 heading size in pt.
	DefaultH2FontSize = 13.0
	// DefaultCaptionOpacity is the default caption text opacity (0.0-1.0).
	DefaultCaptionOpacity = 0.85
	// DefaultCaptionFontSize is the default caption font size in pt.
	DefaultCaptionFontSize = 9.0
)

// fontRegistry contains all available fonts indexed by ID.
var fontRegistry = map[string]FontEntry{
	// --- Serif ---
	"pt-serif": {
		ID:           "pt-serif",
		DisplayName:  "PT Serif",
		Category:     "serif",
		LatexName:    "PT Serif",
		GoogleFamily: "PT+Serif",
		GoogleSpec:   "ital,wght@0,400;0,700;1,400;1,700",
	},
	"libertinus": {
		ID:           "libertinus",
		DisplayName:  "Libertinus Serif",
		Category:     "serif",
		LatexName:    "Libertinus Serif",
		GoogleFamily: "Libertinus+Serif",
		GoogleSpec:   "ital,wght@0,400;0,600;0,700;1,400;1,700",
	},
	"eb-garamond": {
		ID:           "eb-garamond",
		DisplayName:  "EB Garamond",
		Category:     "serif",
		LatexName:    "EB Garamond",
		GoogleFamily: "EB+Garamond",
		GoogleSpec:   "ital,wght@0,400;0,600;0,700;1,400;1,600;1,700",
	},
	"lora": {
		ID:           "lora",
		DisplayName:  "Lora",
		Category:     "serif",
		LatexName:    "Lora",
		GoogleFamily: "Lora",
		GoogleSpec:   "ital,wght@0,400;0,700;1,400;1,700",
	},
	"merriweather": {
		ID:           "merriweather",
		DisplayName:  "Merriweather",
		Category:     "serif",
		LatexName:    "Merriweather",
		GoogleFamily: "Merriweather",
		GoogleSpec:   "ital,wght@0,300;0,400;0,700;1,300;1,400;1,700",
	},
	"noto-serif": {
		ID:           "noto-serif",
		DisplayName:  "Noto Serif",
		Category:     "serif",
		LatexName:    "Noto Serif",
		GoogleFamily: "Noto+Serif",
		GoogleSpec:   "ital,wght@0,400;0,700;1,400;1,700",
	},
	"crimson-pro": {
		ID:           "crimson-pro",
		DisplayName:  "Crimson Pro",
		Category:     "serif",
		LatexName:    "Crimson Pro",
		GoogleFamily: "Crimson+Pro",
		GoogleSpec:   "ital,wght@0,400;0,600;0,700;1,400;1,600;1,700",
	},
	"source-serif": {
		ID:           "source-serif",
		DisplayName:  "Source Serif 4",
		Category:     "serif",
		LatexName:    "Source Serif 4",
		GoogleFamily: "Source+Serif+4",
		GoogleSpec:   "ital,wght@0,400;0,600;0,700;1,400;1,600;1,700",
	},
	"cormorant": {
		ID:           "cormorant",
		DisplayName:  "Cormorant Garamond",
		Category:     "serif",
		LatexName:    "Cormorant Garamond",
		GoogleFamily: "Cormorant+Garamond",
		GoogleSpec:   "ital,wght@0,400;0,600;0,700;1,400;1,600;1,700",
	},
	"bitter": {
		ID:           "bitter",
		DisplayName:  "Bitter",
		Category:     "serif",
		LatexName:    "Bitter",
		GoogleFamily: "Bitter",
		GoogleSpec:   "ital,wght@0,400;0,600;0,700;1,400;1,600;1,700",
	},

	// --- Sans-serif ---
	"source-sans-3": {
		ID:           "source-sans-3",
		DisplayName:  "Source Sans 3",
		Category:     "sans-serif",
		LatexName:    "Source Sans 3",
		GoogleFamily: "Source+Sans+3",
		GoogleSpec:   "ital,wght@0,400;0,600;0,700;1,400;1,600;1,700",
	},
	"pt-sans": {
		ID:           "pt-sans",
		DisplayName:  "PT Sans",
		Category:     "sans-serif",
		LatexName:    "PT Sans",
		GoogleFamily: "PT+Sans",
		GoogleSpec:   "ital,wght@0,400;0,700;1,400;1,700",
	},
	"noto-sans": {
		ID:           "noto-sans",
		DisplayName:  "Noto Sans",
		Category:     "sans-serif",
		LatexName:    "Noto Sans",
		GoogleFamily: "Noto+Sans",
		GoogleSpec:   "ital,wght@0,400;0,700;1,400;1,700",
	},
	"open-sans": {
		ID:           "open-sans",
		DisplayName:  "Open Sans",
		Category:     "sans-serif",
		LatexName:    "Open Sans",
		GoogleFamily: "Open+Sans",
		GoogleSpec:   "ital,wght@0,400;0,600;0,700;1,400;1,600;1,700",
	},
	"lato": {
		ID:           "lato",
		DisplayName:  "Lato",
		Category:     "sans-serif",
		LatexName:    "Lato",
		GoogleFamily: "Lato",
		GoogleSpec:   "ital,wght@0,400;0,700;1,400;1,700",
	},
	"roboto": {
		ID:           "roboto",
		DisplayName:  "Roboto",
		Category:     "sans-serif",
		LatexName:    "Roboto",
		GoogleFamily: "Roboto",
		GoogleSpec:   "ital,wght@0,400;0,700;1,400;1,700",
	},
	"inter": {
		ID:           "inter",
		DisplayName:  "Inter",
		Category:     "sans-serif",
		LatexName:    "Inter",
		GoogleFamily: "Inter",
		GoogleSpec:   "ital,wght@0,400;0,600;0,700;1,400;1,600;1,700",
	},
	"fira-sans": {
		ID:           "fira-sans",
		DisplayName:  "Fira Sans",
		Category:     "sans-serif",
		LatexName:    "Fira Sans",
		GoogleFamily: "Fira+Sans",
		GoogleSpec:   "ital,wght@0,400;0,600;0,700;1,400;1,600;1,700",
	},
	"ibm-plex-sans": {
		ID:           "ibm-plex-sans",
		DisplayName:  "IBM Plex Sans",
		Category:     "sans-serif",
		LatexName:    "IBM Plex Sans",
		GoogleFamily: "IBM+Plex+Sans",
		GoogleSpec:   "ital,wght@0,400;0,600;0,700;1,400;1,600;1,700",
	},
	"nunito-sans": {
		ID:           "nunito-sans",
		DisplayName:  "Nunito Sans",
		Category:     "sans-serif",
		LatexName:    "Nunito Sans",
		GoogleFamily: "Nunito+Sans",
		GoogleSpec:   "ital,wght@0,400;0,600;0,700;1,400;1,600;1,700",
	},
}

// GetFont returns the font entry for the given ID and whether it was found.
func GetFont(id string) (FontEntry, bool) {
	f, ok := fontRegistry[id]
	return f, ok
}

// ValidateFont returns true if the font ID exists in the registry.
func ValidateFont(id string) bool {
	_, ok := fontRegistry[id]
	return ok
}

// AllFonts returns all available fonts sorted by category (serif first, then
// sans-serif) and alphabetically by display name within each category.
func AllFonts() []FontEntry {
	fonts := make([]FontEntry, 0, len(fontRegistry))
	for _, f := range fontRegistry {
		fonts = append(fonts, f)
	}
	sort.Slice(fonts, func(i, j int) bool {
		if fonts[i].Category != fonts[j].Category {
			// "serif" < "sans-serif" alphabetically is false,
			// but we want serif first, so reverse the comparison.
			return fonts[i].Category > fonts[j].Category
		}
		return fonts[i].DisplayName < fonts[j].DisplayName
	})
	return fonts
}
