// Package handlers contains the HTTP handlers for the photo-sorter web API.
//
// The share handler lives here in two halves: the authenticated half
// (ShareHandler.CreateLink / ListLinks / Revoke) which an admin/editor uses
// to mint or revoke a public link, and the public half (ShareHandler.Get /
// VerifyPassword / ListPhotos / Thumbnail / Download) which an anonymous
// recipient hits through `/api/v1/public/share/{slug}/...`. The public
// handlers must never require a session cookie — they are mounted outside
// the session middleware in routes.go.
package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/audit"
	"github.com/kozaktomas/photo-sorter/internal/auth"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/kozaktomas/photo-sorter/internal/storage"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

const (
	// shareCookiePrefix is prepended to a share's slug to derive the per-
	// share authentication cookie name. The cookie's value is a
	// signed token issued by VerifyPassword and re-checked on every
	// subsequent request to a protected share.
	shareCookiePrefix = "share_"

	// shareCookieDuration is the lifetime of the unlocked-share cookie. The
	// spec asks for ~24h; we use exactly 24h so the recipient does not have
	// to re-enter the password every page-reload.
	shareCookieDuration = 24 * time.Hour

	// sharePublicPhotosDefaultLimit is the default page size for
	// /public/share/{slug}/photos and the upper bound on the `limit` query
	// parameter. The recipient does not need to page through tens of
	// thousands of photos in one request.
	sharePublicPhotosDefaultLimit = 200

	// sharePublicPhotosMaxLimit caps the user-supplied `limit` query
	// parameter so a single share link cannot be turned into a heavy data-
	// export endpoint.
	sharePublicPhotosMaxLimit = 1000

	// shareRateLimitWindow + shareRateLimitMax form the in-memory rate
	// limiter applied to /public/share/{slug}/verify: at most 10 attempts
	// from the same IP per 5 minutes. The spec's wording.
	shareRateLimitWindow = 5 * time.Minute
	shareRateLimitMax    = 10
)

// errShareLinkInvalidExpires is returned when the request body's
// expires_at field is set but cannot be parsed as RFC3339 or is in the
// past.
var errShareLinkInvalidExpires = errors.New("expires_at must be a future RFC3339 timestamp")

// ShareHandler bundles both the authenticated and public share
// endpoints. It depends on the share-link repo, the album repo (for
// metadata lookups), and the photo repo + storage (for the public
// photo/thumb/download endpoints).
type ShareHandler struct {
	config         *config.Config
	sessionManager *middleware.SessionManager
	shareRepo      database.ShareLinkWriter
	albumRepo      database.AlbumReader
	photoRepo      database.PhotoReader
	store          *storage.Storage

	limiter *shareRateLimiter
}

// NewShareHandler constructs a ShareHandler. Any of the repository or
// storage dependencies may be nil — in that case the handler endpoints
// return 503 instead of nil-derefing. Tests rely on this leniency.
func NewShareHandler(
	cfg *config.Config,
	sm *middleware.SessionManager,
	shareRepo database.ShareLinkWriter,
	albumRepo database.AlbumReader,
	photoRepo database.PhotoReader,
	store *storage.Storage,
) *ShareHandler {
	return &ShareHandler{
		config:         cfg,
		sessionManager: sm,
		shareRepo:      shareRepo,
		albumRepo:      albumRepo,
		photoRepo:      photoRepo,
		store:          store,
		limiter:        newShareRateLimiter(shareRateLimitMax, shareRateLimitWindow),
	}
}

// ShareLinkResponse is the wire shape returned by every share endpoint.
// PasswordHash is never serialised — the only password-related signal
// downstream consumers get is HasPassword.
type ShareLinkResponse struct {
	Slug             string  `json:"slug"`
	AlbumUID         string  `json:"album_uid"`
	HasPassword      bool    `json:"has_password"`
	ExpiresAt        *string `json:"expires_at"`
	CreatedAt        string  `json:"created_at"`
	CreatedByUserUID string  `json:"created_by_user_uid"`
	URL              string  `json:"url"`
}

func shareLinkToResponse(l database.ShareLink, baseURL string) ShareLinkResponse {
	resp := ShareLinkResponse{
		Slug:             l.Slug,
		AlbumUID:         l.AlbumUID,
		HasPassword:      l.HasPassword(),
		CreatedAt:        l.CreatedAt.UTC().Format(time.RFC3339),
		CreatedByUserUID: l.CreatedByUserUID,
		URL:              baseURL + "/share/" + l.Slug,
	}
	if l.ExpiresAt != nil {
		exp := l.ExpiresAt.UTC().Format(time.RFC3339)
		resp.ExpiresAt = &exp
	}
	return resp
}

// requireShareWriter writes a 503 when the repo is unavailable and
// returns nil so the caller can early-out.
func (h *ShareHandler) requireShareWriter(w http.ResponseWriter) database.ShareLinkWriter {
	if h.shareRepo != nil {
		return h.shareRepo
	}
	respondError(w, http.StatusServiceUnavailable, "share storage not available")
	return nil
}

