// Package imgedit applies non-destructive photo edits (crop, rotate,
// brightness, contrast) to a decoded image. The package never touches
// files on disk — callers decode the original separately, hand the
// resulting image.Image plus a PhotoEdits struct to ApplyEdits, and
// re-encode the result wherever they need it (thumbnails, download
// stream, PDF export).
//
// Edit parameters are stored in the photo_edits table (see migration
// 041); this package is intentionally free of database / storage
// dependencies so it can be reused from any caller that already holds
// pixel data.
package imgedit

import (
	"image"
	"image/color"
	"math"

	"golang.org/x/image/draw"
)

// PhotoEdits holds the non-destructive edit parameters for a single
// photo. The zero value is "no edits".
//
// Crop coordinates are 0.0–1.0 relative to the rotated (display-oriented)
// image, with the origin at the top-left. A nil Crop pointer means "no
// crop". Rotation is restricted to 0/90/180/270 (degrees clockwise).
// Brightness and contrast are floats in [-1.0, 1.0] where 0 means
// "no change".
type PhotoEdits struct {
	Crop       *CropRect
	Rotation   int
	Brightness float64
	Contrast   float64
}

// CropRect describes a crop rectangle in relative coordinates against the
// rotated (display-oriented) image. All four fields are in [0.0, 1.0].
type CropRect struct {
	X float64
	Y float64
	W float64
	H float64
}

// IsZero reports whether the edits are all at their default values. The
// thumb/download pipeline uses this to short-circuit pixel work when a
// photo has a row in photo_edits that was cleared back to defaults.
func (e PhotoEdits) IsZero() bool {
	if e.Crop != nil {
		return false
	}
	if e.Rotation != 0 {
		return false
	}
	if e.Brightness != 0 {
		return false
	}
	if e.Contrast != 0 {
		return false
	}
	return true
}

// ApplyEdits returns a new image with the supplied edits applied to src.
// The order of operations is fixed: crop → rotate → brightness →
// contrast. When edits.IsZero() the input is returned unchanged so the
// caller pays nothing for the common no-op case.
//
// A nil src panics — callers must decode the original first. Crop
// rectangles that fall outside the image are clamped to the image
// bounds, so a rounding error on the frontend cannot crash the server.
func ApplyEdits(src image.Image, edits PhotoEdits) image.Image {
	if src == nil {
		panic("imgedit: ApplyEdits called with nil source")
	}
	if edits.IsZero() {
		return src
	}
	out := src
	if edits.Crop != nil {
		out = applyCrop(out, *edits.Crop)
	}
	if edits.Rotation != 0 {
		out = applyRotation(out, edits.Rotation)
	}
	if edits.Brightness != 0 || edits.Contrast != 0 {
		out = applyBrightnessContrast(out, edits.Brightness, edits.Contrast)
	}
	return out
}

// applyCrop returns a sub-image bounded by rect. Coordinates are clamped
// to the image bounds so a slightly-out-of-range frontend selection still
// produces a usable crop instead of an error.
func applyCrop(src image.Image, rect CropRect) image.Image {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	x := clamp01(rect.X)
	y := clamp01(rect.Y)
	cw := clamp01(rect.W)
	ch := clamp01(rect.H)
	if cw <= 0 || ch <= 0 {
		return src
	}
	if x+cw > 1.0 {
		cw = 1.0 - x
	}
	if y+ch > 1.0 {
		ch = 1.0 - y
	}

	x0 := bounds.Min.X + int(math.Round(x*float64(w)))
	y0 := bounds.Min.Y + int(math.Round(y*float64(h)))
	x1 := x0 + int(math.Round(cw*float64(w)))
	y1 := y0 + int(math.Round(ch*float64(h)))
	if x1 <= x0 || y1 <= y0 {
		return src
	}
	if x1 > bounds.Max.X {
		x1 = bounds.Max.X
	}
	if y1 > bounds.Max.Y {
		y1 = bounds.Max.Y
	}

	cropRect := image.Rect(x0, y0, x1, y1)
	dst := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	draw.Draw(dst, dst.Bounds(), src, cropRect.Min, draw.Src)
	return dst
}

// applyRotation rotates src clockwise by degrees (must be 0/90/180/270).
// Any other value is treated as 0 and the input is returned untouched.
func applyRotation(src image.Image, degrees int) image.Image {
	switch degrees {
	case 0:
		return src
	case 90:
		return rotate90(src)
	case 180:
		return rotate180(src)
	case 270:
		return rotate270(src)
	default:
		return src
	}
}

// rotate90 returns src rotated 90 degrees clockwise.
func rotate90(src image.Image) image.Image {
	b := src.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, srcH, srcW))
	for y := range srcH {
		for x := range srcW {
			dst.Set(srcH-1-y, x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// rotate180 returns src rotated 180 degrees.
func rotate180(src image.Image) image.Image {
	b := src.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, srcW, srcH))
	for y := range srcH {
		for x := range srcW {
			dst.Set(srcW-1-x, srcH-1-y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// rotate270 returns src rotated 270 degrees clockwise (90 counter-clockwise).
func rotate270(src image.Image) image.Image {
	b := src.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, srcH, srcW))
	for y := range srcH {
		for x := range srcW {
			dst.Set(y, srcW-1-x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// applyBrightnessContrast applies brightness then contrast to src.
//
// Brightness shifts every channel by brightness*255. Contrast scales
// every channel around the midpoint (128) by a factor derived from the
// contrast parameter using the standard "(c+1)/(1-c)" curve so that
// contrast=0 is identity and contrast=±1 reaches the extremes.
//
// The output is always a fresh *image.RGBA so the caller can safely
// re-use the source.
func applyBrightnessContrast(src image.Image, brightness, contrast float64) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))

	brightShift := brightness * 255.0
	contrastFactor := contrastCurve(contrast)

	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			rr, gg, bb, aa := src.At(x, y).RGBA()
			// RGBA() returns 16-bit values; divide to 8-bit.
			r := float64(rr>>8) + brightShift
			g := float64(gg>>8) + brightShift
			bch := float64(bb>>8) + brightShift
			r = applyContrastChannel(r, contrastFactor)
			g = applyContrastChannel(g, contrastFactor)
			bch = applyContrastChannel(bch, contrastFactor)
			dst.SetRGBA(x-b.Min.X, y-b.Min.Y, color.RGBA{
				R: clampByte(r),
				G: clampByte(g),
				B: clampByte(bch),
				//nolint:gosec // RGBA() returns 16-bit values; >>8 always fits in uint8.
				A: uint8(aa >> 8),
			})
		}
	}
	return dst
}

// contrastCurve maps the user-facing [-1, 1] contrast slider to the
// multiplicative factor applied to each channel around the midpoint.
//
//   - contrast =  0   →  1.0  (no change)
//   - contrast =  1   → +∞    (clamped to a large finite value)
//   - contrast = -1   →  0    (collapse to mid-grey)
//
// The "(1+c)/(1-c)" form is the classic GIMP/Photoshop contrast curve.
func contrastCurve(c float64) float64 {
	if c >= 1 {
		c = 0.999
	}
	if c <= -1 {
		c = -0.999
	}
	return (1.0 + c) / (1.0 - c)
}

// applyContrastChannel scales v around the midpoint (128) by factor.
func applyContrastChannel(v, factor float64) float64 {
	return (v-128.0)*factor + 128.0
}

// clamp01 clamps v to [0, 1].
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// clampByte clamps v to [0, 255] and rounds to uint8.
func clampByte(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(math.Round(v))
}
