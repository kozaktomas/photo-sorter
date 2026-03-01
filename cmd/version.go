package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Build metadata variables, set by -ldflags at compile time.
var (
	Version   = "dev"
	CommitSHA = "unknown"
	BuildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("photo-sorter %s\n", Version)
		fmt.Printf("  Commit: %s\n", CommitSHA)
		fmt.Printf("  Built:  %s\n", BuildDate)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
