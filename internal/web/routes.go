package web

import (
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/kozaktomas/photo-sorter/internal/auth"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/storage"
	"github.com/kozaktomas/photo-sorter/internal/web/handlers"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
	"github.com/kozaktomas/photo-sorter/internal/web/static"
)

// resolveNativePhotoBackends best-effort-fetches the native PhotoWriter and
// constructs an on-disk Storage. Both are optional during the PhotoPrism
// migration: when either is unavailable, the native endpoints return 503
// while the PhotoPrism-backed endpoints (faces, similarity, etc.) keep
// working. Failures are logged but never block server startup.
func (s *Server) resolveNativePhotoBackends() (database.PhotoWriter, *storage.Storage) {
	var repo database.PhotoWriter
	if r, err := database.GetPhotoWriter(context.Background()); err == nil {
		repo = r
	} else {
		log.Printf("photos: native repo unavailable: %v", err)
	}

	var store *storage.Storage
	st, err := storage.New(s.config.Storage.OriginalsPath, s.config.Storage.CachePath)
	if err != nil {
		log.Printf("photos: native storage unavailable: %v", err)
	} else {
		store = st
	}
	return repo, store
}

// resolveAlbumRepo best-effort-fetches the native AlbumWriter. Returns nil
// (and logs) when the registration is missing — the native album endpoints
// will then surface a 503 rather than blocking server startup.
func (s *Server) resolveAlbumRepo() database.AlbumWriter {
	r, err := database.GetAlbumWriter(context.Background())
	if err != nil {
		log.Printf("albums: native repo unavailable: %v", err)
		return nil
	}
	return r
}

// resolveLabelRepo best-effort-fetches the native LabelWriter. Returns nil
// (and logs) when the registration is missing — the native label endpoints
// will then surface a 503 rather than blocking server startup.
func (s *Server) resolveLabelRepo() database.LabelWriter {
	r, err := database.GetLabelWriter(context.Background())
	if err != nil {
		log.Printf("labels: native repo unavailable: %v", err)
		return nil
	}
	return r
}

// resolveMarkerRepo best-effort-fetches the native MarkerWriter. Returns
// nil (and logs) when the registration is missing — the native marker /
// face endpoints will then surface a 503 rather than blocking server
// startup.
func (s *Server) resolveMarkerRepo() database.MarkerWriter {
	r, err := database.GetMarkerWriter(context.Background())
	if err != nil {
		log.Printf("markers: native repo unavailable: %v", err)
		return nil
	}
	return r
}

// resolveSubjectRepo best-effort-fetches the native SubjectWriter. Returns
// nil (and logs) when the registration is missing — the native subject
// endpoints will then surface a 503 rather than blocking server startup.
func (s *Server) resolveSubjectRepo() database.SubjectWriter {
	r, err := database.GetSubjectWriter(context.Background())
	if err != nil {
		log.Printf("subjects: native repo unavailable: %v", err)
		return nil
	}
	return r
}

// resolveShareLinkRepo best-effort-fetches the native ShareLinkWriter.
// Returns nil (and logs) when the registration is missing — the share
// endpoints then surface a 503 instead of blocking server startup.
func (s *Server) resolveShareLinkRepo() database.ShareLinkWriter {
	r, err := database.GetShareLinkWriter(context.Background())
	if err != nil {
		log.Printf("share-links: native repo unavailable: %v", err)
		return nil
	}
	return r
}

// resolveAuditLogRepo best-effort-fetches the native AuditLogWriter.
// Returns nil (and logs) when the registration is missing — the audit
// trail will then be disabled but the rest of the API keeps working.
func (s *Server) resolveAuditLogRepo() database.AuditLogReader {
	r, err := database.GetAuditLogReader(context.Background())
	if err != nil {
		log.Printf("audit-log: native repo unavailable: %v", err)
		return nil
	}
	return r
}

// resolveSmartAlbumRepo best-effort-fetches the native SmartAlbumWriter.
// Returns nil (and logs) when the registration is missing — the smart
// album endpoints then surface a 503 instead of blocking server startup.
func (s *Server) resolveSmartAlbumRepo() database.SmartAlbumWriter {
	r, err := database.GetSmartAlbumWriter(context.Background())
	if err != nil {
		log.Printf("smart-albums: native repo unavailable: %v", err)
		return nil
	}
	return r
}

