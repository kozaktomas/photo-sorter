package thumb

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"os"

	"golang.org/x/image/draw"
)

// decodeAndOrient opens src.Path, decodes the image via the registered
// image.Decode set (JPEG/PNG/WebP), and applies EXIF orientation so the
// returned image is in display orientation.
func decodeAndOrient(src Source) (image.Image, error) {
	f, err := os.Open(src.Path) // #nosec G304 -- callers supply trusted paths from the storage layer
	if err != nil {
		return nil, fmt.Errorf("thumb: open %q: %w", src.Path, err)
	}
	defer func() { _ = f.Close() }()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("thumb: decode %q: %w", src.Path, err)
	}
	return applyOrientation(img, src.Orientation), nil
}

// applyOrientation rotates and/or flips img according to the EXIF
// orientation value. Orientations 5-8 swap the output width and height.
// Values <= 1 or > 8 are treated as a no-op and img is returned as is.
func applyOrientation(img image.Image, orientation int) image.Image {
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
			sx, sy := mapOrientation(orientation, x, y, srcW, srcH)
			dst.Set(x, y, img.At(bounds.Min.X+sx, bounds.Min.Y+sy))
		}
	}
	return dst
}

// mapOrientation returns the source pixel coordinate that should appear
// at destination (x, y) after applying the given EXIF orientation
// transform to a srcW × srcH image. Behaviour is undefined for
// orientation values outside 2-8; applyOrientation handles the no-op
// cases before calling this.
func mapOrientation(orientation, x, y, srcW, srcH int) (sx, sy int) {
	switch orientation {
	case 2: // Mirror horizontal.
		return srcW - 1 - x, y
	case 3: // Rotate 180.
		return srcW - 1 - x, srcH - 1 - y
	case 4: // Mirror vertical.
		return x, srcH - 1 - y
	case 5: // Transpose (mirror horizontal + rotate 270 CW).
		return y, x
	case 6: // Rotate 90 CW.
		return y, srcH - 1 - x
	case 7: // Transverse (mirror horizontal + rotate 90 CW).
		return srcW - 1 - y, srcH - 1 - x
	case 8: // Rotate 270 CW (= 90 CCW).
		return srcW - 1 - y, x
	}
	return x, y
}

// resizeFit returns an image whose longest side is at most maxSide,
// preserving the aspect ratio. Images that already fit within the bound
// are returned unchanged — no upscaling is performed.
func resizeFit(img image.Image, maxSide int) image.Image {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= maxSide && h <= maxSide {
		return img
	}
	var newW, newH int
	if w >= h {
		newW = maxSide
		newH = h * maxSide / w
	} else {
		newH = maxSide
		newW = w * maxSide / h
	}
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Src, nil)
	return dst
}

// resizeCropSquare center-crops img to the largest square that fits and
// resizes that crop to side × side using CatmullRom interpolation.
func resizeCropSquare(img image.Image, side int) image.Image {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	sq := min(w, h)
	x0 := bounds.Min.X + (w-sq)/2
	y0 := bounds.Min.Y + (h-sq)/2
	cropRect := image.Rect(x0, y0, x0+sq, y0+sq)
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, cropRect, draw.Src, nil)
	return dst
}

// encodeJPEG encodes img as a JPEG byte slice at the given quality.
func encodeJPEG(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}