func (h *ShareHandler) requireAlbumReader(w http.ResponseWriter) database.AlbumReader {
	if h.albumRepo != nil {
		return h.albumRepo
	}
	respondError(w, http.StatusServiceUnavailable, "album storage not available")
	return nil
}

func (h *ShareHandler) requirePhotoReader(w http.ResponseWriter) database.PhotoReader {
	if h.photoRepo != nil {
		return h.photoRepo
	}
	respondError(w, http.StatusServiceUnavailable, "photo storage not available")
	return nil
}

func (h *ShareHandler) requireStorage(w http.ResponseWriter) *storage.Storage {
	if h.store != nil {
		return h.store
	}
	respondError(w, http.StatusServiceUnavailable, "photo storage not available")
	return nil
}

// --- Auth side ----------------------------------------------------------

// createShareRequest is the JSON body accepted by POST
// /albums/{uid}/share. All fields are optional. ExpiresAt is parsed as
// RFC3339; an empty/absent value means "no expiration".
type createShareRequest struct {
	Slug      string `json:"slug"`
	Password  string `json:"password"`
	ExpiresAt string `json:"expires_at"`
}

// CreateLink handles POST /api/v1/albums/{uid}/share. It validates the
// album exists, normalises the slug, bcrypt-hashes the password (if
// any), and inserts the row. The response shape is identical to GET
// /albums/{uid}/shares so the frontend can append-on-create without
// re-fetching.
func (h *ShareHandler) CreateLink(w http.ResponseWriter, r *http.Request) {
	deps, ok := h.prepareCreateLink(w, r)
	if !ok {
		return
	}
	link, slugWasGenerated, ok := h.buildCreateLink(w, r, deps)
	if !ok {
		return
	}
	if err := h.insertShareLinkWithRetry(r, deps.shareRepo, link, slugWasGenerated); err != nil {
		h.respondCreateError(w, link.Slug, err)
		return
	}
	audit.FromContext(r.Context()).Log(
		r.Context(), audit.ActionShareLinkCreate, audit.EntityShareLink, link.Slug,
		map[string]any{
			"album_uid":    link.AlbumUID,
			"has_password": link.PasswordHash != "",
			"expires_at":   formatNullableTime(link.ExpiresAt),
		},
	)
	respondJSON(w, http.StatusCreated, shareLinkToResponse(*link, ""))
}

// shareSlugAllocMaxAttempts caps the number of auto-derived slug
// candidates the server will try before giving up. The bound exists so
// an adversarial album title (or runaway concurrency) cannot turn
// CreateLink into an unbounded loop hammering the database. 10000 is
// well above any realistic collision rate but still fast to exhaust.
const shareSlugAllocMaxAttempts = 10000

// insertShareLinkWithRetry inserts the share link, retrying with the
// next "-N" suffix on a primary-key collision when the slug was auto-
// derived from the album title. Replaces the previous SELECT-then-
// INSERT scheme, which raced under concurrent creates against the same
// album: two requests would both observe the candidate missing, both
// insert, and one would get a 409 the user did not deserve. The
// loop now treats the UNIQUE constraint as the source of truth and
// only retries when the caller did not pin a specific slug.
func (h *ShareHandler) insertShareLinkWithRetry(
	r *http.Request, repo database.ShareLinkWriter, link *database.ShareLink, slugWasGenerated bool,
) error {
	if !slugWasGenerated {
		if err := repo.CreateShareLink(r.Context(), link); err != nil {
			return fmt.Errorf("create share link: %w", err)
		}
		return nil
	}
	base := link.Slug
	for i := 1; i <= shareSlugAllocMaxAttempts; i++ {
		err := repo.CreateShareLink(r.Context(), link)
		if err == nil {
			return nil
		}
		if !errors.Is(err, database.ErrShareLinkSlugTaken) {
			return fmt.Errorf("create share link: %w", err)
		}
		next := nextShareSlugCandidate(base, i+1)
		if next == link.Slug {
			return fmt.Errorf("create share link: %w", err)
		}
		link.Slug = next
	}
	return fmt.Errorf("create share link: %w", database.ErrShareLinkSlugTaken)
}

// nextShareSlugCandidate produces the n-th candidate slug derived from
// base by appending "-N", trimming base if the suffix would push the
// total beyond the 64-byte column limit. The trim keeps the suffix
// readable (never lands on a trailing "-") so successive collisions
// still produce monotonically growing strings.
func nextShareSlugCandidate(base string, n int) string {
	if n < 2 {
		return base
	}
	suffix := "-" + strconv.Itoa(n)
	maxBase := postgres.ShareSlugMaxLen() - len(suffix)
	if maxBase <= 0 {
		return base
	}
	trimmed := base
	if len(trimmed) > maxBase {
		trimmed = strings.TrimRight(trimmed[:maxBase], "-")
	}
	if trimmed == "" {
		// Suffix-only candidates would violate the 3-char minimum; bail.
		return base
	}
	return trimmed + suffix
}

// formatNullableTime returns the RFC3339 representation of t, or empty
// string when t is nil. Used to record optional timestamps in audit
// metadata without exploding the column with "null" strings.
func formatNullableTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// createLinkDeps bundles the dependencies CreateLink needs after every
// auth + lookup check has passed.
type createLinkDeps struct {
	info      *middleware.AuthInfo
	shareRepo database.ShareLinkWriter
	album     *database.Album
}

