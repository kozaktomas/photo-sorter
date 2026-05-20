package imgconvert

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// rawTimeout caps a single dcraw invocation. Half-size decoding of a
// 24-megapixel CR2 finishes in ~3 s on a fast machine and considerably
// longer on a Raspberry Pi; 60 s leaves room for the worst case while
// still detecting a runaway process.
const rawTimeout = 60 * time.Second

// rawJPEGQuality is the JPEG quality used when re-encoding the decoded
// PPM frame. 90 matches the high-tier fit_* thumbnail quality and avoids
// a noticeable second-generation compression artefact.
const rawJPEGQuality = 90

// ppmMaxval8 is the largest "maxval" header value that indicates an 8-bit
// per channel PPM (one byte per channel). Anything above is treated as
// 16-bit per channel (two bytes per channel, big-endian).
const ppmMaxval8 = 0xFF

// convertRAW invokes dcraw to decode the RAW file at srcPath, parses the
// PPM frame it writes to stdout, re-encodes the result as JPEG quality 90,
// and writes that JPEG to a temporary file. The returned cleanup function
// removes the temp JPEG and is safe to call multiple times.
//
// dcraw is invoked as `dcraw -c -w -h <src>`: -c writes to stdout, -w uses
// the camera white balance, -h decodes at half resolution (the upload
// pipeline only needs an image large enough for thumbnail generation, and
// full-size dcraw decoding is too slow for an interactive upload).
//
// If dcraw is not on PATH the returned error wraps ErrConverterMissing.
func convertRAW(ctx context.Context, srcPath string) (string, func(), error) {
	if _, err := exec.LookPath("dcraw"); err != nil {
		return "", nil, fmt.Errorf("%w: dcraw lookup: %w", ErrConverterMissing, err)
	}

	cctx, cancel := context.WithTimeout(ctx, rawTimeout)
	defer cancel()

	img, err := runDcraw(cctx, srcPath)
	if err != nil {
		return "", nil, err
	}
	return writeTempJPEG(img)
}

// runDcraw spawns dcraw with the standard `-c -w -h` flags, parses its
// stdout as a binary PPM frame, and returns the decoded image. Errors
// from the subprocess take precedence over decoder errors because the
// latter are typically caused by the former (e.g. dcraw printing an error
// message to stderr and exiting before writing any PPM bytes).
func runDcraw(ctx context.Context, srcPath string) (image.Image, error) {
	// #nosec G204 -- srcPath is the same caller-supplied path EnsureDecodable
	// stat'ed before dispatch; the other args are constant literals.
	cmd := exec.CommandContext(ctx, "dcraw", "-c", "-w", "-h", srcPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("imgconvert: pipe dcraw stdout: %w", err)
	}
	if startErr := cmd.Start(); startErr != nil {
		return nil, fmt.Errorf("imgconvert: start dcraw: %w", startErr)
	}

	img, decodeErr := decodePPM(stdout)
	// Drain any bytes left in the pipe so dcraw can exit cleanly even if
	// the PPM contained trailing data we didn't consume. Without this the
	// Wait() below could deadlock against a blocked write.
	_, _ = io.Copy(io.Discard, stdout)

	if waitErr := cmd.Wait(); waitErr != nil {
		return nil, fmt.Errorf("imgconvert: dcraw %s: %w (stderr: %s)",
			filepath.Base(srcPath), waitErr, stderr.String())
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("imgconvert: decode dcraw output: %w", decodeErr)
	}
	return img, nil
}

