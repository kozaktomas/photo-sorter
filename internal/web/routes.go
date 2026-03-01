package web

import (
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/web/handlers"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
	"github.com/kozaktomas/photo-sorter/internal/web/static"
)

//nolint:funlen // Route registration is inherently long.
func (s *Server) setupRoutes(sessionManager *middleware.SessionManager) {
	// Create handlers.
	authHandler := handlers.NewAuthHandler(s.config, sessionManager)
	albumsHandler := handlers.NewAlbumsHandler(s.config, sessionManager)
	labelsHandler := handlers.NewLabelsHandler(s.config, sessionManager)
	photosHandler := handlers.NewPhotosHandler(s.config, sessionManager)
	sortHandler := handlers.NewSortHandler(s.config, sessionManager, s.jobManager)
	uploadHandler := handlers.NewUploadHandler(s.config, sessionManager)
	configHandler := handlers.NewConfigHandler(s.config)
	facesHandler := handlers.NewFacesHandler(s.config, sessionManager)
	statsHandler := handlers.NewStatsHandler(s.config, sessionManager)
	processHandler := handlers.NewProcessHandler(s.config, sessionManager, facesHandler, photosHandler, statsHandler)
	booksHandler := handlers.NewBooksHandler(s.config, sessionManager)

	// Health check (no auth required).
	s.router.Get("/api/v1/health", handlers.HealthCheck)

	// API routes.
	s.router.Route("/api/v1", func(r chi.Router) {
		// Auth routes (no PhotoPrism client needed for login).
		r.Post("/auth/login", authHandler.Login)
		r.Post("/auth/logout", authHandler.Logout)
		r.Get("/auth/status", authHandler.Status)

		// All other routes require authentication and get a PhotoPrism client injected.
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(sessionManager))
			r.Use(middleware.WithPhotoPrismClient(s.config))

			// Albums.
			r.Get("/albums", albumsHandler.List)
			r.Post("/albums", albumsHandler.Create)
			r.Get("/albums/{uid}", albumsHandler.Get)
			r.Get("/albums/{uid}/photos", albumsHandler.GetPhotos)
			r.Post("/albums/{uid}/photos", albumsHandler.AddPhotos)
			r.Delete("/albums/{uid}/photos", albumsHandler.ClearPhotos)
			r.Delete("/albums/{uid}/photos/batch", albumsHandler.RemovePhotos)

			// Labels.
			r.Get("/labels", labelsHandler.List)
			r.Get("/labels/{uid}", labelsHandler.Get)
			r.Put("/labels/{uid}", labelsHandler.Update)
			r.Delete("/labels", labelsHandler.BatchDelete)

			// Photos.
			r.Get("/photos", photosHandler.List)
			r.Get("/photos/{uid}", photosHandler.Get)
			r.Put("/photos/{uid}", photosHandler.Update)
			r.Get("/photos/{uid}/thumb/{size}", photosHandler.Thumbnail)
			r.Get("/photos/{uid}/faces", facesHandler.GetPhotoFaces)
			r.Post("/photos/{uid}/faces/compute", facesHandler.ComputeFaces)
			r.Get("/photos/{uid}/estimate-era", photosHandler.EstimateEra)
			r.Get("/photos/{uid}/books", booksHandler.GetPhotoBookMemberships)
			r.Post("/photos/similar", photosHandler.FindSimilar)
			r.Post("/photos/similar/collection", photosHandler.FindSimilarToCollection)
			r.Post("/photos/batch/labels", photosHandler.BatchAddLabels)
			r.Post("/photos/batch/edit", photosHandler.BatchEdit)
			r.Post("/photos/batch/archive", photosHandler.BatchArchive)
			r.Post("/photos/duplicates", photosHandler.FindDuplicates)
			r.Post("/photos/suggest-albums", photosHandler.SuggestAlbums)
			r.Post("/photos/search-by-text", photosHandler.SearchByText)

			// Sort (long-running operations).
			r.Post("/sort", sortHandler.Start)
			r.Get("/sort/{jobId}", sortHandler.Status)
			r.Get("/sort/{jobId}/events", sortHandler.Events)
			r.Delete("/sort/{jobId}", sortHandler.Cancel)

			// Upload.
			r.Post("/upload", uploadHandler.Upload)

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

			// Process (embeddings & face detection).
			r.Post("/process", processHandler.Start)
			r.Get("/process/{jobId}/events", processHandler.Events)
			r.Delete("/process/{jobId}", processHandler.Cancel)
			r.Post("/process/rebuild-index", processHandler.RebuildIndex)
			r.Post("/process/sync-cache", processHandler.SyncCache)

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
			r.Get("/books/{id}/export-pdf", booksHandler.ExportPDF)
		})
	})

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