// prepareCreateLink validates the URL parameter, auth role, and
// dependency availability for CreateLink. Returns false (after writing
// the response) on any failure.
func (h *ShareHandler) prepareCreateLink(
	w http.ResponseWriter, r *http.Request,
) (createLinkDeps, bool) {
	var deps createLinkDeps
	albumUID := chi.URLParam(r, "uid")
	if albumUID == "" {
		respondError(w, http.StatusBadRequest, "missing album UID")
		return deps, false
	}
	info := h.requireWriteAccess(w, r)
	if info == nil {
		return deps, false
	}
	shareRepo := h.requireShareWriter(w)
	if shareRepo == nil {
		return deps, false
	}
	albumRepo := h.requireAlbumReader(w)
	if albumRepo == nil {
		return deps, false
	}
	album, ok := h.loadAlbum(w, r, albumRepo, albumUID)
	if !ok {
		return deps, false
	}
	deps.info = info
	deps.shareRepo = shareRepo
	deps.album = album
	return deps, true
}

// buildCreateLink decodes the JSON body, resolves the slug, hashes the
// password, and parses the expiration. It returns the ready-to-insert
// ShareLink and a flag reporting whether the slug came from the album
// title (true) or was provided by the caller (false). The boolean
// drives whether insertShareLinkWithRetry will adjust the suffix on
// collision. Returns ok=false (after writing the response) on any
// validation failure.
func (h *ShareHandler) buildCreateLink(
	w http.ResponseWriter, r *http.Request, deps createLinkDeps,
) (*database.ShareLink, bool, bool) {
	var req createShareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return nil, false, false
	}
	slug, slugWasGenerated, status, errMsg := h.resolveShareSlug(req.Slug, deps.album.Title)
	if status != 0 {
		respondError(w, status, errMsg)
		return nil, false, false
	}
	passwordHash, ok := h.hashRequestPassword(w, req.Password)
	if !ok {
		return nil, false, false
	}
	expiresAt, ok := h.parseRequestExpiration(w, req.ExpiresAt)
	if !ok {
		return nil, false, false
	}
	return &database.ShareLink{
		Slug:             slug,
		AlbumUID:         deps.album.UID,
		PasswordHash:     passwordHash,
		ExpiresAt:        expiresAt,
		CreatedByUserUID: deps.info.UserUID,
	}, slugWasGenerated, true
}

// requireWriteAccess pulls AuthInfo and enforces HasWriteAccess. It
// writes the appropriate error response and returns nil when the caller
// is missing or read-only.
func (h *ShareHandler) requireWriteAccess(w http.ResponseWriter, r *http.Request) *middleware.AuthInfo {
	info := middleware.MustGetAuthInfo(r.Context(), w)
	if info == nil {
		return nil
	}
	if !auth.HasWriteAccess(info.Role) {
		respondError(w, http.StatusForbidden, "forbidden")
		return nil
	}
	return info
}

// loadAlbum fetches an album for the given UID, writing the response on
// error and returning false so the caller can early-out.
func (h *ShareHandler) loadAlbum(
	w http.ResponseWriter, r *http.Request, repo database.AlbumReader, albumUID string,
) (*database.Album, bool) {
	album, err := repo.GetAlbum(r.Context(), albumUID)
	if errors.Is(err, database.ErrNotFound) {
		respondError(w, http.StatusNotFound, "album not found")
		return nil, false
	}
	if err != nil {
		log.Printf("share: album lookup %s: %v", sanitizeForLog(albumUID), err)
		respondError(w, http.StatusInternalServerError, "failed to load album")
		return nil, false
	}
	return album, true
}

// hashRequestPassword bcrypt-hashes a non-empty password from the
// request body. An empty password yields an empty hash, which the DB
// stores as NULL.
func (h *ShareHandler) hashRequestPassword(w http.ResponseWriter, plain string) (string, bool) {
	if plain == "" {
		return "", true
	}
	hash, err := auth.HashPassword(plain)
	if err != nil {
		log.Printf("share: hash password: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to hash password")
		return "", false
	}
	return hash, true
}

