package cmd

import (
	"fmt"
	"io"
	"os/exec"
)

// decoderBinaries lists the external converters the upload + EXIF
// pipelines depend on, paired with the user-visible warning that fires
// when the binary is missing from PATH. The order is the same as the
// order rendered to the operator on startup.
var decoderBinaries = []struct {
	name string
	warn string
}{
	{"dcraw", "startup: dcraw not found in PATH — RAW uploads (NEF/CR2/CR3/ARW/DNG/...) will fail"},
	{"heif-convert", "startup: heif-convert not found in PATH — HEIC/HEIF uploads will fail"},
	{"exiftool", "startup: exiftool not found in PATH — EXIF edits will not write XMP sidecars"},
}

// checkExternalDecoders verifies that the three external binaries the
// upload + EXIF pipelines shell out to are available on PATH. Each
// missing binary produces a single WARN line on out; when all three are
// present a single "external decoders OK" line is printed. The function
// never returns an error — these dependencies are non-fatal because
// JPEG-only deployments do not need them.
func checkExternalDecoders(out io.Writer, lookPath func(string) (string, error)) {
	missing := 0
	for _, b := range decoderBinaries {
		if _, err := lookPath(b.name); err != nil {
			fmt.Fprintln(out, b.warn)
			missing++
		}
	}
	if missing == 0 {
		fmt.Fprintln(out, "startup: external decoders OK")
	}
}

// runDecoderCheck wires checkExternalDecoders to stdout and exec.LookPath
// for the real startup path.
func runDecoderCheck(out io.Writer) {
	checkExternalDecoders(out, exec.LookPath)
}
