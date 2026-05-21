package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/kozaktomas/photo-sorter/internal/migrate"
)

var migrateRemapReferencesCmd = &cobra.Command{
	Use:   "migrate-remap-references",
	Short: "Rewrite native photo references using a photo-UID map",
	Long: `Rewrite every soft photo_uid reference in the native database using a
photo-UID map (as emitted by ` + "`migrate-from-photoprism --emit-photo-map`" + `).

The command is intended for operators who landed an older buggy version
of ` + "`migrate-from-photoprism`" + ` that wrote new generated UIDs into photos
instead of preserving the PhotoPrism UIDs. It rewrites embeddings,
faces, faces_processed, markers, album_photos, photo_labels,
photo_phashes, section_photos, and page_slots to point at the new UIDs
inside a single transaction.

Identity maps (every key == value) short-circuit before opening the
transaction; the command prints "nothing to remap" and exits 0.

Examples:
  # Dry-run against the file the migrator just wrote — prints would-be
  # row counts and orphan stats without touching the DB.
  photo-sorter migrate-remap-references --map /tmp/photo-map.json --dry-run

  # Apply remap, skip the interactive confirmation.
  photo-sorter migrate-remap-references --map /tmp/photo-map.json --yes`,
	RunE: runMigrateRemapReferences,
}

func init() {
	rootCmd.AddCommand(migrateRemapReferencesCmd)
	migrateRemapReferencesCmd.Flags().String(
		"map", "",
		"Path to the photo-UID map JSON file (required)")
	migrateRemapReferencesCmd.Flags().Bool(
		"dry-run", false,
		"Run the UPDATEs and roll back; report row counts without writing")
	migrateRemapReferencesCmd.Flags().Bool(
		yesAnswer, false,
		"Skip the interactive confirmation prompt")
	_ = migrateRemapReferencesCmd.MarkFlagRequired("map")
}

// yesAnswer is the long-form affirmative answer the confirm prompt
// accepts. Extracted as a const so goconst is happy.
const yesAnswer = "yes"

func runMigrateRemapReferences(cmd *cobra.Command, _ []string) error {
	mapPath := mustGetString(cmd, "map")
	dryRun := mustGetBool(cmd, "dry-run")
	skipConfirm := mustGetBool(cmd, yesAnswer)

	loaded, err := migrate.LoadPhotoMap(mapPath)
	if err != nil {
		return fmt.Errorf("load photo map: %w", err)
	}
	if loaded.IdentityMap() {
		fmt.Fprintln(cmd.OutOrStdout(),
			"nothing to remap; map is identity (every old UID equals its new UID)")
		return nil
	}

	pool, cleanup, err := openRemapPool()
	if err != nil {
		return err
	}
	defer cleanup()

	if !dryRun && !skipConfirm {
		ok, err := confirmRemap(cmd, loaded)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(cmd.OutOrStdout(), "aborted")
			return nil
		}
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	summary, err := migrate.RemapReferences(ctx, pool.DB(), &migrate.RemapOptions{
		Map:    loaded,
		DryRun: dryRun,
		Writer: cmd.OutOrStdout(),
	})
	if err != nil {
		return fmt.Errorf("remap-references: %w", err)
	}
	printRemapSummary(cmd, summary, dryRun)
	return nil
}

// openRemapPool initialises the global Postgres pool and returns it
// together with a cleanup that closes it. Keeping the open + cleanup
// dance out of runMigrateRemapReferences keeps the latter under the
// cyclomatic-complexity cap.
func openRemapPool() (*postgres.Pool, func(), error) {
	cfg := config.Load()
	if cfg.Database.URL == "" {
		return nil, nil, errors.New("DATABASE_URL is required")
	}
	if err := postgres.Initialize(&cfg.Database); err != nil {
		return nil, nil, fmt.Errorf("initialise postgres: %w", err)
	}
	pool := postgres.GetGlobalPool()
	return pool, func() { _ = pool.Close() }, nil
}

// confirmRemap reads a single y/N answer from stdin. Returning false
// aborts the run.
func confirmRemap(cmd *cobra.Command, loaded *migrate.PhotoMapJSON) (bool, error) {
	nonIdentity := 0
	for k, v := range loaded.PhotoUIDMap {
		if k != v {
			nonIdentity++
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(),
		"About to rewrite native photo references for %d (old -> new) pairs.\n"+
			"This touches embeddings, faces, faces_processed, markers, album_photos,\n"+
			"photo_labels, photo_phashes, section_photos, and page_slots — one\n"+
			"transaction, all-or-nothing.\n\nContinue? [y/N]: ",
		nonIdentity)
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == yesAnswer, nil
}

// printRemapSummary writes the per-table update and orphan counts in a
// stable order so the output is greppable by table.column.
func printRemapSummary(cmd *cobra.Command, summary *migrate.RemapSummary, dryRun bool) {
	out := cmd.OutOrStdout()
	if summary.Identity {
		fmt.Fprintln(out, "nothing to remap; map is identity")
		return
	}
	label := "Updated"
	if dryRun {
		label = "Would update (dry-run)"
	}
	fmt.Fprintf(out, "\n== %s ==\n", label)
	keys := sortedKeys(summary.Updated)
	for _, k := range keys {
		fmt.Fprintf(out, "  %-30s %d rows\n", k, summary.Updated[k])
	}
	fmt.Fprintln(out, "\n== Orphan audit (rows pointing at no photos.uid) ==")
	orphanKeys := sortedKeys(summary.Orphans)
	totalOrphans := int64(0)
	for _, k := range orphanKeys {
		n := summary.Orphans[k]
		fmt.Fprintf(out, "  %-30s %d rows\n", k, n)
		totalOrphans += n
	}
	if totalOrphans > 0 {
		fmt.Fprintf(out,
			"\nWARNING: %d soft-FK rows do not reference any photos.uid. This may be "+
				"expected (PhotoPrism originals deleted before the migration) but is worth "+
				"investigating.\n", totalOrphans)
	}
}

// sortedKeys returns a deterministic ordering of the map keys so the
// printed output is reproducible across runs.
func sortedKeys(m map[string]int64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
