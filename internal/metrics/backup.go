package metrics

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BackupScanInterval is the default cadence for the backup freshness scan.
// The systemd backup timer fires daily, so checking every 10 minutes is
// more than enough to catch a missed run within an alert's evaluation
// window without burning extra IO.
const BackupScanInterval = 10 * time.Minute

// backupDirPrefix mirrors cmd/backup.go's naming scheme. Kept as a copy so
// the metrics package does not import cmd/.
const backupDirPrefix = "photo-sorter-"

// backupMetadata is the subset of metadata.json the freshness collector
// cares about. Defined locally to avoid coupling to cmd/backup.go's full
// struct.
type backupMetadata struct {
	CreatedAt time.Time `json:"created_at"`
}

// StartBackupWatcher periodically scans backupRoot for finished backup
// directories and publishes the most recent created_at timestamp as
// photo_sorter_last_backup_timestamp_seconds. An empty backupRoot leaves
// the metric at 0, which the alert rules treat as "backups not wired up".
//
// The watcher stops when ctx is cancelled. Scan failures are logged but
// never crash the goroutine — a missing or unmounted backup directory
// must not take the server down.
func (r *Registry) StartBackupWatcher(ctx context.Context, backupRoot string, interval time.Duration) {
	if backupRoot == "" {
		return
	}
	if interval <= 0 {
		interval = BackupScanInterval
	}
	go r.runBackupWatcher(ctx, backupRoot, interval)
}

// runBackupWatcher is the goroutine body. Performs one immediate scan so
// /metrics reports a real value on first scrape, then ticks.
func (r *Registry) runBackupWatcher(ctx context.Context, backupRoot string, interval time.Duration) {
	r.scanBackups(backupRoot)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.scanBackups(backupRoot)
		}
	}
}

// scanBackups walks backupRoot one level deep, reads metadata.json from
// each finished backup directory, and records the latest created_at into
// the gauge. Directories whose metadata cannot be parsed are skipped.
func (r *Registry) scanBackups(backupRoot string) {
	latest, err := findLatestBackup(backupRoot)
	if err != nil {
		log.Printf("metrics: backup scan failed (%s): %v", backupRoot, err)
		return
	}
	r.SetLastBackupTimestamp(latest)
}

// findLatestBackup walks backupRoot and returns the highest created_at
// among the metadata.json files it finds. The zero Time is returned when
// no parseable backup is present (so the gauge stays at 0).
func findLatestBackup(backupRoot string) (time.Time, error) {
	entries, err := os.ReadDir(backupRoot)
	if err != nil {
		return time.Time{}, err //nolint:wrapcheck // surfaced verbatim to the log line
	}
	var latest time.Time
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), backupDirPrefix) {
			continue
		}
		ts := readBackupMetadata(filepath.Join(backupRoot, e.Name(), "metadata.json"))
		if ts.After(latest) {
			latest = ts
		}
	}
	return latest, nil
}

// readBackupMetadata returns the created_at timestamp inside metadata.json,
// or the zero Time when the file is missing or malformed. The zero return
// is the signal for "skip this directory" — distinguishing real errors from
// half-written backups is out of scope for a metric refresh.
func readBackupMetadata(path string) time.Time {
	data, err := os.ReadFile(path) //nolint:gosec // path constrained to backupRoot/<dir>/metadata.json
	if err != nil {
		return time.Time{}
	}
	var meta backupMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return time.Time{}
	}
	return meta.CreatedAt
}
