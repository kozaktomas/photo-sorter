package facematch

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// RemoveDiacritics removes diacritical marks from a string (e.g., "Jiří" -> "Jiri").
func RemoveDiacritics(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	result, _, _ := transform.String(t, s)
	return result
}

// NormalizePersonName normalizes a name for comparison (lowercase, no diacritics, spaces for dashes).
func NormalizePersonName(name string) string {
	name = RemoveDiacritics(name)
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "-", " ")
	return name
}
