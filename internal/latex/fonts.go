package latex

import (
	"fmt"
	"os"
	"sort"
)

// FontEntry describes a font available for book typography.
type FontEntry struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Category    string `json:"category"` // "serif" or "sans-serif"
	LatexName   string `json:"-"`        // fontspec family name for LuaLaTeX
	// LatexFile / LatexItalicFile / LatexFontDir are for variable fonts
	// where fontspec cannot auto-detect bold. LatexDeclaration uses
	// fontspec's Path= option for direct filesystem lookup (no brackets,
	// no luaotfload cache needed). LatexFontDir is the subdirectory
	// under the font root (e.g. "truetype/crimsonpro").
	LatexFile       string `json:"-"`
	LatexItalicFile string `json:"-"`
	LatexFontDir    string `json:"-"`
	GoogleFamily    string `json:"google_family"` // URL-safe Google Fonts family
	GoogleSpec      string `json:"google_spec"`   // Google Fonts weight/style spec
}

// LatexDeclaration returns a complete fontspec command (\setmainfont or
// \setsansfont) configured for this font. fontRoot is the base directory
// where fonts are installed (e.g. "/usr/share/fonts"); use FindFontRoot
// to detect it. For static fonts it emits a simple family-name declaration.
// For variable fonts with LatexFile set, it uses fontspec's Path= option
// for direct filesystem lookup with explicit wght axis features.
// command must be the full LaTeX command including the leading backslash,
// e.g. `\setmainfont` or `\setsansfont`.
func (f FontEntry) LatexDeclaration(command, fontRoot string) string {
	if f.LatexFile != "" && f.LatexItalicFile != "" && f.LatexFontDir != "" {
		fontPath := fontRoot + "/" + f.LatexFontDir + "/"
		return fmt.Sprintf(
			"%s{%s}[\n"+
				"  Path=%s,\n"+
				"  Ligatures=TeX,\n"+
				"  ItalicFont=%s,\n"+
				"  BoldFont=%s,\n"+
				"  BoldItalicFont=%s,\n"+
				"  UprightFeatures={RawFeature={+axis={wght=400}}},\n"+
				"  ItalicFeatures={RawFeature={+axis={wght=400}}},\n"+
				"  BoldFeatures={RawFeature={+axis={wght=700}}},\n"+
				"  BoldItalicFeatures={RawFeature={+axis={wght=700}}},\n"+
				"]",
			command, f.LatexFile, fontPath, f.LatexItalicFile, f.LatexFile, f.LatexItalicFile,
		)
	}
	return fmt.Sprintf("%s{%s}[\n  Ligatures=TeX,\n]", command, f.LatexName)
}

// fontRootSearchPaths are checked in order to find the installed font root.
var fontRootSearchPaths = []string{
	"/usr/share/fonts",                    // Docker (Alpine)
	"/usr/local/share/fonts/photo-sorter", // dev (make install-fonts)
}

