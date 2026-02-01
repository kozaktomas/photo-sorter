package cmd

import (
	"github.com/spf13/cobra"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Cache management commands",
	Long:  `Commands for managing the local PostgreSQL face cache.`,
}

func init() {
	rootCmd.AddCommand(cacheCmd)
}