// resolveUserRepos best-effort-fetches the native UserReader and
// UserWriter. Returns nils (and logs) when the registration is missing —
// the auth handler then surfaces a 500 from login until the user store is
// wired in.
func (s *Server) resolveUserRepos() (database.UserReader, database.UserWriter) {
	reader, err := database.GetUserReader(context.Background())
	if err != nil {
		log.Printf("auth: user reader unavailable: %v", err)
		return nil, nil
	}
	writer, err := database.GetUserWriter(context.Background())
	if err != nil {
		log.Printf("auth: user writer unavailable: %v", err)
		return reader, nil
	}
	return reader, writer
}

//nolint:funlen // Route registration is inherently long.
func (s *Server) setupRoutes(sessionManager *middleware.SessionManager) {
	// Resolve native photo backends. Both are optional in the transitional
	// state: when PhotoReader/Storage are unavailable, the native GET
	// endpoints return 503 while PhotoPrism-backed endpoints keep working.
	photoRepo, photoStore := s.resolveNativePhotoBackends()
	albumRepo := s.resolveAlbumRepo()
	labelRepo := s.resolveLabelRepo()
	markerRepo := s.resolveMarkerRepo()
	subjectRepo := s.resolveSubjectRepo()
	shareLinkRepo := s.resolveShareLinkRepo()
	smartAlbumRepo := s.resolveSmartAlbumRepo()
	userReader, userWriter := s.resolveUserRepos()

	// Create handlers.
	authHandler := handlers.NewAuthHandler(s.config, sessionManager, userReader, userWriter)
	albumsHandler := handlers.NewAlbumsHandler(s.config, sessionManager, albumRepo, photoRepo)
	labelsHandler := handlers.NewLabelsHandler(s.config, sessionManager, labelRepo)
	photosHandler := handlers.NewPhotosHandler(s.config, sessionManager, photoRepo, photoStore, labelRepo)
	sortHandler := handlers.NewSortHandler(s.config, sessionManager, s.jobManager, labelRepo)
	configHandler := handlers.NewConfigHandler(s.config)
	facesHandler := handlers.NewFacesHandler(
		s.config, sessionManager,
		markerRepo, subjectRepo, photoRepo, photoStore,
	)
	statsHandler := handlers.NewStatsHandler(s.config, sessionManager)
	processHandler := handlers.NewProcessHandler(s.config, sessionManager, facesHandler, photosHandler, statsHandler)
	uploadHandler := handlers.NewUploadHandler(s.config, sessionManager, processHandler)
	booksHandler := handlers.NewBooksHandler(s.config, sessionManager)
	s.booksHandler = booksHandler
	textHandler := handlers.NewTextHandler(s.config)
	textVersionsHandler := handlers.NewTextVersionsHandler()
	usersHandler := handlers.NewUsersHandler(s.config, userWriter)
	shareHandler := handlers.NewShareHandler(
		s.config, sessionManager, shareLinkRepo, albumRepo, photoRepo, photoStore,
	)
	smartAlbumsHandler := handlers.NewSmartAlbumsHandler(s.config, sessionManager, smartAlbumRepo, photoRepo)
	auditLogRepo := s.resolveAuditLogRepo()
	auditLogHandler := handlers.NewAuditLogHandler(auditLogRepo)

	// Health check (no auth required).
	s.router.Get("/api/v1/health", handlers.HealthCheck)

	// API routes.
	s.router.Route("/api/v1", func(r chi.Router) {
		// Auth routes (no PhotoPrism client needed for login).
		r.Post("/auth/login", authHandler.Login)
		r.Post("/auth/logout", authHandler.Logout)
		r.Get("/auth/status", authHandler.Status)

		// Public share endpoints. These intentionally live outside the
		// RequireAuth group so anonymous recipients can hit them. The
		// handler enforces its own (optional) per-link password and
		// rate-limits the verify endpoint.
		r.Route("/public/share/{slug}", func(r chi.Router) {
			r.Get("/", shareHandler.Get)
			r.Post("/verify", shareHandler.VerifyPassword)
			r.Get("/photos", shareHandler.ListPhotos)
			r.Get("/photos/{photo_uid}/thumb/{size}", shareHandler.Thumbnail)
			r.Get("/photos/{photo_uid}/download", shareHandler.Download)
		})

		// All other routes require authentication and get a PhotoPrism client injected.
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(sessionManager))
			r.Use(middleware.WithPhotoPrismClient(s.config))

			// --- Short-lived endpoints ---
			// 5-minute chi Timeout + 5-minute http.Server WriteTimeout
			// apply here: suitable for CRUD where stuck requests should
			// be killed promptly.
			r.Group(func(r chi.Router) {
				r.Use(chiMiddleware.Timeout(5 * time.Minute))

				// Albums.
				r.Get("/albums", albumsHandler.List)
				r.Post("/albums", albumsHandler.Create)
				r.Get("/albums/{uid}", albumsHandler.Get)
				r.Put("/albums/{uid}", albumsHandler.Update)
				r.Delete("/albums/{uid}", albumsHandler.Delete)
				r.Get("/albums/{uid}/photos", albumsHandler.GetPhotos)
				r.Post("/albums/{uid}/photos", albumsHandler.AddPhotos)
				r.Delete("/albums/{uid}/photos", albumsHandler.ClearPhotos)
				r.Delete("/albums/{uid}/photos/batch", albumsHandler.RemovePhotos)

				// Album share links (auth-side).
				r.Post("/albums/{uid}/share", shareHandler.CreateLink)
				r.Get("/albums/{uid}/shares", shareHandler.ListLinks)
				r.Delete("/shares/{slug}", shareHandler.RevokeLink)

				// Smart albums (saved photo searches).
				r.Get("/smart-albums", smartAlbumsHandler.List)
				r.Post("/smart-albums", smartAlbumsHandler.Create)
				r.Get("/smart-albums/{uid}", smartAlbumsHandler.Get)
				r.Put("/smart-albums/{uid}", smartAlbumsHandler.Update)
				r.Delete("/smart-albums/{uid}", smartAlbumsHandler.Delete)
				r.Get("/smart-albums/{uid}/photos", smartAlbumsHandler.GetPhotos)

				// Labels.
				r.Get("/labels", labelsHandler.List)
				r.Get("/labels/{uid}", labelsHandler.Get)
				r.Put("/labels/{uid}", labelsHandler.Update)
				r.Delete("/labels", labelsHandler.BatchDelete)

				// Photos.
				r.Get("/photos", photosHandler.List)
				r.Get("/photos/histogram", photosHandler.Histogram)
				r.Get("/photos/geo-points", photosHandler.GeoPoints)
				r.Get("/photos/{uid}", photosHandler.Get)
				r.Put("/photos/{uid}", photosHandler.Update)
				r.Put("/photos/{uid}/exif", photosHandler.EditExif)
				r.Get("/photos/{uid}/thumb/{size}", photosHandler.Thumbnail)
				r.Get("/photos/{uid}/download", photosHandler.Download)
				r.Get("/photos/{uid}/edits", photosHandler.GetEdits)
				r.Put("/photos/{uid}/edits", photosHandler.PutEdits)
				r.Delete("/photos/{uid}/edits", photosHandler.DeleteEdits)
				r.Get("/photos/{uid}/faces", facesHandler.GetPhotoFaces)
				r.Post("/photos/{uid}/faces/compute", facesHandler.ComputeFaces)
				r.Get("/photos/{uid}/estimate-era", photosHandler.EstimateEra)
				r.Get("/photos/{uid}/albums", albumsHandler.GetPhotoAlbums)
				r.Get("/photos/{uid}/books", booksHandler.GetPhotoBookMemberships)
				r.Post("/photos/similar", photosHandler.FindSimilar)
				r.Post("/photos/similar/collection", photosHandler.FindSimilarToCollection)
				r.Post("/photos/batch/labels", photosHandler.BatchAddLabels)
				r.Post("/photos/batch/edit", photosHandler.BatchEdit)
				r.Post("/photos/batch/archive", photosHandler.BatchArchive)
				r.Post("/photos/batch/restore", photosHandler.BatchRestore)
				r.Get("/photos/trash", photosHandler.ListTrash)
				r.Post("/photos/duplicates", photosHandler.FindDuplicates)
				r.Post("/photos/suggest-albums", photosHandler.SuggestAlbums)
				r.Post("/photos/search-by-text", photosHandler.SearchByText)

				// Sort (start/poll/cancel; progress stream is in the long group).
				r.Post("/sort", sortHandler.Start)
				r.Get("/sort/{jobId}", sortHandler.Status)
				r.Delete("/sort/{jobId}", sortHandler.Cancel)

				// Upload cancel (the upload endpoints themselves are in the long group).
				r.Delete("/upload/{jobId}", uploadHandler.CancelJob)

				// Config.
				r.Get("/config", configHandler.Get)

				// Stats.
				r.Get("/stats", statsHandler.Get)

				// Faces.
				r.Get("/subjects", facesHandler.ListSubjects)
				r.Get("/subjects/{uid}", facesHandler.GetSubject)
				r.Put("/subjects/{uid}", facesHandler.UpdateSubject)
				r.Post("/faces/match", facesHandler.Match)
				r.Post("/faces/apply", facesHandler.Apply)
				r.Post("/faces/outliers", facesHandler.FindOutliers)

				// Process (start/cancel/sync; progress stream is in the long group).
				r.Post("/process", processHandler.Start)
				r.Delete("/process/{jobId}", processHandler.Cancel)
				r.Post("/process/sync-cache", processHandler.SyncCache)
				// Build-thumbs is the thumbnail backfill — admin only, since
				// it can rewrite the entire cache and is expensive.
				r.With(middleware.RequireRole(auth.RoleAdmin)).
					Post("/process/build-thumbs", processHandler.BuildThumbs)

				// Fonts.
				r.Get("/fonts", booksHandler.ListFonts)

				// Photo Books.
				r.Get("/books", booksHandler.ListBooks)
				r.Post("/books", booksHandler.CreateBook)
				r.Get("/books/{id}", booksHandler.GetBook)
				r.Put("/books/{id}", booksHandler.UpdateBook)
				r.Delete("/books/{id}", booksHandler.DeleteBook)
				r.Post("/books/{id}/chapters", booksHandler.CreateChapter)
				r.Put("/books/{id}/chapters/reorder", booksHandler.ReorderChapters)
				r.Put("/chapters/{id}", booksHandler.UpdateChapter)
				r.Delete("/chapters/{id}", booksHandler.DeleteChapter)
				r.Post("/books/{id}/sections", booksHandler.CreateSection)
				r.Put("/books/{id}/sections/reorder", booksHandler.ReorderSections)
				r.Put("/sections/{id}", booksHandler.UpdateSection)
				r.Delete("/sections/{id}", booksHandler.DeleteSection)
				r.Get("/sections/{id}/photos", booksHandler.GetSectionPhotos)
				r.Post("/sections/{id}/photos", booksHandler.AddSectionPhotos)
				r.Delete("/sections/{id}/photos", booksHandler.RemoveSectionPhotos)
				r.Put("/sections/{id}/photos/{photoUid}/description", booksHandler.UpdatePhotoDescription)
				r.Post("/books/{id}/pages", booksHandler.CreatePage)
				r.Put("/books/{id}/pages/reorder", booksHandler.ReorderPages)
				r.Put("/pages/{id}", booksHandler.UpdatePage)
				r.Delete("/pages/{id}", booksHandler.DeletePage)
				r.Put("/pages/{id}/slots/{index}", booksHandler.AssignSlot)
				r.Put("/pages/{id}/slots/{index}/crop", booksHandler.UpdateSlotCrop)
				r.Post("/pages/{id}/slots/swap", booksHandler.SwapSlots)
				r.Delete("/pages/{id}/slots/{index}", booksHandler.ClearSlot)
				r.Post("/books/{id}/sections/{sectionId}/auto-layout", booksHandler.AutoLayout)
				r.Get("/books/{id}/preflight", booksHandler.Preflight)
				r.Post("/books/{id}/export-pdf/job", booksHandler.StartExportJob)
				r.Get("/book-export/{jobId}", booksHandler.GetExportJob)
				r.Delete("/book-export/{jobId}", booksHandler.CancelExportJob)
				r.Get("/books/{id}/text-check-status", textHandler.TextCheckStatus)

				// Text AI operations.
				r.Post("/text/check", textHandler.Check)
				r.Post("/text/check-and-save", textHandler.CheckAndSave)
				r.Post("/text/rewrite", textHandler.Rewrite)
				r.Post("/text/consistency", textHandler.Consistency)

				// Text version history.
				r.Get("/text-versions", textVersionsHandler.List)
				r.Post("/text-versions/{id}/restore", textVersionsHandler.Restore)

				// Self-service user endpoints (any logged-in role).
				r.Get("/me", usersHandler.Me)
				r.Post("/me/password", usersHandler.ChangeMyPassword)

				// Admin-only routes.
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireRole(auth.RoleAdmin))
					r.Get("/users", usersHandler.List)
					r.Post("/users", usersHandler.Create)
					r.Get("/users/{uid}", usersHandler.Get)
					r.Put("/users/{uid}", usersHandler.Update)
					r.Post("/users/{uid}/password", usersHandler.SetPassword)
					r.Post("/users/{uid}/disable", usersHandler.SetDisabled)
					r.Delete("/users/{uid}", usersHandler.Delete)

					// Trash hard delete is admin-only: it removes the
					// originals from disk and is irreversible.
					r.Post("/photos/batch/purge", photosHandler.BatchPurge)

					// Audit log viewer: admin-only.
					r.Get("/audit-log", auditLogHandler.List)
				})
			})

			// --- Long-running / streaming endpoints ---
			// No chi Timeout. NoWriteDeadline lifts the per-connection
			// http.Server.WriteTimeout so SSE progress streams, synchronous
			// PDF generation, large downloads, and multipart uploads can run
			// as long as the client stays connected. Cancellation still
			// propagates via r.Context() when the client disconnects.
			r.Group(func(r chi.Router) {
				r.Use(middleware.NoWriteDeadline)

				// SSE progress streams.
				r.Get("/sort/{jobId}/events", sortHandler.Events)
				r.Get("/upload/{jobId}/events", uploadHandler.GetJobEvents)
				r.Get("/process/{jobId}/events", processHandler.Events)
				r.Get("/book-export/{jobId}/events", booksHandler.StreamExportJobEvents)

				// Large multipart uploads.
				r.Post("/upload", uploadHandler.Upload)
				r.Post("/upload/job", uploadHandler.StartJob)

				// Synchronous PDF generation (CLI/MCP) and large downloads.
				r.Get("/books/{id}/export-pdf", booksHandler.ExportPDF)
				r.Get("/pages/{id}/export-pdf", booksHandler.ExportPagePDF)
				r.Get("/book-export/{jobId}/download", booksHandler.DownloadExport)
			})
		})
	})

	// MCP SSE server (optional, enabled when MCP_API_TOKEN is set).
	if s.mcpHandler != nil {
		s.router.Handle("/mcp/*", s.mcpHandler)
	}

	// Serve static files for frontend (SPA).
	s.router.Get("/*", s.serveSPA)
}

