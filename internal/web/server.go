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

// Server represents the web server
type Server struct {
	config         *config.Config
	router         *chi.Mux
	httpServer     *http.Server
	jobManager     *handlers.JobManager
	sessionManager *middleware.SessionManager
}

// NewServer creates a new web server
func NewServer(cfg *config.Config, port int, host string, sessionSecret string, sessionRepo middleware.SessionRepository) *Server {
	r := chi.NewRouter()

	// Create job manager for async operations
	jobManager := handlers.NewJobManager()

	// Create session manager with optional persistence
	sessionManager := middleware.NewSessionManager(sessionSecret, sessionRepo)

	s := &Server{
		config:         cfg,
		router:         r,
		jobManager:     jobManager,
		sessionManager: sessionManager,
	}

	// Set up middleware stack
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.RealIP)
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Timeout(5 * time.Minute))
	r.Use(middleware.CORS())

	// Set up routes
	s.setupRoutes(sessionManager)

	// Create HTTP server
	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", host, port),
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute, // Long timeout for SSE and uploads
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// Start starts the HTTP server
func (s *Server) Start() error {
	log.Printf("Starting web server on %s", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("failed to start server: %w", err)
	}
	return nil
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("Shutting down web server...")

	// Stop the session cleanup goroutine
	if s.sessionManager != nil {
		s.sessionManager.Stop()
	}

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutting down server: %w", err)
	}
	return nil
}

// Router returns the chi router for testing
func (s *Server) Router() *chi.Mux {
	return s.router
}
