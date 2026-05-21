package cmd

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
)

// originalsSummary aggregates the totals reported at the end of a tar run.
type originalsSummary struct {
	fileCount  int
	totalBytes int64
}

// errSkipEntry signals that processWalkEntry decided to silently skip an
// entry (e.g. an irregular non-symlink such as a FIFO or socket) and that
// the caller should treat the walk as continuing normally.
var errSkipEntry = errors.New("backup: skip entry")

// archiveOriginals streams the originals tree into a compressed tar archive
// at outputPath. Only regular files (or symlinks resolving to regular files)
// are included. Header names are stored relative to the originals root.
//
// Progress is logged every progressEvery files; a final summary line is
// always written.
func archiveOriginals(
	ctx context.Context,
	originalsRoot, outputPath string,
	comp backupCompressor,
	progressEvery int,
) (originalsSummary, error) {
	var summary originalsSummary

	rootInfo, err := os.Stat(originalsRoot)
	if err != nil {
		return summary, fmt.Errorf("stat originals root %s: %w", originalsRoot, err)
	}
	if !rootInfo.IsDir() {
		return summary, fmt.Errorf("originals root %s is not a directory", originalsRoot)
	}

	out, err := os.Create(outputPath) //nolint:gosec // path is constructed from CLI flags by the caller
	if err != nil {
		return summary, fmt.Errorf("creating archive file: %w", err)
	}
	defer out.Close()

	cw, err := newCompressingWriter(out, comp)
	if err != nil {
		return summary, err
	}
	tw := tar.NewWriter(cw)

	fmt.Printf("Archiving originals from %s ...\n", originalsRoot)
	walkErr := walkOriginalsTree(ctx, tw, originalsRoot, progressEvery, &summary)

	if err := tw.Close(); err != nil && walkErr == nil {
		walkErr = fmt.Errorf("closing tar writer: %w", err)
	}
	if err := cw.Close(); err != nil && walkErr == nil {
		walkErr = fmt.Errorf("closing compressor: %w", err)
	}
	if walkErr != nil {
		return summary, walkErr
	}

	fmt.Printf("Originals archive complete: %d files, %d bytes\n", summary.fileCount, summary.totalBytes)
	return summary, nil
}

// walkOriginalsTree walks the originals root and writes each regular file (or
// regular-file symlink target) into tw. The caller is responsible for closing
// tw afterwards.
func walkOriginalsTree(
	ctx context.Context,
	tw *tar.Writer,
	root string,
	progressEvery int,
	summary *originalsSummary,
) error {
	walkFn := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("walking %s: %w", path, err)
		}
		if cancelErr := ctx.Err(); cancelErr != nil {
			return fmt.Errorf("aborting walk: %w", cancelErr)
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return fmt.Errorf("rel %s: %w", path, relErr)
		}
		if rel == "." {
			return nil
		}
		return processWalkEntry(tw, path, rel, info, summary, progressEvery)
	}
	if err := filepath.Walk(root, walkFn); err != nil {
		return fmt.Errorf("filepath.Walk: %w", err)
	}
	return nil
}

// processWalkEntry decides what to do with a single Walk entry: skip
// directories and non-regular files (with symlink-to-file resolution), then
// archive the result.
func processWalkEntry(
	tw *tar.Writer,
	absPath, relPath string,
	info os.FileInfo,
	summary *originalsSummary,
	progressEvery int,
) error {
	if info.IsDir() {
		return nil
	}
	resolvedInfo, err := resolveRegularFile(absPath, info)
	if errors.Is(err, errSkipEntry) {
		return nil
	}
	if err != nil {
		// Symlink to non-regular target or stat error: skip with a warning.
		fmt.Fprintf(os.Stderr, "skipping %s: %v\n", relPath, err)
		return nil
	}
	if err := archiveRegularFile(tw, absPath, relPath, resolvedInfo); err != nil {
		return err
	}
	summary.fileCount++
	summary.totalBytes += resolvedInfo.Size()
	if progressEvery > 0 && summary.fileCount%progressEvery == 0 {
		fmt.Printf("  ... %d files archived\n", summary.fileCount)
	}
	return nil
}

// resolveRegularFile returns FileInfo for the target of a regular file or
// regular-file symlink. errSkipEntry signals that the entry is neither a
// regular file nor a symlink and should be silently skipped by the caller.
func resolveRegularFile(absPath string, info os.FileInfo) (os.FileInfo, error) {
	mode := info.Mode()
	if mode.IsRegular() {
		return info, nil
	}
	if mode&os.ModeSymlink == 0 {
		return nil, errSkipEntry
	}
	target, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("resolving symlink: %w", err)
	}
	if !target.Mode().IsRegular() {
		return nil, errors.New("symlink target is not a regular file")
	}
	return target, nil
}

// archiveRegularFile writes a single tar header and the file body into tw.
// relPath is used as the tar Header.Name; absPath is read for the contents.
func archiveRegularFile(tw *tar.Writer, absPath, relPath string, info os.FileInfo) error {
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("building tar header for %s: %w", relPath, err)
	}
	header.Name = filepath.ToSlash(relPath)
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("writing tar header for %s: %w", relPath, err)
	}
	f, err := os.Open(absPath) //nolint:gosec // path comes from filepath.Walk inside the configured originals root
	if err != nil {
		return fmt.Errorf("opening %s: %w", absPath, err)
	}
	defer f.Close()
	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("copying %s to tar: %w", absPath, err)
	}
	return nil
}

// newCompressingWriter wraps w with the requested compressor.
func newCompressingWriter(w io.Writer, comp backupCompressor) (io.WriteCloser, error) {
	switch comp {
	case compressorGzip:
		return gzip.NewWriter(w), nil
	case compressorZstd:
		zw, err := zstd.NewWriter(w)
		if err != nil {
			return nil, fmt.Errorf("creating zstd writer: %w", err)
		}
		return zw, nil
	default:
		return nil, fmt.Errorf("unknown compressor %q", comp)
	}
}
