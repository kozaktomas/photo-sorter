package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Server wraps the MCP server with dependencies needed by tool handlers.
type Server struct {
	mcpServer  *server.MCPServer
	sseServer  *server.SSEServer
	bookWriter database.BookWriter
	pp         *photoprism.PhotoPrism
	apiToken   string
}

// NewServer creates a new MCP server with all book/chapter tools registered.
func NewServer(
	version string,
	bookWriter database.BookWriter,
	pp *photoprism.PhotoPrism,
	apiToken string,
) *Server {
	s := &Server{
		bookWriter: bookWriter,
		pp:         pp,
		apiToken:   apiToken,
	}

	mcpServer := server.NewMCPServer(
		"photo-sorter-books",
		version,
		server.WithToolCapabilities(true),
	)

	s.mcpServer = mcpServer
	s.registerBookTools()
	s.registerChapterTools()
	s.registerSectionTools()
	s.registerSectionPhotoTools()

	return s
}

// Start begins serving on the given address (e.g. ":8086").
func (s *Server) Start(addr string) error {
	s.sseServer = server.NewSSEServer(s.mcpServer,
		server.WithBaseURL("http://localhost"+addr),
	)

	handler := s.authMiddleware(s.sseServer)

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return fmt.Errorf("listen: %w", httpServer.ListenAndServe())
}

// authMiddleware validates the Bearer token on every request.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, `{"error":"missing or invalid Authorization header"}`, http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token != s.apiToken {
			http.Error(w, `{"error":"invalid API token"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// registerBookTools registers book CRUD tools.
func (s *Server) registerBookTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("list_books",
			mcp.WithDescription("List all photo books"),
		),
		s.handleListBooks,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("get_book",
			mcp.WithDescription("Get book detail with chapters, sections, pages"),
			mcp.WithString("book_id", mcp.Required(), mcp.Description("Book ID (UUID)")),
		),
		s.handleGetBook,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("create_book",
			mcp.WithDescription("Create a new photo book"),
			mcp.WithString("title", mcp.Required(), mcp.Description("Book title")),
			mcp.WithString("description", mcp.Description("Book description")),
		),
		s.handleCreateBook,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("update_book",
			mcp.WithDescription("Update book title/description"),
			mcp.WithString("book_id", mcp.Required(), mcp.Description("Book ID (UUID)")),
			mcp.WithString("title", mcp.Description("New title")),
			mcp.WithString("description", mcp.Description("New description")),
		),
		s.handleUpdateBook,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("delete_book",
			mcp.WithDescription("Delete a book and all its content"),
			mcp.WithString("book_id", mcp.Required(), mcp.Description("Book ID (UUID)")),
		),
		s.handleDeleteBook,
	)
}

// registerChapterTools registers chapter CRUD + reorder tools.
//
//nolint:dupl // intentionally mirrors registerSectionTools — same CRUD pattern, different entity
func (s *Server) registerChapterTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("create_chapter",
			mcp.WithDescription("Create a chapter in a book"),
			mcp.WithString("book_id", mcp.Required(), mcp.Description("Book ID (UUID)")),
			mcp.WithString("title", mcp.Required(), mcp.Description("Chapter title")),
			mcp.WithString("color", mcp.Description("Hex color (e.g. #8B0000)")),
		),
		s.handleCreateChapter,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("update_chapter",
			mcp.WithDescription("Update chapter title/color"),
			mcp.WithString("chapter_id", mcp.Required(), mcp.Description("Chapter ID (UUID)")),
			mcp.WithString("title", mcp.Description("New title")),
			mcp.WithString("color", mcp.Description("New hex color")),
		),
		s.handleUpdateChapter,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("delete_chapter",
			mcp.WithDescription("Delete a chapter"),
			mcp.WithString("chapter_id", mcp.Required(), mcp.Description("Chapter ID (UUID)")),
		),
		s.handleDeleteChapter,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("reorder_chapters",
			mcp.WithDescription("Reorder chapters in a book"),
			mcp.WithString("book_id", mcp.Required(), mcp.Description("Book ID (UUID)")),
			mcp.WithArray("chapter_ids", mcp.Required(), mcp.Description("Chapter IDs (UUIDs) in new order")),
		),
		s.handleReorderChapters,
	)
}

// ctx returns a background context for database operations.
func (s *Server) ctx() context.Context {
	return context.Background()
}
