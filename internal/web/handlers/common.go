package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// errInvalidRequestBody is a shared error message for invalid JSON request bodies.
const errInvalidRequestBody = "invalid request body"

// roleViewer identifies the read-only role. Anything else (admin, editor, or
// the empty string left over from the PhotoPrism-era session) is allowed to
// mutate data.
const roleViewer = "viewer"

// errForbidden is returned by requireWriteRole when the current session's
// role is "viewer" and therefore lacks write permission.
var errForbidden = errors.New("forbidden")

// requireWriteRole returns errForbidden when the current session's role is
// "viewer". The check intentionally treats a missing session or empty role
// as admin so callers without an authenticated session (tests, MCP, CLI
// scripts) keep working until the native users table is wired in.
func requireWriteRole(r *http.Request) error {
	session := middleware.GetSessionFromContext(r.Context())
	if session == nil {
		return nil
	}
	if session.Role == roleViewer {
		return errForbidden
	}
	return nil
}

// sanitizeForLog removes newlines and carriage returns to prevent log injection.
func sanitizeForLog(s string) string {
	return strings.NewReplacer("\n", "", "\r", "").Replace(s)
}

// contentDispositionAttachment builds an RFC 6266 / RFC 5987 compliant
// Content-Disposition header value for a download response. It emits a
// quoted ASCII fallback in `filename=` (with any byte outside the safe
// HTTP-header set scrubbed) and, when the original contains non-ASCII or
// header-unsafe characters, an RFC 5987 percent-encoded `filename*=`
// parameter so modern browsers render the correct UTF-8 filename. The
// dual form defeats CRLF injection and gracefully handles user-supplied
// filenames containing quotes, control bytes, or non-ASCII characters.
func contentDispositionAttachment(name string) string {
	if name == "" {
		name = "download"
	}
	ascii := asciiSafeFilename(name)
	if needsRFC5987(name) {
		return fmt.Sprintf(
			"attachment; filename=%q; filename*=UTF-8''%s",
			ascii, rfc5987Encode(name),
		)
	}
	return fmt.Sprintf("attachment; filename=%q", ascii)
}

// asciiSafeFilename returns name with any byte outside printable ASCII
// (32-126) replaced by '_' and any quote / backslash / control character
// scrubbed. The result is always safe to embed inside an HTTP header
// double-quoted string.
func asciiSafeFilename(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for i := range len(name) {
		c := name[i]
		switch {
		case c < 32 || c == 127:
			b.WriteByte('_')
		case c == '"' || c == '\\':
			b.WriteByte('_')
		case c > 126:
			b.WriteByte('_')
		default:
			b.WriteByte(c)
		}
	}
	out := b.String()
	if out == "" {
		return "download"
	}
	return out
}

// needsRFC5987 reports whether name contains any byte that requires the
// RFC 5987 extended form to round-trip safely.
func needsRFC5987(name string) bool {
	for i := range len(name) {
		c := name[i]
		if c < 32 || c == 127 || c > 126 {
			return true
		}
		if c == '"' || c == '\\' {
			return true
		}
	}
	return false
}

// rfc5987Encode percent-encodes name per RFC 5987's `value-chars`
// production (a subset of RFC 3986 unreserved + a small allow-list of
// printable extras). Multi-byte UTF-8 sequences are encoded byte-by-byte
// so the receiver can reassemble the original Unicode string.
func rfc5987Encode(name string) string {
	const safe = "!#$&+-.^_`|~"
	var b strings.Builder
	b.Grow(len(name))
	for i := range len(name) {
		c := name[i]
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			strings.IndexByte(safe, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

// respondJSON sends a JSON response.
func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

// respondError sends an error response.
func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}

// getPhotoPrismClient creates a PhotoPrism client.
// If a session is provided, uses its tokens. Otherwise, authenticates with config credentials.
// This allows the API to work both with and without user sessions.
func getPhotoPrismClient(cfg *config.Config, session *middleware.Session) (*photoprism.PhotoPrism, error) {
	if session != nil && session.Token != "" {
		pp, err := photoprism.NewPhotoPrismFromToken(
			cfg.PhotoPrism.URL, session.Token, session.DownloadToken, session.UserUID,
		)
		if err != nil {
			return nil, fmt.Errorf("creating PhotoPrism client from token: %w", err)
		}
		return pp, nil
	}
	// No session - authenticate directly with config credentials.
	pp, err := photoprism.NewPhotoPrism(cfg.PhotoPrism.URL, cfg.PhotoPrism.Username, cfg.PhotoPrism.GetPassword())
	if err != nil {
		return nil, fmt.Errorf("creating PhotoPrism client: %w", err)
	}
	return pp, nil
}

// HealthCheck handles the health check endpoint.
func HealthCheck(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}
