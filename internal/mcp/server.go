package mcp

import (
	"context"
	"net/http"
	"strings"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Server wraps the MCP server with dependencies needed by tool handlers.
type Server struct {
	mcpServer        *server.MCPServer
	sseServer        *server.SSEServer
	bookWriter       database.BookWriter
	textVersionStore database.TextVersionStore
	textCheckStore   database.TextCheckStore
	embeddingReader  database.EmbeddingReader
	pp               *photoprism.PhotoPrism
	config           *config.Config
	apiToken         string
}

// NewServer creates a new MCP server with all book/chapter tools registered.
// basePath is the URL prefix where the MCP SSE server will be mounted (e.g. "/mcp").
func NewServer(
	version string,
	bookWriter database.BookWriter,
	textVersionStore database.TextVersionStore,
	textCheckStore database.TextCheckStore,
	embeddingReader database.EmbeddingReader,
	pp *photoprism.PhotoPrism,
	cfg *config.Config,
	apiToken string,
	basePath string,
) *Server {
	s := &Server{
		bookWriter:       bookWriter,
		textVersionStore: textVersionStore,
		textCheckStore:   textCheckStore,
		embeddingReader:  embeddingReader,
		pp:               pp,
		config:           cfg,
		apiToken:         apiToken,
	}

	mcpServer := server.NewMCPServer(
		"photo-sorter-books",
		version,
		server.WithToolCapabilities(true),
	)

	s.mcpServer = mcpServer
	s.sseServer = server.NewSSEServer(mcpServer,
		server.WithStaticBasePath(basePath),
	)

	s.registerBookTools()
	s.registerChapterTools()
	s.registerSectionTools()
	s.registerSectionPhotoTools()
	s.registerPageTools()
	s.registerSlotTools()
	s.registerTextTools()
	s.registerPhotoTools()
	s.registerAlbumTools()
	s.registerLabelTools()

	return s
}

// Handler returns the MCP SSE server as an http.Handler.
// The caller is responsible for applying auth middleware.
func (s *Server) Handler() http.Handler {
	return s.sseServer
}

// APIToken returns the configured API token for Bearer auth.
func (s *Server) APIToken() string {
	return s.apiToken
}

// BearerAuthMiddleware returns middleware that validates Bearer token authentication.
func BearerAuthMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				http.Error(w, `{"error":"missing or invalid Authorization header"}`, http.StatusUnauthorized)
				return
			}
			t := strings.TrimPrefix(authHeader, "Bearer ")
			if t != token {
				http.Error(w, `{"error":"invalid API token"}`, http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
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

	s.registerUpdateBookTool()

	s.mcpServer.AddTool(
		mcp.NewTool("delete_book",
			mcp.WithDescription("Delete a book and all its content"),
			mcp.WithString("book_id", mcp.Required(), mcp.Description("Book ID (UUID)")),
		),
		s.handleDeleteBook,
	)
}

// registerUpdateBookTool registers the update_book tool. Extracted because
// its typography argument surface pushes the parent function past funlen.
func (s *Server) registerUpdateBookTool() {
	s.mcpServer.AddTool(
		mcp.NewTool("update_book",
			mcp.WithDescription(
				"Update book title, description, or typography settings"),
			mcp.WithString("book_id", mcp.Required(),
				mcp.Description("Book ID (UUID)")),
			mcp.WithString("title", mcp.Description("New title")),
			mcp.WithString("description", mcp.Description("New description")),
			mcp.WithString("body_font",
				mcp.Description("Body font ID (must exist in font registry)")),
			mcp.WithString("heading_font",
				mcp.Description("Heading font ID (must exist in font registry)")),
			mcp.WithNumber("body_font_size",
				mcp.Description("Body font size in pt (6-36)")),
			mcp.WithNumber("body_line_height",
				mcp.Description("Body line height in pt (8-48)")),
			mcp.WithNumber("h1_font_size",
				mcp.Description("H1 heading size in pt (6-36)")),
			mcp.WithNumber("h2_font_size",
				mcp.Description("H2 heading size in pt (6-36)")),
			mcp.WithNumber("caption_opacity",
				mcp.Description("Photo caption opacity (0.0-1.0)")),
			mcp.WithNumber("caption_font_size",
				mcp.Description("Caption font size in pt (6-36)")),
			mcp.WithNumber("heading_color_bleed",
				mcp.Description("Chapter color bleed behind headings in mm (0-20)")),
			mcp.WithNumber("caption_badge_size",
				mcp.Description("Caption badge size in mm (2-12)")),
			mcp.WithNumber("body_text_pad_mm",
				mcp.Description("Inner padding (mm) added to body text on the side adjacent to a photo in mixed layouts (0-10)")),
		),
		s.handleUpdateBook,
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
