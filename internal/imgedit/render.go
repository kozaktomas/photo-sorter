package imgedit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // register PNG decoder for non-JPEG originals
	"os"

	_ "golang.org/x/image/webp" // register WebP decoder

	"github.com/kozaktomas/photo-sorter/internal/imgconvert"
)

// ErrUnsupportedFormatNoDecoder is returned by DecodeAndApply when the
// source file is in HEIC or RAW format but the host is missing the
// corresponding external converter binary (heif-convert / dcraw). The web
// handler maps this to 503 with a clear "install heif-convert/dcraw"
// message.
var ErrUnsupportedFormatNoDecoder = errors.New(
	"imgedit: format requires heif-convert/dcraw which is not installed")

// DecodeAndApply opens the file at srcPath, decodes the pixels (going
// through imgconvert.EnsureDecodable for HEIC/RAW), applies the supplied
// orientation tag, then applies the supplied edits in order
// (crop → rotate → brightness → contrast).
//
// orientation is the photo's EXIF orientation tag (1-8); applyOrientation
// rotates/flips the pixel data to match the display orientation before the
// edit operations run. Pass 0 or 1 to skip orientation handling.
//
// If the converter binary needed for the format is missing the function
// returns ErrUnsupportedFormatNoDecoder. The caller is expected to release
// the returned image by simply dropping the reference.
func DecodeAndApply(
	ctx context.Context, srcPath string, orientation int, edits PhotoEdits,
) (image.Image, error) {
	decodedPath, cleanup, err := imgconvert.EnsureDecodable(ctx, srcPath)
	if err != nil {
		if errors.Is(err, imgconvert.ErrConverterMissing) {
			return nil, ErrUnsupportedFormatNoDecoder
		}
		return nil, fmt.Errorf("imgedit: ensure decodable: %w", err)
	}
	defer cleanup()

	f, err := os.Open(decodedPath) //nolint:gosec // path comes from imgconvert, already validated
	if err != nil {
		return nil, fmt.Errorf("imgedit: open: %w", err)
	}
	defer func() { _ = f.Close() }()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("imgedit: decode: %w", err)
	}

	oriented := applyEXIFOrientation(img, orientation)
	return ApplyEdits(oriented, edits), nil
}

// EncodeJPEG returns img encoded as JPEG at the given quality (1-100).
func EncodeJPEG(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("imgedit: encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}

// applyEXIFOrientation rotates/flips img according to the EXIF orientation
// tag. Values <= 1 or > 8 are treated as a no-op (orientation 1 is "normal"
// and means no transform). Mirrors thumb.applyOrientation but lives here
// so the imgedit package is self-contained.
func applyEXIFOrientation(img image.Image, orientation int) image.Image {
	if orientation <= 1 || orientation > 8 {
		return img
	}
	bounds := img.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()

	dstW, dstH := srcW, srcH
	switch orientation {
	case 5, 6, 7, 8:
		dstW, dstH = srcH, srcW
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for y := range dstH {
		for x := range dstW {
			sx, sy := mapEXIFOrientation(orientation, x, y, srcW, srcH)
			dst.Set(x, y, img.At(bounds.Min.X+sx, bounds.Min.Y+sy))
		}
	}
	return dst
}

// mapEXIFOrientation returns the source coordinate for destination (x, y)
// under the given EXIF orientation. Identical to the helper in
// internal/thumb but reproduced here to avoid an internal import cycle
// (thumb already depends on storage).
func mapEXIFOrientation(orientation, x, y, srcW, srcH int) (sx, sy int) {
	switch orientation {
	case 2: // Mirror horizontal.
		return srcW - 1 - x, y
	case 3: // Rotate 180.
		return srcW - 1 - x, srcH - 1 - y
	case 4: // Mirror vertical.
		return x, srcH - 1 - y
	case 5: // Transpose.
		return y, x
	case 6: // Rotate 90 CW.
		return y, srcH - 1 - x
	case 7: // Transverse.
		return srcW - 1 - y, srcH - 1 - x
	case 8: // Rotate 270 CW.
		return srcW - 1 - y, x
	}
	return x, y
}