// FindFontRoot returns the first font root directory that contains the
// sentinel font file (CrimsonPro). Returns an empty string if not found.
func FindFontRoot() string {
	for _, root := range fontRootSearchPaths {
		sentinel := root + "/truetype/crimsonpro/CrimsonPro-wght.ttf"
		if _, err := os.Stat(sentinel); err == nil {
			return root
		}
	}
	return ""
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
	// DefaultHeadingColorBleed is the default bleed (mm) for colored heading boxes.
	DefaultHeadingColorBleed = 4.0
	// DefaultCaptionBadgeSize is the default size (mm) of footer caption
	// marker badges. Each badge is rendered as a fixed mm-square TikZ node so
	// every marker has identical outer dimensions regardless of which digit it
	// contains. The default matches the on-photo overlay badge (4×4 mm).
	DefaultCaptionBadgeSize = 4.0
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
		ID:              "lora",
		DisplayName:     "Lora",
		Category:        "serif",
		LatexName:       "Lora",
		LatexFile:       "Lora-wght.ttf",
		LatexItalicFile: "Lora-Italic-wght.ttf",
		LatexFontDir:    "truetype/lora",
		GoogleFamily:    "Lora",
		GoogleSpec:      "ital,wght@0,400;0,700;1,400;1,700",
	},
	"merriweather": {
		ID:              "merriweather",
		DisplayName:     "Merriweather",
		Category:        "serif",
		LatexName:       "Merriweather",
		LatexFile:       "Merriweather-opsz-wdth-wght.ttf",
		LatexItalicFile: "Merriweather-Italic-opsz-wdth-wght.ttf",
		LatexFontDir:    "truetype/merriweather",
		GoogleFamily:    "Merriweather",
		GoogleSpec:      "ital,wght@0,300;0,400;0,700;1,300;1,400;1,700",
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
		ID:              "crimson-pro",
		DisplayName:     "Crimson Pro",
		Category:        "serif",
		LatexName:       "Crimson Pro",
		LatexFile:       "CrimsonPro-wght.ttf",
		LatexItalicFile: "CrimsonPro-Italic-wght.ttf",
		LatexFontDir:    "truetype/crimsonpro",
		GoogleFamily:    "Crimson+Pro",
		GoogleSpec:      "ital,wght@0,400;0,600;0,700;1,400;1,600;1,700",
	},
	"source-serif": {
		ID:              "source-serif",
		DisplayName:     "Source Serif 4",
		Category:        "serif",
		LatexName:       "Source Serif 4",
		LatexFile:       "SourceSerif4-opsz-wght.ttf",
		LatexItalicFile: "SourceSerif4-Italic-opsz-wght.ttf",
		LatexFontDir:    "truetype/sourceserif4",
		GoogleFamily:    "Source+Serif+4",
		GoogleSpec:      "ital,wght@0,400;0,600;0,700;1,400;1,600;1,700",
	},
	"cormorant": {
		ID:              "cormorant",
		DisplayName:     "Cormorant Garamond",
		Category:        "serif",
		LatexName:       "Cormorant Garamond",
		LatexFile:       "CormorantGaramond-wght.ttf",
		LatexItalicFile: "CormorantGaramond-Italic-wght.ttf",
		LatexFontDir:    "truetype/cormorantgaramond",
		GoogleFamily:    "Cormorant+Garamond",
		GoogleSpec:      "ital,wght@0,400;0,600;0,700;1,400;1,600;1,700",
	},
	"bitter": {
		ID:              "bitter",
		DisplayName:     "Bitter",
		Category:        "serif",
		LatexName:       "Bitter",
		LatexFile:       "Bitter-wght.ttf",
		LatexItalicFile: "Bitter-Italic-wght.ttf",
		LatexFontDir:    "truetype/bitter",
		GoogleFamily:    "Bitter",
		GoogleSpec:      "ital,wght@0,400;0,600;0,700;1,400;1,600;1,700",
	},
	"gelasio": {
		ID:              "gelasio",
		DisplayName:     "Gelasio",
		Category:        "serif",
		LatexName:       "Gelasio",
		LatexFile:       "Gelasio-wght.ttf",
		LatexItalicFile: "Gelasio-Italic-wght.ttf",
		LatexFontDir:    "truetype/gelasio",
		GoogleFamily:    "Gelasio",
		GoogleSpec:      "ital,wght@0,400;0,500;0,600;0,700;1,400;1,500;1,600;1,700",
	},
	"bookman-old-style": {
		ID:          "bookman-old-style",
		DisplayName: "Bookman Old Style",
		Category:    "serif",
		// Blocker: Bookman Old Style is a proprietary Microsoft font and cannot
		// be freely redistributed. The font is not installed in the Docker image,
		// so PDF export will fail when this font is selected until the TTF files
		// are obtained from a licensed source and added to fonts/. Use the
		// "urw-bookman" entry below for a free Bookman alternative.
		LatexName: "Bookman Old Style",
		// Libre Baskerville is used as the Google Fonts fallback for the web
		// preview because Bookman Old Style is not available on Google Fonts.
		GoogleFamily: "Libre+Baskerville",
		GoogleSpec:   "ital,wght@0,400;0,700;1,400",
	},
	"urw-bookman": {
		ID:          "urw-bookman",
		DisplayName: "URW Bookman",
		Category:    "serif",
		LatexName:   "URW Bookman",
		// Libre Baskerville is used as the Google Fonts fallback for the web
		// preview because URW Bookman is not available on Google Fonts.
		GoogleFamily: "Libre+Baskerville",
		GoogleSpec:   "ital,wght@0,400;0,700;1,400",
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
		ID:              "nunito-sans",
		DisplayName:     "Nunito Sans",
		Category:        "sans-serif",
		LatexName:       "Nunito Sans",
		LatexFile:       "NunitoSans-YTLC-opsz-wdth-wght.ttf",
		LatexItalicFile: "NunitoSans-Italic-YTLC-opsz-wdth-wght.ttf",
		LatexFontDir:    "truetype/nunitosans",
		GoogleFamily:    "Nunito+Sans",
		GoogleSpec:      "ital,wght@0,400;0,600;0,700;1,400;1,600;1,700",
	},
	"raleway": {
		ID:              "raleway",
		DisplayName:     "Raleway",
		Category:        "sans-serif",
		LatexName:       "Raleway",
		LatexFile:       "Raleway-wght.ttf",
		LatexItalicFile: "Raleway-Italic-wght.ttf",
		LatexFontDir:    "truetype/raleway",
		GoogleFamily:    "Raleway",
		GoogleSpec:      "ital,wght@0,400;0,600;0,700;1,400;1,600;1,700",
	},
	"montserrat": {
		ID:              "montserrat",
		DisplayName:     "Montserrat",
		Category:        "sans-serif",
		LatexName:       "Montserrat",
		LatexFile:       "Montserrat-wght.ttf",
		LatexItalicFile: "Montserrat-Italic-wght.ttf",
		LatexFontDir:    "truetype/montserrat",
		GoogleFamily:    "Montserrat",
		GoogleSpec:      "ital,wght@0,400;0,600;0,700;1,400;1,600;1,700",
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
