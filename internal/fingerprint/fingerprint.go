package fingerprint

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"math"
	"sort"

	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
)

// HashResult contains computed perceptual hashes for an image.
type HashResult struct {
	PHash     string `json:"phash"` // 64-bit perceptual hash as hex string
	DHash     string `json:"dhash"` // 64-bit difference hash as hex string
	PHashBits uint64 `json:"-"`     // Raw pHash for comparison
	DHashBits uint64 `json:"-"`     // Raw dHash for comparison
}

// ComputeHashes computes both pHash and dHash for an image.
func ComputeHashes(imageData []byte) (*HashResult, error) {
	img, _, err := image.Decode(bytes.NewReader(imageData))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	pHash := computePHash(img)
	dHash := computeDHash(img)

	return &HashResult{
		PHash:     fmt.Sprintf("%016x", pHash),
		DHash:     fmt.Sprintf("%016x", dHash),
		PHashBits: pHash,
		DHashBits: dHash,
	}, nil
}

// HammingDistance computes the Hamming distance between two 64-bit hashes.
func HammingDistance(hash1, hash2 uint64) int {
	xor := hash1 ^ hash2
	distance := 0
	for xor != 0 {
		distance++
		xor &= xor - 1 // Clear lowest set bit
	}
	return distance
}

// Similar returns true if two hashes are within the given threshold.
// A threshold of 10 is typically used for near-duplicate detection.
func Similar(hash1, hash2 uint64, threshold int) bool {
	return HammingDistance(hash1, hash2) <= threshold
}

// computePHash computes a 64-bit perceptual hash using DCT.
func computePHash(img image.Image) uint64 {
	// 1. Resize to 32x32 for DCT processing
	resized := resizeImage(img, 32, 32)

	// 2. Convert to grayscale
	gray := toGrayscale(resized)

	// 3. Compute 32x32 DCT (Discrete Cosine Transform)
	dct := computeDCT(gray)

	// 4. Extract top-left 8x8 DCT coefficients (low frequencies)
	//    excluding DC component (0,0)
	lowFreq := make([]float64, 64)
	idx := 0
	for u := range 8 {
		for v := range 8 {
			if u == 0 && v == 0 {
				continue // Skip DC component
			}
			if idx < 64 {
				lowFreq[idx] = dct[u][v]
				idx++
			}
		}
	}
	// Fill remaining with the last few coefficients.
	for ; idx < 64; idx++ {
		lowFreq[idx] = dct[idx/8][idx%8]
	}

	// 5. Compute median of the 64 values
	median := computeMedian(lowFreq)

	// 6. Generate hash: 1 if value > median, 0 otherwise
	var hash uint64
	for i := range 64 {
		if lowFreq[i] > median {
			hash |= 1 << (63 - i)
		}
	}

	return hash
}

// computeDHash computes a 64-bit difference hash.
func computeDHash(img image.Image) uint64 {
	// 1. Resize to 9x8 (we need 9 columns for 8 differences)
	resized := resizeImage(img, 9, 8)

	// 2. Convert to grayscale
	gray := toGrayscale(resized)

	// 3. Compare adjacent pixels horizontally
	//    Each row: compare pixel[x] vs pixel[x+1]
	//    8 rows * 8 comparisons = 64 bits
	var hash uint64
	bit := 63
	for y := range 8 {
		for x := range 8 {
			if gray[x][y] > gray[x+1][y] {
				hash |= 1 << bit
			}
			bit--
		}
	}

	return hash
}

// resizeImage scales an image to the specified dimensions.
func resizeImage(img image.Image, width, height int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.BiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	return dst
}

// ResizeImage resizes an image to fit within maxSize while keeping aspect ratio.
// Returns JPEG-encoded bytes.
func ResizeImage(data []byte, maxSize int) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Check if resizing is needed.
	if width <= maxSize && height <= maxSize {
		return data, nil
	}

	// Calculate new dimensions.
	var newWidth, newHeight int
	if width > height {
		newWidth = maxSize
		newHeight = int(float64(height) * float64(maxSize) / float64(width))
	} else {
		newHeight = maxSize
		newWidth = int(float64(width) * float64(maxSize) / float64(height))
	}

	// Create resized image.
	resized := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	draw.BiLinear.Scale(resized, resized.Bounds(), img, bounds, draw.Over, nil)

	// Encode as JPEG.
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: 85}); err != nil {
		return nil, fmt.Errorf("failed to encode resized image: %w", err)
	}

	return buf.Bytes(), nil
}

// toGrayscale converts an image to a 2D array of grayscale values (0-255).
func toGrayscale(img *image.RGBA) [][]float64 {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	gray := make([][]float64, width)
	for x := range width {
		gray[x] = make([]float64, height)
		for y := range height {
			r, g, b, _ := img.At(x, y).RGBA()
			// ITU-R BT.601 luma formula.
			luma := 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(b>>8)
			gray[x][y] = luma
		}
	}

	return gray
}

// computeDCT computes the Discrete Cosine Transform of a grayscale image.
func computeDCT(gray [][]float64) [][]float64 {
	size := len(gray)
	dct := make([][]float64, size)
	for i := range dct {
		dct[i] = make([]float64, size)
	}

	// Precompute cosine values for efficiency.
	cosTable := make([][]float64, size)
	for i := range cosTable {
		cosTable[i] = make([]float64, size)
		for j := range size {
			cosTable[i][j] = math.Cos(math.Pi * float64(i) * (2*float64(j) + 1) / (2 * float64(size)))
		}
	}

	// DCT-II formula.
	for u := range size {
		for v := range size {
			var sum float64
			for x := range size {
				for y := range size {
					sum += gray[x][y] * cosTable[u][x] * cosTable[v][y]
				}
			}
			dct[u][v] = sum
		}
	}

	return dct
}

// computeMedian returns the median value from a slice.
func computeMedian(values []float64) float64 {
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return sorted[n/2]
}
