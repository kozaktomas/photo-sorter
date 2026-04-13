package web

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/web/handlers"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// Server represents the web server.
type Server struct {
	config         *config.Config
	router         *chi.Mux
	httpServer     *http.Server
	jobManager     *handlers.JobManager
	sessionManager *middleware.SessionManager
	mcpHandler     http.Handler // nil if MCP not enabled
	booksHandler   *handlers.BooksHandler
}

// NewServer creates a new web server.
// mcpHandler is optional — pass nil to disable MCP endpoints.
func NewServer(
	cfg *config.Config, port int, host string,
	sessionSecret string, sessionRepo middleware.SessionRepository,
	mcpHandler http.Handler,
) *Server {
	r := chi.NewRouter()

	// Create job manager for async operations.
	jobManager := handlers.NewJobManager()

	// Create session manager with optional persistence.
	sessionManager := middleware.NewSessionManager(sessionSecret, sessionRepo)

	s := &Server{
		config:         cfg,
		router:         r,
		jobManager:     jobManager,
		sessionManager: sessionManager,
		mcpHandler:     mcpHandler,
	}

	// Middleware stack. chiMiddleware.Timeout is deliberately NOT applied
	// at this level — it would kill SSE progress streams, large PDF
	// downloads, and long multipart uploads after 5 minutes. It is applied
	// inside setupRoutes on the short-lived sub-group instead. The
	// http.Server.WriteTimeout below is the other half of that story; it
	// stays at 5 min and is disabled per-request via the NoWriteDeadline
	// middleware on the long-running routes.
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.RealIP)
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(middleware.CORS())
	r.Use(middleware.SecurityHeaders())

	// Set up routes.
	s.setupRoutes(sessionManager)

	// Create HTTP server.
	s.httpServer = &http.Server{
		Addr:        fmt.Sprintf("%s:%d", host, port),
		Handler:     r,
		ReadTimeout: 30 * time.Second,
		// Applied to short-lived routes. Long-running routes (SSE streams,
		// book export, large downloads, multipart uploads) disable this
		// per-request via the NoWriteDeadline middleware — see routes.go.
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	log.Printf("Starting web server on %s", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("failed to start server: %w", err)
	}
	return nil
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("Shutting down web server...")

	// Stop the session cleanup goroutine.
	if s.sessionManager != nil {
		s.sessionManager.Stop()
	}

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutting down server: %w", err)
	}

	// Release background workers and temp files owned by handlers after the
	// HTTP server has stopped accepting requests.
	if s.booksHandler != nil {
		s.booksHandler.Shutdown()
	}
	return nil
}

// Router returns the chi router for testing.
func (s *Server) Router() *chi.Mux {
	return s.router
}
