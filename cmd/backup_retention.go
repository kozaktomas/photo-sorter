package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// pruneBackups deletes old backup directories under output, keeping the
// `keep` most-recent runs. It returns the number of directories removed.
//
// "Most recent" is determined by lexical order of the directory name; the
// timestamp layout used by the backup command sorts chronologically so this
// is equivalent to sorting by creation time.
//
// When keep is 0 or negative this function is a no-op and returns 0.
func pruneBackups(output string, keep int) (int, error) {
	if keep <= 0 {
		return 0, nil
	}

	dirs, err := listBackupDirs(output)
	if err != nil {
		return 0, err
	}
	if len(dirs) <= keep {
		return 0, nil
	}

	// dirs is sorted ascending (oldest first); the tail of length `keep`
	// is what we want to retain.
	toRemove := dirs[:len(dirs)-keep]
	pruned := 0
	for _, dir := range toRemove {
		full := filepath.Join(output, dir)
		if err := os.RemoveAll(full); err != nil {
			return pruned, fmt.Errorf("removing %s: %w", full, err)
		}
		pruned++
	}
	return pruned, nil
}

// listBackupDirs returns the immediate-child directory names under output
// that match the finished-backup prefix, sorted ascending.
//
// In-flight `.photo-sorter-<ts>.tmp/` directories are intentionally excluded
// because they start with a dot and therefore don't match the prefix.
func listBackupDirs(output string) ([]string, error) {
	entries, err := os.ReadDir(output)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", output, err)
	}
	var dirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, backupDirPrefix) {
			continue
		}
		dirs = append(dirs, name)
	}
	sort.Strings(dirs)
	return dirs, nil
}
