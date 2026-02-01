package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
)

var photoClearFacesCmd = &cobra.Command{
	Use:   "clear-faces <photo-uid>",
	Short: "Remove all face markers from a photo",
	Long: `Removes all face markers from a photo in PhotoPrism.

By default, this deletes all face markers (both assigned and unassigned).
Use --assigned-only to only remove markers that have a person assigned.

Examples:
  # Delete all face markers from a photo
  photo-sorter photo clear-faces pt4abc123def

  # Only remove assigned markers (keep unassigned detections)
  photo-sorter photo clear-faces pt4abc123def --assigned-only

  # Preview what would be deleted (dry run)
  photo-sorter photo clear-faces pt4abc123def --dry-run`,
	Args: cobra.ExactArgs(1),
	RunE: runPhotoClearFaces,
}

func init() {
	photoCmd.AddCommand(photoClearFacesCmd)

	photoClearFacesCmd.Flags().Bool("dry-run", false, "Preview changes without applying them")
	photoClearFacesCmd.Flags().Bool("assigned-only", false, "Only remove markers with person assignments")
}

func runPhotoClearFaces(cmd *cobra.Command, args []string) error {
	photoUID := args[0]
	dryRun := mustGetBool(cmd, "dry-run")
	assignedOnly := mustGetBool(cmd, "assigned-only")

	cfg := config.Load()

	// Connect to PhotoPrism
	fmt.Println("Connecting to PhotoPrism...")
	pp, err := photoprism.NewPhotoPrismWithCapture(
		cfg.PhotoPrism.URL,
		cfg.PhotoPrism.Username,
		cfg.PhotoPrism.Password,
		captureDir,
	)
	if err != nil {
		return fmt.Errorf("failed to connect to PhotoPrism: %w", err)
	}
	defer pp.Logout()

	// Get markers for the photo
	fmt.Printf("Getting markers for photo %s...\n", photoUID)
	markers, err := pp.GetPhotoMarkers(photoUID)
	if err != nil {
		return fmt.Errorf("failed to get markers: %w", err)
	}

	// Filter to face markers
	var faceMarkers []photoprism.Marker
	for _, m := range markers {
		if m.Type != "face" {
			continue
		}
		if assignedOnly && m.Name == "" && m.SubjUID == "" {
			continue
		}
		faceMarkers = append(faceMarkers, m)
	}

	if len(faceMarkers) == 0 {
		if assignedOnly {
			fmt.Println("No assigned face markers found on this photo.")
		} else {
			fmt.Println("No face markers found on this photo.")
		}
		return nil
	}

	fmt.Printf("Found %d face marker(s) to delete:\n", len(faceMarkers))
	for i, m := range faceMarkers {
		name := m.Name
		if name == "" {
			name = "(unassigned)"
		}
		fmt.Printf("  %d. %s (marker: %s)\n", i+1, name, m.UID)
	}

	if dryRun {
		fmt.Println("\nDry run - no changes made.")
		return nil
	}

	fmt.Println("\nDeleting face markers...")
	deleted := 0
	for _, m := range faceMarkers {
		_, err := pp.DeleteMarker(m.UID)
		if err != nil {
			name := m.Name
			if name == "" {
				name = "(unassigned)"
			}
			fmt.Printf("  Failed to delete %s (%s): %v\n", name, m.UID, err)
			continue
		}
		name := m.Name
		if name == "" {
			name = "(unassigned)"
		}
		fmt.Printf("  Deleted: %s\n", name)
		deleted++
	}

	fmt.Printf("\nDeleted %d/%d face markers.\n", deleted, len(faceMarkers))
	return nil
}