// writeTempJPEG JPEG-encodes img at rawJPEGQuality into a temp file
// named imgconvert-raw-*.jpg under os.TempDir() and returns the path plus
// a once-only cleanup function. If JPEG encoding fails the partial file
// is removed before the error is returned.
func writeTempJPEG(img image.Image) (string, func(), error) {
	tmp, err := os.CreateTemp("", "imgconvert-raw-*.jpg")
	if err != nil {
		return "", nil, fmt.Errorf("imgconvert: create temp jpeg: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := onceRemove(tmpPath)

	if encodeErr := jpeg.Encode(tmp, img, &jpeg.Options{Quality: rawJPEGQuality}); encodeErr != nil {
		_ = tmp.Close()
		cleanup()
		return "", nil, fmt.Errorf("imgconvert: encode jpeg: %w", encodeErr)
	}
	if closeErr := tmp.Close(); closeErr != nil {
		cleanup()
		return "", nil, fmt.Errorf("imgconvert: close jpeg: %w", closeErr)
	}
	return tmpPath, cleanup, nil
}

// decodePPM parses a binary PPM stream (the format dcraw writes to stdout
// with -c). Both 8-bit (maxval ≤ 255) and 16-bit (maxval ≤ 65535) variants
// are accepted; the latter is downsampled to 8 bits per channel by taking
// the high byte before being stored in the returned *image.RGBA.
func decodePPM(r io.Reader) (image.Image, error) {
	br := bufio.NewReader(r)
	magic, err := readToken(br)
	if err != nil {
		return nil, fmt.Errorf("read magic: %w", err)
	}
	if magic != "P6" {
		return nil, fmt.Errorf("unsupported PPM magic %q (want P6)", magic)
	}
	width, height, maxval, err := readPPMDims(br)
	if err != nil {
		return nil, err
	}
	// PPM specifies exactly one whitespace byte between maxval and the
	// binary pixel data.
	if _, err := br.ReadByte(); err != nil {
		return nil, fmt.Errorf("read header terminator: %w", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	if maxval <= ppmMaxval8 {
		return img, readPPM8(br, img, width, height)
	}
	return img, readPPM16(br, img, width, height)
}

// readPPMDims reads width, height, and maxval (in that order) from the
// PPM header. All three are required to be positive; maxval is clamped to
// the 16-bit ceiling that the format spec allows.
func readPPMDims(br *bufio.Reader) (width, height, maxval int, err error) {
	if width, err = readIntToken(br); err != nil {
		return 0, 0, 0, fmt.Errorf("read width: %w", err)
	}
	if height, err = readIntToken(br); err != nil {
		return 0, 0, 0, fmt.Errorf("read height: %w", err)
	}
	if maxval, err = readIntToken(br); err != nil {
		return 0, 0, 0, fmt.Errorf("read maxval: %w", err)
	}
	if width <= 0 || height <= 0 || maxval <= 0 || maxval > 0xFFFF {
		return 0, 0, 0, fmt.Errorf("invalid PPM header (w=%d h=%d max=%d)", width, height, maxval)
	}
	return width, height, maxval, nil
}

// readPPM8 fills img from an 8-bit (maxval ≤ 255) PPM pixel stream.
func readPPM8(r io.Reader, img *image.RGBA, width, height int) error {
	rowBytes := width * 3
	buf := make([]byte, rowBytes)
	for y := range height {
		if _, err := io.ReadFull(r, buf); err != nil {
			return fmt.Errorf("ppm row %d: %w", y, err)
		}
		for x := range width {
			img.SetRGBA(x, y, color.RGBA{
				R: buf[x*3],
				G: buf[x*3+1],
				B: buf[x*3+2],
				A: 0xFF,
			})
		}
	}
	return nil
}

// readPPM16 fills img from a 16-bit (256 ≤ maxval ≤ 65535) PPM pixel
// stream, downsampling each channel to 8 bits by taking the high byte.
func readPPM16(r io.Reader, img *image.RGBA, width, height int) error {
	rowBytes := width * 6
	buf := make([]byte, rowBytes)
	for y := range height {
		if _, err := io.ReadFull(r, buf); err != nil {
			return fmt.Errorf("ppm row %d: %w", y, err)
		}
		for x := range width {
			off := x * 6
			r16 := binary.BigEndian.Uint16(buf[off : off+2])
			g16 := binary.BigEndian.Uint16(buf[off+2 : off+4])
			b16 := binary.BigEndian.Uint16(buf[off+4 : off+6])
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(r16 >> 8),
				G: uint8(g16 >> 8),
				B: uint8(b16 >> 8),
				A: 0xFF,
			})
		}
	}
	return nil
}

// readToken reads a single whitespace-delimited token from r, skipping
// leading whitespace and PPM-style comments ("#" to end of line). Returns
// io.EOF if no token can be read.
func readToken(r *bufio.Reader) (string, error) {
	if err := skipSpacesAndComments(r); err != nil {
		return "", err
	}
	return readNonSpaceToken(r)
}

// skipSpacesAndComments advances r past any whitespace and "#"-prefixed
// PPM comments until the first non-whitespace, non-comment byte; that
// byte is then unread so the caller can consume it.
func skipSpacesAndComments(r *bufio.Reader) error {
	for {
		b, err := r.ReadByte()
		if err != nil {
			return fmt.Errorf("read byte: %w", err)
		}
		if b == '#' {
			if _, err := r.ReadBytes('\n'); err != nil {
				return fmt.Errorf("skip comment: %w", err)
			}
			continue
		}
		if !isPPMSpace(b) {
			if err := r.UnreadByte(); err != nil {
				return fmt.Errorf("unread byte: %w", err)
			}
			return nil
		}
	}
}

// readNonSpaceToken consumes bytes until the next whitespace or comment
// marker and returns them as a string. The terminating byte is unread so
// the caller's next readToken sees the correct position.
func readNonSpaceToken(r *bufio.Reader) (string, error) {
	var tok []byte
	for {
		b, err := r.ReadByte()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read byte: %w", err)
		}
		if isPPMSpace(b) || b == '#' {
			if unreadErr := r.UnreadByte(); unreadErr != nil {
				return "", fmt.Errorf("unread byte: %w", unreadErr)
			}
			break
		}
		tok = append(tok, b)
	}
	if len(tok) == 0 {
		return "", io.EOF
	}
	return string(tok), nil
}

// readIntToken reads a whitespace-delimited token and parses it as a
// positive decimal integer.
func readIntToken(r *bufio.Reader) (int, error) {
	tok, err := readToken(r)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(tok)
	if err != nil {
		return 0, fmt.Errorf("parse %q: %w", tok, err)
	}
	return n, nil
}

// isPPMSpace reports whether b is one of the four whitespace bytes the
// PPM grammar treats as token separators.
func isPPMSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}
