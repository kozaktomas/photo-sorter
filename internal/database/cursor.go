package database

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidCursor is returned by DecodePhotoCursor when the supplied cursor
// is not a cursor this server minted (bad base64, wrong field count, or an
// unparseable timestamp). Callers should surface it as a 400, never a 500 —
// it is always caused by client input.
var ErrInvalidCursor = errors.New("invalid cursor")

// cursorSeparator splits the timestamp from the UID inside the decoded
// cursor payload. A photo UID is base32 (see postgres.NewPhotoUID) and an
// RFC3339Nano timestamp contains no "|", so the separator is unambiguous.
const cursorSeparator = "|"

const (
	// DefaultPhotoListLimit is the page size ListPhotos applies when
	// PhotoFilter.Limit is 0.
	DefaultPhotoListLimit = 50

	// MaxPhotoListLimit caps PhotoFilter.Limit. Larger values are clamped
	// down silently rather than rejected.
	MaxPhotoListLimit = 500
)

// ClampPhotoLimit returns the page size ListPhotos will actually apply for a
// requested limit.
//
// It lives here, shared, rather than being reimplemented in the handler:
// the handler needs the effective limit to decide whether a page came back
// full (and therefore whether to mint a next cursor). If its idea of the cap
// ever drifted from the repository's, a full page would be misread as a
// short one and the export would stop early, silently missing photos.
func ClampPhotoLimit(limit int) int {
	if limit <= 0 {
		return DefaultPhotoListLimit
	}
	if limit > MaxPhotoListLimit {
		return MaxPhotoListLimit
	}
	return limit
}

// PhotoCursor is a keyset position in the SortUpdated ordering — the
// (updated_at, uid) pair of the last row a client received.
//
// It exists because OFFSET pagination cannot survive a long export: rows
// shift under the offset as photos are written, so pages silently skip and
// repeat records. A keyset cursor addresses a *position in the sort order*
// rather than a count of rows, so concurrent writes cannot move it.
type PhotoCursor struct {
	UpdatedAt time.Time
	UID       string
}

// EncodePhotoCursor renders a cursor as an opaque, URL-safe string.
//
// The payload is deliberately opaque: clients must treat it as a token to
// echo back, not as a value to construct. Encoding both halves of the keyset
// (rather than just the UID, which the caller could look the timestamp up
// from) is what makes the cursor immune to the row it points at being
// modified mid-export — see DecodePhotoCursor for the failure that avoids.
func EncodePhotoCursor(c PhotoCursor) string {
	payload := c.UpdatedAt.UTC().Format(time.RFC3339Nano) + cursorSeparator + c.UID
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// DecodePhotoCursor parses a cursor produced by EncodePhotoCursor. It returns
// ErrInvalidCursor (wrapped, so errors.Is works) for any malformed input.
//
// Note what the self-contained encoding buys us. Had the cursor been just the
// last UID — with the server looking up that row's updated_at to resume — then
// a photo edited *after* the client paged past it would have its updated_at
// bumped to now(). The resume predicate would jump to that new timestamp and
// silently skip every photo in between. Carrying the timestamp in the cursor
// pins the resume point to where the client actually stopped.
func DecodePhotoCursor(s string) (PhotoCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return PhotoCursor{}, fmt.Errorf("%w: not base64url", ErrInvalidCursor)
	}
	ts, uid, found := strings.Cut(string(raw), cursorSeparator)
	if !found || uid == "" {
		return PhotoCursor{}, fmt.Errorf("%w: malformed payload", ErrInvalidCursor)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return PhotoCursor{}, fmt.Errorf("%w: bad timestamp", ErrInvalidCursor)
	}
	return PhotoCursor{UpdatedAt: updatedAt, UID: uid}, nil
}