// parseRequestExpiration validates and parses the expires_at field of
// the create request. An empty string yields a nil time pointer.
func (h *ShareHandler) parseRequestExpiration(
	w http.ResponseWriter, raw string,
) (*time.Time, bool) {
	if strings.TrimSpace(raw) == "" {
		return nil, true
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil || !t.After(time.Now()) {
		respondError(w, http.StatusBadRequest, errShareLinkInvalidExpires.Error())
		return nil, false
	}
	t = t.UTC()
	return &t, true
}

// respondCreateError maps the typed errors returned by
// CreateShareLink onto HTTP responses.
func (h *ShareHandler) respondCreateError(w http.ResponseWriter, slug string, err error) {
	switch {
	case errors.Is(err, database.ErrShareLinkSlugTaken):
		respondError(w, http.StatusConflict, "slug already in use")
	case errors.Is(err, database.ErrShareLinkInvalidSlug):
		respondError(w, http.StatusBadRequest, database.ErrShareLinkInvalidSlug.Error())
	default:
		log.Printf("share: create %s: %v", sanitizeForLog(slug), err)
		respondError(w, http.StatusInternalServerError, "failed to create share link")
	}
}

// resolveShareSlug normalises a share slug for a create request. A
// caller-supplied value is accepted only when it matches the canonical
// `^[a-z0-9-]{3,64}$` pattern; otherwise the slug is derived from the
// album title and the second return value (generated) is set so
// insertShareLinkWithRetry knows it may append "-N" on collision. The
// previous SELECT-then-INSERT scheme has been retired — the UNIQUE
// constraint on the slug column is now the only race-resistant source
// of truth, see [insertShareLinkWithRetry].
func (h *ShareHandler) resolveShareSlug(
	requested, albumTitle string,
) (slug string, generated bool, status int, msg string) {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		if !postgres.IsValidShareSlug(requested) {
			return "", false, http.StatusBadRequest, database.ErrShareLinkInvalidSlug.Error()
		}
		return requested, false, 0, ""
	}
	return postgres.SlugifyShareTitle(albumTitle), true, 0, ""
}

// ListLinks handles GET /api/v1/albums/{uid}/shares. It returns every
// active share link pointing at the album. Expired links are still
// included so the operator can revoke them — the public side handles the
// 410.
func (h *ShareHandler) ListLinks(w http.ResponseWriter, r *http.Request) {
	albumUID := chi.URLParam(r, "uid")
	if albumUID == "" {
		respondError(w, http.StatusBadRequest, "missing album UID")
		return
	}
	info := middleware.MustGetAuthInfo(r.Context(), w)
	if info == nil {
		return
	}
	if !auth.HasWriteAccess(info.Role) {
		respondError(w, http.StatusForbidden, "forbidden")
		return
	}
	shareRepo := h.requireShareWriter(w)
	if shareRepo == nil {
		return
	}
	links, err := shareRepo.ListShareLinksForAlbum(r.Context(), albumUID)
	if err != nil {
		log.Printf("share: list %s: %v", sanitizeForLog(albumUID), err)
		respondError(w, http.StatusInternalServerError, "failed to list share links")
		return
	}
	resp := make([]ShareLinkResponse, 0, len(links))
	for _, l := range links {
		resp = append(resp, shareLinkToResponse(l, ""))
	}
	respondJSON(w, http.StatusOK, map[string]any{"links": resp})
}

// RevokeLink handles DELETE /api/v1/shares/{slug}. The link is hard-
// deleted; cookies issued for it stay valid for the rest of their 24h
// life but every subsequent request returns 404.
func (h *ShareHandler) RevokeLink(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		respondError(w, http.StatusBadRequest, "missing slug")
		return
	}
	info := middleware.MustGetAuthInfo(r.Context(), w)
	if info == nil {
		return
	}
	if !auth.HasWriteAccess(info.Role) {
		respondError(w, http.StatusForbidden, "forbidden")
		return
	}
	shareRepo := h.requireShareWriter(w)
	if shareRepo == nil {
		return
	}
	if err := shareRepo.DeleteShareLink(r.Context(), slug); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			respondError(w, http.StatusNotFound, "share link not found")
			return
		}
		log.Printf("share: revoke %s: %v", sanitizeForLog(slug), err)
		respondError(w, http.StatusInternalServerError, "failed to revoke share link")
		return
	}
	audit.FromContext(r.Context()).Log(
		r.Context(), audit.ActionShareLinkRevoke, audit.EntityShareLink, slug, nil,
	)
	respondJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// --- Public side -------------------------------------------------------

// publicAlbumPayload is the trimmed album view served by the public
// share endpoints. It omits every user-identifying or PhotoPrism-era
// field — the recipient does not need to know who minted the link.
type publicAlbumPayload struct {
	Title         string `json:"title"`
	PhotoCount    int    `json:"photo_count"`
	CoverThumbURL string `json:"cover_thumb_url"`
}

// publicShareGetResponse is the JSON body of GET /public/share/{slug}.
// When the link is password-protected and the request lacks a valid
// share cookie, Album is omitted (the recipient must verify first).
type publicShareGetResponse struct {
	Slug        string              `json:"slug"`
	HasPassword bool                `json:"has_password"`
	ExpiresAt   *string             `json:"expires_at"`
	Album       *publicAlbumPayload `json:"album,omitempty"`
}

