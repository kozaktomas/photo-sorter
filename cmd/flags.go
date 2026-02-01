package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// mustGetBool gets a bool flag value or panics if the flag doesn't exist.
// This is appropriate for flags defined in init() - errors indicate programming bugs.
func mustGetBool(cmd *cobra.Command, name string) bool {
	val, err := cmd.Flags().GetBool(name)
	if err != nil {
		panic(fmt.Sprintf("flag error for --%s: %v", name, err))
	}
	return val
}

// mustGetInt gets an int flag value or panics if the flag doesn't exist.
func mustGetInt(cmd *cobra.Command, name string) int {
	val, err := cmd.Flags().GetInt(name)
	if err != nil {
		panic(fmt.Sprintf("flag error for --%s: %v", name, err))
	}
	return val
}

// mustGetString gets a string flag value or panics if the flag doesn't exist.
func mustGetString(cmd *cobra.Command, name string) string {
	val, err := cmd.Flags().GetString(name)
	if err != nil {
		panic(fmt.Sprintf("flag error for --%s: %v", name, err))
	}
	return val
}

// mustGetFloat64 gets a float64 flag value or panics if the flag doesn't exist.
func mustGetFloat64(cmd *cobra.Command, name string) float64 {
	val, err := cmd.Flags().GetFloat64(name)
	if err != nil {
		panic(fmt.Sprintf("flag error for --%s: %v", name, err))
	}
	return val
}

// mustGetStringSlice gets a string slice flag value or panics if the flag doesn't exist.
func mustGetStringSlice(cmd *cobra.Command, name string) []string {
	val, err := cmd.Flags().GetStringSlice(name)
	if err != nil {
		panic(fmt.Sprintf("flag error for --%s: %v", name, err))
	}
	return val
}
