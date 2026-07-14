package handlers

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// includeLabels / includeAlbums / includeMarkers / includeFiles are the
// accepted values of the ?include= query parameter.
const (
	includeLabels  = "labels"
	includeAlbums  = "albums"
	includeMarkers = "markers"
	includeFiles   = "files"
)

// resolveRelationReader returns a PhotoRelationReader or writes a 503 to w.
// Honours a pre-injected reader on the handler (tests) before falling back to
// the registered Postgres backend — the same lazy-resolution shape as
// resolveBrowseReader.
func (h *PhotosHandler) resolveRelationReader(w http.ResponseWriter) database.PhotoRelationReader {
	if h.relations != nil {
		return h.relations
	}
	reader, err := database.GetPhotoRelationReader(context.Background())
	if err != nil {
		log.Printf("photos relations: %v", err)
		respondError(w, http.StatusServiceUnavailable, "photo storage not available")
		return nil
	}
	h.relations = reader
	return reader
}

// parseInclude parses the ?include=labels,albums,markers,files parameter into
// a RelationSet. Unknown names are rejected with a 400 rather than ignored:
// silently dropping a misspelled "marker" would hand an importer a photo with
// no markers and no indication that it asked for the wrong thing.
//
// On invalid input it writes the error response and returns ok=false, matching
// the convention of the other parse helpers in this package.
func parseInclude(w http.ResponseWriter, raw string) (database.RelationSet, bool) {
	var set database.RelationSet
	if strings.TrimSpace(raw) == "" {
		return set, true
	}
	for name := range strings.SplitSeq(raw, ",") {
		switch strings.TrimSpace(name) {
		case includeLabels:
			set.Labels = true
		case includeAlbums:
			set.Albums = true
		case includeMarkers:
			set.Markers = true
		case includeFiles:
			set.Files = true
		case "":
			continue // tolerate "labels,,albums" and a trailing comma
		default:
			respondError(w, http.StatusBadRequest,
				"invalid include (want any of: labels, albums, markers, files)")
			return set, false
		}
	}
	return set, true
}

// applyCursorParam decodes the ?cursor= parameter onto the filter.
//
// A cursor is only meaningful under sort=updated — that is the only ordering
// whose sort key is the (updated_at, uid) pair the cursor encodes. Rather
// than silently ignoring the cursor under any other sort (which would hand a
// client page 1 forever, an infinite export loop), we reject the combination.
func applyCursorParam(
	w http.ResponseWriter, raw string, filter *database.PhotoFilter,
) bool {
	if raw == "" {
		return true
	}
	if filter.SortBy != database.SortUpdated {
		respondError(w, http.StatusBadRequest, "cursor requires sort=updated")
		return false
	}
	cursor, err := database.DecodePhotoCursor(raw)
	if err != nil {
		// DecodePhotoCursor only ever fails with ErrInvalidCursor, and it is
		// always the client's input at fault — never a 500.
		respondError(w, http.StatusBadRequest, "invalid cursor")
		return false
	}
	filter.Cursor = &cursor
	return true
}

// expandRelations loads the requested relations for a page of photos and
// attaches them to the corresponding responses in place.
//
// It is a no-op when nothing was requested, so the common UI path costs
// nothing. Otherwise it issues one query per requested relation for the
// entire page — never one per photo.
func (h *PhotosHandler) expandRelations(
	ctx context.Context, w http.ResponseWriter,
	photos []database.Photo, responses []PhotoResponse, include database.RelationSet,
) bool {
	if include.Empty() || len(photos) == 0 {
		return true
	}
	reader := h.resolveRelationReader(w)
	if reader == nil {
		return false
	}

	uids := make([]string, 0, len(photos))
	for i := range photos {
		uids = append(uids, photos[i].UID)
	}

	rels, err := reader.LoadPhotoRelations(ctx, uids, include)
	if err != nil {
		log.Printf("photos relations: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to load photo relations")
		return false
	}
	for i := range responses {
		attachRelations(&responses[i], rels[responses[i].UID])
	}
	return true
}

// nextCursorFor returns the keyset cursor a client should send to fetch the
// page after this one, or "" when the walk is complete.
//
// The cursor is minted from the last row of the page, and only when the page
// came back full: a short page means the server had nothing more to give, and
// handing back a cursor there would invite a client to poll forever. Note the
// converse is not a bug — a full final page yields a cursor whose next fetch
// returns zero rows and no cursor, which terminates the loop one request
// later.
//
// "Full" is measured against database.ClampPhotoLimit, i.e. the page size the
// repository actually applied, not the one the client asked for. A client
// requesting limit=1000 gets 500 rows back; comparing against 1000 would read
// that full page as short and end the export at 500 photos out of 20,310.
func nextCursorFor(photos []database.Photo, filter database.PhotoFilter) string {
	if filter.SortBy != database.SortUpdated || len(photos) == 0 {
		return ""
	}
	if len(photos) < database.ClampPhotoLimit(filter.Limit) {
		return ""
	}
	last := photos[len(photos)-1]
	return database.EncodePhotoCursor(database.PhotoCursor{
		UpdatedAt: last.UpdatedAt,
		UID:       last.UID,
	})
}