// Get handles GET /api/v1/public/share/{slug}. It is the public-side
// metadata endpoint the frontend hits on page load to decide whether to
// render the password gate or the gallery.
func (h *ShareHandler) Get(w http.ResponseWriter, r *http.Request) {
	link, ok := h.loadPublicLink(w, r)
	if !ok {
		return
	}

	resp := publicShareGetResponse{
		Slug:        link.Slug,
		HasPassword: link.HasPassword(),
	}
	if link.ExpiresAt != nil {
		exp := link.ExpiresAt.UTC().Format(time.RFC3339)
		resp.ExpiresAt = &exp
	}

	// Password-protected and not yet verified — hide album metadata.
	if link.HasPassword() && !h.hasVerifiedCookie(r, link.Slug) {
		respondJSON(w, http.StatusOK, resp)
		return
	}

	albumRepo := h.requireAlbumReader(w)
	if albumRepo == nil {
		return
	}
	album, err := albumRepo.GetAlbum(r.Context(), link.AlbumUID)
	if errors.Is(err, database.ErrNotFound) {
		// The album was deleted but the link survives (cascade should
		// have caught this, but treat as gone-anyway). Respond 404.
		respondError(w, http.StatusNotFound, "share not found")
		return
	}
	if err != nil {
		log.Printf("share public get %s: %v", sanitizeForLog(link.Slug), err)
		respondError(w, http.StatusInternalServerError, "failed to load share")
		return
	}

	resp.Album = &publicAlbumPayload{
		Title:      album.Title,
		PhotoCount: h.countPublicPhotos(r, link.AlbumUID),
	}
	if album.CoverPhotoUID != "" && h.coverIsPublic(r, link.AlbumUID, album.CoverPhotoUID) {
		resp.Album.CoverThumbURL = publicThumbURL(link.Slug, album.CoverPhotoUID, "fit_720")
	}
	respondJSON(w, http.StatusOK, resp)
}

// countPublicPhotos returns the number of photos in the album that are
// safe to surface through the public share endpoints (archived and
// private photos are excluded). On error it logs the failure and falls
// back to 0 so the request still succeeds — the caller is anonymous and
// gains nothing from a 500 here.
func (h *ShareHandler) countPublicPhotos(r *http.Request, albumUID string) int {
	if h.photoRepo == nil {
		return 0
	}
	_, total, err := h.photoRepo.ListPhotos(r.Context(), publicPhotoFilter(albumUID, 1, 0))
	if err != nil {
		log.Printf("share public count %s: %v", sanitizeForLog(albumUID), err)
		return 0
	}
	return total
}

// coverIsPublic reports whether the album's stored cover photo is
// eligible to be exposed via the public thumbnail endpoint. A cover that
// is archived or marked private would otherwise leak — the thumbnail
// route would refuse to serve it but the recipient would see a broken
// image and learn the UID. Returning false here suppresses the URL
// entirely.
func (h *ShareHandler) coverIsPublic(r *http.Request, albumUID, photoUID string) bool {
	if h.photoRepo == nil {
		return false
	}
	photo, err := h.photoRepo.GetPhoto(r.Context(), photoUID)
	if err != nil {
		// ErrNotFound or a transient failure: drop the cover silently.
		return false
	}
	if !photoIsPublic(photo) {
		return false
	}
	uids, err := h.albumRepo.ListAlbumPhotoUIDs(r.Context(), albumUID)
	if err != nil {
		return false
	}
	return slices.Contains(uids, photoUID)
}

// publicPhotoFilter returns a PhotoFilter that excludes archived and
// private photos so the same gate applies to every public surface
// (listing, count, membership check). The function is a single source
// of truth for what counts as "visible to an anonymous share recipient".
func publicPhotoFilter(albumUID string, limit, offset int) database.PhotoFilter {
	private := false
	archived := false
	return database.PhotoFilter{
		AlbumUID: albumUID,
		Limit:    limit,
		Offset:   offset,
		SortBy:   "newest",
		Private:  &private,
		Archived: &archived,
	}
}

// photoIsPublic reports whether the given photo row may be exposed
// through a public share. Archived (soft-deleted) and private photos
// are always hidden. The check is the per-photo counterpart of
// publicPhotoFilter so the listing, thumbnail, and download endpoints
// stay in agreement.
func photoIsPublic(photo *database.Photo) bool {
	if photo == nil {
		return false
	}
	if photo.Private {
		return false
	}
	if photo.ArchivedAt != nil {
		return false
	}
	return true
}

// verifyRequest is the JSON body accepted by POST
// /public/share/{slug}/verify.
type verifyRequest struct {
	Password string `json:"password"`
}

