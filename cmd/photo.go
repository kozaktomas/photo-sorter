package cmd

import (
	"github.com/spf13/cobra"
)

var photoCmd = &cobra.Command{
	Use:   "photo",
	Short: "Photo operations and information",
	Long:  `Commands for working with individual photos or photo collections.`,
}

func init() {
	rootCmd.AddCommand(photoCmd)
}
