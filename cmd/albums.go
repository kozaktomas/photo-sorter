package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
)

var albumsCmd = &cobra.Command{
	Use:   "albums",
	Short: "List all albums from PhotoPrism",
	Long:  `Retrieves and displays all albums from your PhotoPrism instance.`,
	RunE:  runAlbums,
}

func init() {
	rootCmd.AddCommand(albumsCmd)

	albumsCmd.Flags().Int("count", 100, "Number of albums to retrieve")
	albumsCmd.Flags().Int("offset", 0, "Offset for pagination")
	albumsCmd.Flags().String("order", "", "Sort order (e.g., 'name', 'count')")
	albumsCmd.Flags().String("query", "", "Search query to filter albums")
}

func runAlbums(cmd *cobra.Command, args []string) error {
	cfg := config.Load()

	count := mustGetInt(cmd, "count")
	offset := mustGetInt(cmd, "offset")
	order := mustGetString(cmd, "order")
	query := mustGetString(cmd, "query")

	pp, err := photoprism.NewPhotoPrismWithCapture(cfg.PhotoPrism.URL, cfg.PhotoPrism.Username, cfg.PhotoPrism.GetPassword(), captureDir)
	if err != nil {
		return fmt.Errorf("failed to connect to PhotoPrism: %w", err)
	}
	defer pp.Logout()

	albums, err := pp.GetAlbums(count, offset, order, query, "")
	if err != nil {
		return fmt.Errorf("failed to get albums: %w", err)
	}

	if len(albums) == 0 {
		fmt.Println("No albums found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "UID\tTITLE\tPHOTOS\tTYPE")
	fmt.Fprintln(w, "---\t-----\t------\t----")

	for i := range albums {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", albums[i].UID, albums[i].Title, albums[i].PhotoCount, albums[i].Type)
	}

	w.Flush()

	fmt.Printf("\nTotal: %d albums\n", len(albums))

	return nil
}
