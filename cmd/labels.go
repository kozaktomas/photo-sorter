package cmd

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
)

var labelsCmd = &cobra.Command{
	Use:   "labels",
	Short: "List and manage labels",
	Long:  `List all labels from PhotoPrism. Use subcommands to delete labels.`,
	RunE:  runLabelsList,
}

var labelsDeleteCmd = &cobra.Command{
	Use:   "delete [uid...]",
	Short: "Delete labels by UID",
	Long: `Delete one or more labels by their UID.

Example:
  photo-sorter labels delete lqb0y3b13vqo0gjx
  photo-sorter labels delete lqb0y3b13vqo0gjx lqb0y3b13vqo0gjy`,
	Args: cobra.MinimumNArgs(1),
	RunE: runLabelsDelete,
}

func init() {
	rootCmd.AddCommand(labelsCmd)
	labelsCmd.AddCommand(labelsDeleteCmd)

	// List flags
	labelsCmd.Flags().Int("count", 1000, "Maximum number of labels to retrieve")
	labelsCmd.Flags().Bool("all", true, "Include all labels (including unused)")
	labelsCmd.Flags().String("sort", "name", "Sort by: name, count, -name, -count (prefix with - for descending)")
	labelsCmd.Flags().Int("min-photos", 0, "Only show labels with at least N photos")

	// Delete flags
	labelsDeleteCmd.Flags().Bool("yes", false, "Skip confirmation prompt")
}

func runLabelsList(cmd *cobra.Command, args []string) error {
	cfg := config.Load()

	count := mustGetInt(cmd, "count")
	all := mustGetBool(cmd, "all")
	sortBy := mustGetString(cmd, "sort")
	minPhotos := mustGetInt(cmd, "min-photos")

	pp, err := photoprism.NewPhotoPrismWithCapture(cfg.PhotoPrism.URL, cfg.PhotoPrism.Username, cfg.PhotoPrism.Password, captureDir)
	if err != nil {
		return fmt.Errorf("failed to connect to PhotoPrism: %w", err)
	}
	defer pp.Logout()

	labels, err := pp.GetLabels(count, 0, all)
	if err != nil {
		return fmt.Errorf("failed to get labels: %w", err)
	}

	// Filter by minimum photo count
	if minPhotos > 0 {
		var filtered []photoprism.Label
		for _, label := range labels {
			if label.PhotoCount >= minPhotos {
				filtered = append(filtered, label)
			}
		}
		labels = filtered
	}

	// Sort labels
	sortLabels(labels, sortBy)

	if len(labels) == 0 {
		fmt.Println("No labels found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "UID\tNAME\tPHOTOS\tFAVORITE")
	fmt.Fprintln(w, "---\t----\t------\t--------")

	for _, label := range labels {
		fav := ""
		if label.Favorite {
			fav = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", label.UID, label.Name, label.PhotoCount, fav)
	}

	w.Flush()

	fmt.Printf("\nTotal: %d labels\n", len(labels))

	return nil
}

func runLabelsDelete(cmd *cobra.Command, args []string) error {
	cfg := config.Load()
	skipConfirm := mustGetBool(cmd, "yes")

	pp, err := photoprism.NewPhotoPrismWithCapture(cfg.PhotoPrism.URL, cfg.PhotoPrism.Username, cfg.PhotoPrism.Password, captureDir)
	if err != nil {
		return fmt.Errorf("failed to connect to PhotoPrism: %w", err)
	}
	defer pp.Logout()

	// Get all labels to show names
	labels, err := pp.GetLabels(10000, 0, true)
	if err != nil {
		return fmt.Errorf("failed to get labels: %w", err)
	}

	// Build UID to name map
	uidToName := make(map[string]string)
	for _, label := range labels {
		uidToName[label.UID] = label.Name
	}

	// Validate UIDs and show what will be deleted
	var validUIDs []string
	fmt.Println("Labels to delete:")
	for _, uid := range args {
		if name, ok := uidToName[uid]; ok {
			fmt.Printf("  - %s (%s)\n", name, uid)
			validUIDs = append(validUIDs, uid)
		} else {
			fmt.Printf("  - WARNING: Unknown UID %s (skipping)\n", uid)
		}
	}

	if len(validUIDs) == 0 {
		return fmt.Errorf("no valid labels to delete")
	}

	// Confirm deletion
	if !skipConfirm {
		fmt.Printf("\nDelete %d label(s)? [y/N]: ", len(validUIDs))
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	// Delete labels
	if err := pp.DeleteLabels(validUIDs); err != nil {
		return fmt.Errorf("failed to delete labels: %w", err)
	}

	fmt.Printf("Deleted %d label(s).\n", len(validUIDs))
	return nil
}

func sortLabels(labels []photoprism.Label, sortBy string) {
	descending := strings.HasPrefix(sortBy, "-")
	field := strings.TrimPrefix(sortBy, "-")

	sort.Slice(labels, func(i, j int) bool {
		var less bool
		switch field {
		case "count":
			less = labels[i].PhotoCount < labels[j].PhotoCount
		case "name":
			fallthrough
		default:
			less = strings.ToLower(labels[i].Name) < strings.ToLower(labels[j].Name)
		}
		if descending {
			return !less
		}
		return less
	})
}
