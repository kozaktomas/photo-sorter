// Package thumb generates JPEG thumbnails for photo originals in the
// PhotoPrism-compatible size set and writes them through the storage layer.
//
// The package handles JPEG, PNG, and WebP source inputs directly. HEIC and
// RAW originals are pre-decoded by a separate layer (heif-convert, dcraw)
// before being handed to this package.
package thumb

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder
	_ "image/png"  // register PNG decoder

	_ "golang.org/x/image/webp" // register WebP decoder

	"github.com/kozaktomas/photo-sorter/internal/storage"
)

// Source identifies the photo a thumbnail will be generated from.
type Source struct {
	// Path is the absolute path of the source image on disk.
	Path string
	// Orientation is the EXIF orientation tag value (1-8). Zero or any
	// value outside that range is treated as no rotation.
	Orientation int
}

// modeFit preserves aspect ratio while clamping the longest side; no
// upscaling is performed. modeCropSquare center-crops to a square then
// resizes to side × side.
const (
	modeFit        = "fit"
	modeCropSquare = "crop-square"
)

// sizeSpec describes a single named thumbnail size in the registry.
type sizeSpec struct {
	Max     int    // longest side (or square side for crop mode), in pixels
	Quality int    // JPEG quality, 1-100
	Mode    string // modeFit or modeCropSquare
}

// sizes is the read-only registry of PhotoPrism-compatible thumbnail sizes.
// Callers reference sizes by their string name (e.g. "fit_1920").
var sizes = map[string]sizeSpec{
	"fit_720":  {Max: 720, Quality: 90, Mode: modeFit},
	"fit_1280": {Max: 1280, Quality: 90, Mode: modeFit},
	"fit_1920": {Max: 1920, Quality: 90, Mode: modeFit},
	"fit_2560": {Max: 2560, Quality: 88, Mode: modeFit},
	"fit_3840": {Max: 3840, Quality: 88, Mode: modeFit},
	"fit_7680": {Max: 7680, Quality: 85, Mode: modeFit},
	"tile_50":  {Max: 50, Quality: 80, Mode: modeCropSquare},
	"tile_100": {Max: 100, Quality: 80, Mode: modeCropSquare},
	"tile_224": {Max: 224, Quality: 85, Mode: modeCropSquare},
	"tile_500": {Max: 500, Quality: 85, Mode: modeCropSquare},
}

// sizeOrder defines the deterministic iteration order for GenerateAll.
// Sizes progress from the largest "fit" thumbnail down to the smallest
// "tile" so callers that observe progress see big thumbnails complete
// first.
var sizeOrder = []string{
	"fit_7680",
	"fit_3840",
	"fit_2560",
	"fit_1920",
	"fit_1280",
	"fit_720",
	"tile_500",
	"tile_224",
	"tile_100",
	"tile_50",
}

// SizeNames returns the list of every thumbnail size name registered in
// the package, in the canonical sizeOrder. The returned slice is a fresh
// copy so callers can sort/filter it without mutating the registry.
func SizeNames() []string {
	out := make([]string, len(sizeOrder))
	copy(out, sizeOrder)
	return out
}

// IsValidSize reports whether name is a registered thumbnail size.
func IsValidSize(name string) bool {
	_, ok := sizes[name]
	return ok
}

// Generate decodes the source at src.Path, applies its EXIF orientation,
// resizes for sizeName, JPEG-encodes the result, and writes it through
// store. It returns the relative path of the thumbnail (the same value
// store.ThumbExists or store.AbsThumb would accept).
//
// If the thumbnail already exists in store the file is left untouched and
// the existing relative path is returned. An error is returned if
// sizeName is not in the registry or any decode/encode/IO step fails.
func Generate(src Source, sizeName string, store *storage.Storage, fileHash string) (string, error) {
	spec, ok := sizes[sizeName]
	if !ok {
		return "", fmt.Errorf("thumb: unknown size %q", sizeName)
	}
	rel, err := storage.ThumbRelPath(fileHash, sizeName)
	if err != nil {
		return "", fmt.Errorf("thumb: build path for %s: %w", sizeName, err)
	}
	if store.ThumbExists(rel) {
		return rel, nil
	}

	img, err := decodeAndOrient(src)
	if err != nil {
		return "", err
	}
	return writeThumb(img, spec, sizeName, rel, store)
}

// GenerateAll iterates every size in the registry in deterministic order.
// Sizes that already exist in store are skipped; missing sizes are
// generated sequentially (the source image is decoded once and reused).
// The returned map covers every registered size — both pre-existing and
// freshly generated — keyed by size name, valued by the relative path
// under the thumb cache root.
//
// If decoding the source or writing any single thumb fails the function
// returns immediately; thumbnails already written are kept on disk and
// will be picked up as "already exists" on the next call.
func GenerateAll(src Source, store *storage.Storage, fileHash string) (map[string]string, error) {
	return GenerateSizes(src, sizeOrder, store, fileHash)
}

// GenerateSizes generates the named subset of thumbnails for src. Sizes
// that already exist in store are skipped; missing sizes are generated
// after decoding the source image exactly once.
//
// sizeNames are processed in the order supplied by the caller. Each name
// must be a registered size or ErrUnknownSize wrapping the bad name is
// returned. The returned map covers every requested size — both
// pre-existing and freshly generated — keyed by size name, valued by the
// relative path under the thumb cache root.
//
// Pass an empty slice (or thumb.SizeNames()) to generate every size; the
// thin GenerateAll wrapper does the latter.
func GenerateSizes(
	src Source, sizeNames []string,
	store *storage.Storage, fileHash string,
) (map[string]string, error) {
	if len(sizeNames) == 0 {
		return map[string]string{}, nil
	}

	result := make(map[string]string, len(sizeNames))
	needed := make([]string, 0, len(sizeNames))

	for _, name := range sizeNames {
		if _, ok := sizes[name]; !ok {
			return nil, fmt.Errorf("thumb: unknown size %q", name)
		}
		rel, err := storage.ThumbRelPath(fileHash, name)
		if err != nil {
			return nil, fmt.Errorf("thumb: build path for %s: %w", name, err)
		}
		result[name] = rel
		if !store.ThumbExists(rel) {
			needed = append(needed, name)
		}
	}
	if len(needed) == 0 {
		return result, nil
	}

	img, err := decodeAndOrient(src)
	if err != nil {
		return nil, err
	}
	for _, name := range needed {
		if _, err := writeThumb(img, sizes[name], name, result[name], store); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// writeThumb resizes img according to spec, JPEG-encodes the result, and
// writes the bytes through store at rel. It returns rel on success so
// callers can ignore the intermediate variables.
func writeThumb(
	img image.Image,
	spec sizeSpec,
	sizeName, rel string,
	store *storage.Storage,
) (string, error) {
	var resized image.Image
	switch spec.Mode {
	case modeFit:
		resized = resizeFit(img, spec.Max)
	case modeCropSquare:
		resized = resizeCropSquare(img, spec.Max)
	default:
		return "", fmt.Errorf("thumb: invalid mode %q for size %q", spec.Mode, sizeName)
	}
	data, err := encodeJPEG(resized, spec.Quality)
	if err != nil {
		return "", fmt.Errorf("thumb: encode %s: %w", sizeName, err)
	}
	if _, err := store.WriteThumb(rel, bytes.NewReader(data)); err != nil {
		return "", fmt.Errorf("thumb: write %s: %w", sizeName, err)
	}
	return rel, nil
}