// contentTypeByExt maps file extensions to MIME content types.
var contentTypeByExt = map[string]string{
	".html":  "text/html; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".js":    "application/javascript; charset=utf-8",
	".json":  "application/json",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".ico":   "image/x-icon",
	".woff2": "font/woff2",
	".woff":  "font/woff",
}

// getContentTypeForExt returns the MIME content type for a file path based on its extension.
func getContentTypeForExt(path string) string {
	// Find the last dot to extract extension.
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			if ct, ok := contentTypeByExt[path[i:]]; ok {
				return ct
			}
			break
		}
	}
	return "application/octet-stream"
}

// serveEmbeddedFile attempts to serve a file from the embedded filesystem.
// Returns true if the file was served, false otherwise.
func serveEmbeddedFile(w http.ResponseWriter, fsys http.FileSystem, path string) bool {
	f, err := fsys.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		return false
	}

	w.Header().Set("Content-Type", getContentTypeForExt(path))
	if strings.HasPrefix(path, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	w.WriteHeader(http.StatusOK)
	io.Copy(w, f)
	return true
}

// serveSPA serves the single-page application.
func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request) {
	if static.HasDist() {
		fs := static.GetFileSystem()
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		if serveEmbeddedFile(w, fs, path) {
			return
		}

		// For SPA routing, serve index.html for non-asset paths.
		if !strings.HasPrefix(path, "/assets/") && serveEmbeddedFile(w, fs, "/index.html") {
			return
		}
	}

	// Fallback: return placeholder page if no frontend is built.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
    <title>Photo Sorter</title>
    <style>
        body { font-family: system-ui, sans-serif; display: flex;
            justify-content: center; align-items: center;
            height: 100vh; margin: 0;
            background: #1a1a2e; color: #eee; }
        .container { text-align: center; }
        h1 { color: #00d9ff; }
        p { color: #aaa; }
        a { color: #00d9ff; }
        code { background: #2a2a3e; padding: 2px 8px; border-radius: 4px; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Photo Sorter Web UI</h1>
        <p>Frontend is not built yet. Run <code>make build-web</code> to build the frontend.</p>
        <p>API is available at <a href="/api/v1/health">/api/v1/health</a></p>
    </div>
</body>
</html>`))
}
