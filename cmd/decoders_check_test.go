package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestCheckExternalDecoders_AllPresent(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	lookPath := func(name string) (string, error) {
		return "/fake/bin/" + name, nil
	}
	checkExternalDecoders(&buf, lookPath)
	out := buf.String()
	if !strings.Contains(out, "startup: external decoders OK") {
		t.Errorf("expected OK summary, got:\n%s", out)
	}
	for _, b := range decoderBinaries {
		if strings.Contains(out, b.warn) {
			t.Errorf("did not expect WARN for %s, got:\n%s", b.name, out)
		}
	}
}

func TestCheckExternalDecoders_OneMissing(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	missingErr := errors.New("not found")
	lookPath := func(name string) (string, error) {
		if name == "dcraw" {
			return "", missingErr
		}
		return "/fake/bin/" + name, nil
	}
	checkExternalDecoders(&buf, lookPath)
	out := buf.String()

	wantWarn := decoderBinaries[0].warn
	if !strings.Contains(out, wantWarn) {
		t.Errorf("expected WARN line %q, got:\n%s", wantWarn, out)
	}
	if strings.Contains(out, "startup: external decoders OK") {
		t.Errorf("did not expect OK summary when a binary is missing, got:\n%s", out)
	}
	// The two binaries that are present must not produce a WARN line.
	for _, b := range decoderBinaries[1:] {
		if strings.Contains(out, b.warn) {
			t.Errorf("did not expect WARN for present binary %s, got:\n%s", b.name, out)
		}
	}
}

func TestCheckExternalDecoders_AllMissing(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	lookPath := func(string) (string, error) { return "", errors.New("not found") }
	checkExternalDecoders(&buf, lookPath)
	out := buf.String()
	for _, b := range decoderBinaries {
		if !strings.Contains(out, b.warn) {
			t.Errorf("expected WARN line for %s, got:\n%s", b.name, out)
		}
	}
	if strings.Contains(out, "startup: external decoders OK") {
		t.Errorf("did not expect OK summary when all binaries are missing, got:\n%s", out)
	}
}
