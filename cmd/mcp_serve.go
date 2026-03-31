package cmd

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	mcpserver "github.com/kozaktomas/photo-sorter/internal/mcp"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/spf13/cobra"
)

var mcpServeCmd = &cobra.Command{
	Use:   "mcp-serve",
	Short: "Start the MCP server for photo book management",
	Long: `Start an MCP (Model Context Protocol) server that exposes photo book
management operations as tools. Uses HTTP SSE transport.

Requires MCP_API_TOKEN environment variable for authentication.
MCP clients must pass the token as "Authorization: Bearer <token>".`,
	RunE: runMCPServe,
}

func init() {
	rootCmd.AddCommand(mcpServeCmd)

	mcpServeCmd.Flags().Int("port", 8086, "Port to listen on")
	mcpServeCmd.Flags().String("host", "0.0.0.0", "Host to bind to")
}

func runMCPServe(cmd *cobra.Command, args []string) error {
	apiToken := os.Getenv("MCP_API_TOKEN")
	if apiToken == "" {
		return errors.New("MCP_API_TOKEN environment variable is required")
	}

	cfg := config.Load()

	if cfg.Database.URL == "" {
		return errors.New("DATABASE_URL environment variable is required")
	}

	fmt.Println("Connecting to PostgreSQL database...")
	if err := postgres.Initialize(&cfg.Database); err != nil {
		return fmt.Errorf("failed to initialize PostgreSQL: %w", err)
	}

	pool := postgres.GetGlobalPool()
	bookRepo := postgres.NewBookRepository(pool)
	database.RegisterBookWriter(func() database.BookWriter { return bookRepo })

	// Create PhotoPrism session for metadata access.
	if cfg.PhotoPrism.URL == "" {
		return errors.New("PHOTOPRISM_URL environment variable is required")
	}
	pp, err := photoprism.NewPhotoPrism(cfg.PhotoPrism.URL, cfg.PhotoPrism.Username, cfg.PhotoPrism.GetPassword())
	if err != nil {
		return fmt.Errorf("failed to connect to PhotoPrism: %w", err)
	}

	port := mustGetInt(cmd, "port")
	host := mustGetString(cmd, "host")
	addr := fmt.Sprintf("%s:%d", host, port)

	server := mcpserver.NewServer(Version, bookRepo, pp, apiToken)

	fmt.Printf("Starting MCP server (photo-sorter-books) on http://%s\n", addr)
	fmt.Println("Press Ctrl+C to stop")

	if err := server.Start(":" + strconv.Itoa(port)); err != nil {
		return fmt.Errorf("mcp server: %w", err)
	}
	return nil
}