// VerifyPassword handles POST /api/v1/public/share/{slug}/verify. It
// applies an in-memory per-IP rate limit, compares the supplied
// password against the bcrypt hash, and on success sets an HttpOnly,
// 24h-lived signed cookie keyed by the slug.
func (h *ShareHandler) VerifyPassword(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		respondError(w, http.StatusBadRequest, "missing slug")
		return
	}
	if !h.checkVerifyRateLimit(w, r, slug) {
		return
	}
	link, ok := h.loadVerifyLink(w, r, slug)
	if !ok {
		return
	}
	if !link.HasPassword() {
		// Already public — no need to verify.
		respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	var req verifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if !auth.CheckPassword(req.Password, link.PasswordHash) {
		audit.FromContext(r.Context()).LogAnonymous(
			r.Context(), audit.ActionShareLinkPasswordFailed,
			audit.EntityShareLink, link.Slug, "",
			map[string]any{"album_uid": link.AlbumUID},
		)
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid password"})
		return
	}

	h.setShareCookie(w, r, link.Slug)
	audit.FromContext(r.Context()).LogAnonymous(
		r.Context(), audit.ActionShareLinkPasswordVerify,
		audit.EntityShareLink, link.Slug, "",
		map[string]any{"album_uid": link.AlbumUID},
	)
	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// checkVerifyRateLimit consults the in-memory limiter and writes a 429
// response when the per-(IP, slug) attempt budget is exhausted. Keying
// on the tuple (instead of IP alone) means an attacker exhausting one
// share link does not lock the same IP out of every other share, and
// an attacker rotating slugs to dodge the limit still hits the cap on
// the slug they are actually trying to crack.
func (h *ShareHandler) checkVerifyRateLimit(w http.ResponseWriter, r *http.Request, slug string) bool {
	if h.limiter == nil {
		return true
	}
	key := shareRateLimitKey(clientIP(r), slug)
	retryAfter, blocked := h.limiter.allow(key, time.Now())
	if !blocked {
		return true
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
	respondError(w, http.StatusTooManyRequests, "too many attempts")
	return false
}

// shareRateLimitKey builds the limiter bucket key from the client IP
// and the slug being verified. The `|` separator cannot appear in a
// valid slug (the slug pattern is `^[a-z0-9-]{3,64}$`) so the
// concatenation is unambiguous and an IP cannot be conflated with a
// slug.
func shareRateLimitKey(ip, slug string) string {
	return ip + "|" + slug
}

// loadVerifyLink is the slug -> ShareLink lookup used by VerifyPassword.
// It writes the 404/410/500 responses on error.
func (h *ShareHandler) loadVerifyLink(
	w http.ResponseWriter, r *http.Request, slug string,
) (*database.ShareLink, bool) {
	shareRepo := h.requireShareWriter(w)
	if shareRepo == nil {
		return nil, false
	}
	link, err := shareRepo.GetShareLink(r.Context(), slug)
	if errors.Is(err, database.ErrNotFound) {
		respondError(w, http.StatusNotFound, "share not found")
		return nil, false
	}
	if err != nil {
		log.Printf("share verify lookup %s: %v", sanitizeForLog(slug), err)
		respondError(w, http.StatusInternalServerError, "failed to load share")
		return nil, false
	}
	if link.IsExpired(time.Now()) {
		respondError(w, http.StatusGone, "share link expired")
		return nil, false
	}
	return link, true
}

// publicPhoto is the wire shape returned by ListPhotos.
type publicPhoto struct {
	UID      string  `json:"uid"`
	Title    string  `json:"title"`
	TakenAt  *string `json:"taken_at"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	ThumbURL string  `json:"thumb_url"`
}

// ListPhotos handles GET /api/v1/public/share/{slug}/photos. It returns
// a paginated photo list (uid + dimensions + taken_at + title) for the
// linked album. Each entry includes a thumb_url for the public viewer.
func (h *ShareHandler) ListPhotos(w http.ResponseWriter, r *http.Request) {
	link, ok := h.loadPublicLink(w, r)
	if !ok {
		return
	}
	if !h.requireVerified(w, r, link) {
		return
	}
	photoRepo := h.requirePhotoReader(w)
	if photoRepo == nil {
		return
	}

	limit, offset := readPaginationParams(r,
		sharePublicPhotosDefaultLimit, sharePublicPhotosMaxLimit)
	photos, total, err := photoRepo.ListPhotos(r.Context(), publicPhotoFilter(link.AlbumUID, limit, offset))
	if err != nil {
		log.Printf("share public photos %s: %v", sanitizeForLog(link.Slug), err)
		respondError(w, http.StatusInternalServerError, "failed to list photos")
		return
	}

	items := make([]publicPhoto, 0, len(photos))
	for _, p := range photos {
		items = append(items, toPublicPhoto(link.Slug, p))
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"photos": items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// readPaginationParams parses limit/offset query parameters with safe
// clamping. Negative or non-numeric values fall back to the defaults.
func readPaginationParams(
	r *http.Request, defaultLimit, maxLimit int,
) (int, int) {
	limit := defaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}

// toPublicPhoto builds the wire view for one photo, including the
// per-share thumbnail URL and the RFC3339-formatted taken_at.
func toPublicPhoto(slug string, p database.Photo) publicPhoto {
	entry := publicPhoto{
		UID:      p.UID,
		Title:    p.Title,
		Width:    p.FileWidth,
		Height:   p.FileHeight,
		ThumbURL: publicThumbURL(slug, p.UID, "fit_720"),
	}
	if p.TakenAt != nil {
		t := p.TakenAt.UTC().Format(time.RFC3339)
		entry.TakenAt = &t
	}
	return entry
}

// Thumbnail handles GET
// /api/v1/public/share/{slug}/photos/{photo_uid}/thumb/{size}. It is
// the public mirror of PhotosHandler.Thumbnail with the share-link
// membership check layered on top.
func (h *ShareHandler) Thumbnail(w http.ResponseWriter, r *http.Request) {
	ctx, ok := h.preparePhotoRequest(w, r)
	if !ok {
		return
	}
	size := chi.URLParam(r, "size")
	if size == "" {
		respondError(w, http.StatusBadRequest, "missing size")
		return
	}
	if _, ok := storage.ValidThumbSizes[size]; !ok {
		respondError(w, http.StatusBadRequest, "invalid size")
		return
	}
	if ctx.photo.FileHash == "" {
		respondError(w, http.StatusNotFound, "photo has no thumbnail")
		return
	}
	rel, err := storage.ThumbRelPath(ctx.photo.FileHash, size)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid thumb path: "+err.Error())
		return
	}
	serveThumb(w, r, ctx.store, ctx.photo.FileHash, size, rel)
}

// sharePhotoContext bundles the per-request artefacts the public photo
// endpoints need: the validated link, the resolved photo, the photo
// reader (so callers can list files for download), and the on-disk
// store. preparePhotoRequest builds this in one go so each endpoint
// stays under the cyclomatic budget.
type sharePhotoContext struct {
	link  *database.ShareLink
	photo *database.Photo
	repo  database.PhotoReader
	store *storage.Storage
}

// preparePhotoRequest runs every check shared by the public Thumbnail
// and Download endpoints: load the link, gate on the share cookie if
// the link is protected, verify the photo is a member of the album,
// and resolve the storage. It writes the response on any failure and
// returns ok=false so the caller can early-out.
func (h *ShareHandler) preparePhotoRequest(
	w http.ResponseWriter, r *http.Request,
) (sharePhotoContext, bool) {
	var ctx sharePhotoContext
	link, ok := h.loadPublicLink(w, r)
	if !ok {
		return ctx, false
	}
	if !h.requireVerified(w, r, link) {
		return ctx, false
	}
	photoUID := chi.URLParam(r, "photo_uid")
	if photoUID == "" {
		respondError(w, http.StatusBadRequest, "missing photo UID")
		return ctx, false
	}
	if !h.photoBelongsToAlbum(w, r, photoUID, link.AlbumUID) {
		return ctx, false
	}
	repo := h.requirePhotoReader(w)
	if repo == nil {
		return ctx, false
	}
	store := h.requireStorage(w)
	if store == nil {
		return ctx, false
	}
	photo, ok := loadPhoto(w, r, repo, photoUID, "share-photo")
	if !ok {
		return ctx, false
	}
	// Belt-and-braces: a photo can be a member of the album but still
	// archived or marked private. The share recipient must not see it
	// even if they know the UID. Mirror the gate publicPhotoFilter
	// applies to the listing.
	if !photoIsPublic(photo) {
		respondError(w, http.StatusNotFound, "photo not found in share")
		return ctx, false
	}
	ctx.link = link
	ctx.photo = photo
	ctx.repo = repo
	ctx.store = store
	return ctx, true
}

// Download handles GET
// /api/v1/public/share/{slug}/photos/{photo_uid}/download. It streams
// the primary file of the requested photo as an attachment, with the
// same Range support as the authenticated download endpoint.
func (h *ShareHandler) Download(w http.ResponseWriter, r *http.Request) {
	ctx, ok := h.preparePhotoRequest(w, r)
	if !ok {
		return
	}
	rel, fileName, mime, err := resolvePrimaryFile(r.Context(), ctx.repo, ctx.photo)
	if err != nil {
		log.Printf("share download %s: %v", sanitizeForLog(ctx.photo.UID), err)
		respondError(w, http.StatusInternalServerError, "failed to resolve photo file")
		return
	}
	if rel == "" {
		respondError(w, http.StatusNotFound, "primary file not found")
		return
	}
	serveOriginal(w, r, ctx.store, rel, fileName, mime)
}

// --- Internal helpers --------------------------------------------------

// loadPublicLink fetches the share link by slug, applying the expired
// and not-found rules. Returns false when an error response has already
// been written.
func (h *ShareHandler) loadPublicLink(
	w http.ResponseWriter, r *http.Request,
) (*database.ShareLink, bool) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		respondError(w, http.StatusBadRequest, "missing slug")
		return nil, false
	}
	shareRepo := h.requireShareWriter(w)
	if shareRepo == nil {
		return nil, false
	}
	link, err := shareRepo.GetShareLink(r.Context(), slug)
	if errors.Is(err, database.ErrNotFound) {
		respondError(w, http.StatusNotFound, "share not found")
		return nil, false
	}
	if err != nil {
		log.Printf("share public load %s: %v", sanitizeForLog(slug), err)
		respondError(w, http.StatusInternalServerError, "failed to load share")
		return nil, false
	}
	if link.IsExpired(time.Now()) {
		respondError(w, http.StatusGone, "share link expired")
		return nil, false
	}
	return link, true
}

// requireVerified ensures that when the link is password-protected, the
// request carries a valid share cookie. Returns false (and writes 401)
// when verification is missing.
func (h *ShareHandler) requireVerified(
	w http.ResponseWriter, r *http.Request, link *database.ShareLink,
) bool {
	if !link.HasPassword() {
		return true
	}
	if h.hasVerifiedCookie(r, link.Slug) {
		return true
	}
	respondError(w, http.StatusUnauthorized, "password required")
	return false
}

// photoBelongsToAlbum returns true when the supplied photo UID is a
// member of the linked album. It writes a 404 response (and returns
// false) otherwise, so the share cookie cannot be used to fetch
// arbitrary photos.
func (h *ShareHandler) photoBelongsToAlbum(
	w http.ResponseWriter, r *http.Request, photoUID, albumUID string,
) bool {
	albumRepo := h.requireAlbumReader(w)
	if albumRepo == nil {
		return false
	}
	uids, err := albumRepo.ListAlbumPhotoUIDs(r.Context(), albumUID)
	if err != nil {
		log.Printf("share membership %s/%s: %v",
			sanitizeForLog(albumUID), sanitizeForLog(photoUID), err)
		respondError(w, http.StatusInternalServerError, "failed to verify membership")
		return false
	}
	if slices.Contains(uids, photoUID) {
		return true
	}
	respondError(w, http.StatusNotFound, "photo not found in share")
	return false
}

// hasVerifiedCookie returns true when the request carries a share cookie
// whose signed value matches the slug.
func (h *ShareHandler) hasVerifiedCookie(r *http.Request, slug string) bool {
	cookie, err := r.Cookie(shareCookiePrefix + slug)
	if err != nil {
		return false
	}
	return h.verifyShareToken(slug, cookie.Value)
}

// setShareCookie issues a per-share HttpOnly cookie containing an HMAC-
// signed token. The cookie name is `share_<slug>` so an unlock for one
// link does not unlock another, and the path is scoped to the slug's
// public sub-tree (with a trailing slash, so RFC 6265 path-match never
// bleeds the cookie onto a different share whose slug happens to share
// a prefix).
func (h *ShareHandler) setShareCookie(w http.ResponseWriter, r *http.Request, slug string) {
	token := h.signShareToken(slug)
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     shareCookiePrefix + slug,
		Value:    token,
		Path:     "/api/v1/public/share/" + slug + "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(shareCookieDuration.Seconds()),
	})
}

// signShareToken returns an HMAC-SHA256 token tied to the share slug and
// the session manager's signing secret. The token has no payload — the
// slug already lives in the cookie name — but it cannot be forged
// without the secret.
func (h *ShareHandler) signShareToken(slug string) string {
	mac := hmac.New(sha256.New, h.shareSecret())
	mac.Write([]byte("share|" + slug))
	return base64.URLEncoding.EncodeToString(mac.Sum(nil))
}

// verifyShareToken returns true when token is the HMAC of slug under the
// share secret. Constant-time comparison is used.
func (h *ShareHandler) verifyShareToken(slug, token string) bool {
	expected := h.signShareToken(slug)
	return hmac.Equal([]byte(expected), []byte(token))
}

// shareSecret returns the HMAC key used to sign share cookies. It
// reuses the configured session secret (via WEB_SESSION_SECRET) so
// rotating that secret invalidates outstanding share cookies along with
// outstanding sessions.
func (h *ShareHandler) shareSecret() []byte {
	if h.sessionManager == nil {
		return []byte("photo-sorter-share-dev-secret")
	}
	return h.sessionManager.Secret()
}

// publicThumbURL builds the public-facing thumbnail URL for a photo in
// the share. The URL is relative so a deployment behind a proxy keeps
// working without configuration.
func publicThumbURL(slug, photoUID, size string) string {
	return fmt.Sprintf("/api/v1/public/share/%s/photos/%s/thumb/%s", slug, photoUID, size)
}

// clientIP extracts the rate-limit key for a request. chi's RealIP
// middleware runs before us and rewrites r.RemoteAddr to the
// X-Forwarded-For / X-Real-IP / True-Client-IP value (in that order)
// when those headers are present, so the audit log, the chi access
// log, and this limiter all key on the same IP. We strip the trailing
// :port — RealIP only writes a bare IP, but a direct (non-proxied)
// client still arrives with host:port and the bucket key should be
// the IP alone so an attacker cycling source ports cannot dodge it.
//
// Reading X-Forwarded-For ourselves would re-introduce the bypass we
// disable in the audit middleware: an unproxied attacker can supply
// any value for that header.
func clientIP(r *http.Request) string {
	addr := r.RemoteAddr
	if addr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	if strings.HasPrefix(addr, "[") {
		if end := strings.Index(addr, "]"); end > 0 {
			return addr[1:end]
		}
	}
	return addr
}

// --- Rate limiter ------------------------------------------------------

// shareRateLimiter is a tiny in-memory sliding-window limiter. It is
// protected by a single mutex; the share verify endpoint is expected to
// see at most a few dozen requests per second, so a lock-free design is
// not worth the complexity. The map can grow unbounded for adversarial
// inputs, so we purge entries whose window has fully expired on every
// allow() call.
type shareRateLimiter struct {
	max    int
	window time.Duration

	mu     sync.Mutex
	events map[string][]time.Time
}

func newShareRateLimiter(maxAttempts int, window time.Duration) *shareRateLimiter {
	return &shareRateLimiter{
		max:    maxAttempts,
		window: window,
		events: make(map[string][]time.Time),
	}
}

// allow records an attempt for key and reports whether the limit has
// been exceeded. The returned duration is the remaining time the caller
// should wait before retrying.
func (l *shareRateLimiter) allow(key string, now time.Time) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	events := l.events[key]
	// Drop expired entries.
	kept := events[:0]
	for _, t := range events {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		retryAfter := max(kept[0].Add(l.window).Sub(now), time.Second)
		l.events[key] = kept
		return retryAfter, true
	}
	kept = append(kept, now)
	l.events[key] = kept
	return 0, false
}
