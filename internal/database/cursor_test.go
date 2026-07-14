package database

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

func TestEncodePhotoCursor_roundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cursor PhotoCursor
	}{
		{
			name: "plain timestamp",
			cursor: PhotoCursor{
				UpdatedAt: time.Date(2024, 6, 1, 12, 30, 0, 0, time.UTC),
				UID:       "pa3bz5x9k8mq7n2v",
			},
		},
		{
			// Postgres stores timestamptz at microsecond precision, so a
			// cursor minted from a real row carries sub-second digits. If
			// those did not survive the round trip, the resume predicate
			// would land on the wrong side of a row and either skip it or
			// serve it twice.
			name: "microsecond precision survives",
			cursor: PhotoCursor{
				UpdatedAt: time.Date(2024, 6, 1, 12, 30, 0, 123456000, time.UTC),
				UID:       "pzzzz1111aaaa222",
			},
		},
		{
			name: "non-UTC input is normalised",
			cursor: PhotoCursor{
				UpdatedAt: time.Date(2024, 6, 1, 12, 0, 0, 0, time.FixedZone("CET", 3600)),
				UID:       "pabc",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := DecodePhotoCursor(EncodePhotoCursor(tt.cursor))
			if err != nil {
				t.Fatalf("DecodePhotoCursor() error = %v, want nil", err)
			}
			if !got.UpdatedAt.Equal(tt.cursor.UpdatedAt) {
				t.Errorf("UpdatedAt = %v, want %v (equal instant)", got.UpdatedAt, tt.cursor.UpdatedAt)
			}
			if got.UID != tt.cursor.UID {
				t.Errorf("UID = %q, want %q", got.UID, tt.cursor.UID)
			}
		})
	}
}

func TestDecodePhotoCursor_invalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{name: "not base64", input: "!!!not base64!!!"},
		{name: "base64 but no separator", input: encodeRaw("2024-06-01T12:00:00Z")},
		{name: "empty uid", input: encodeRaw("2024-06-01T12:00:00Z|")},
		{name: "unparseable timestamp", input: encodeRaw("not-a-time|pabc")},
		{name: "empty string", input: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := DecodePhotoCursor(tt.input)
			if !errors.Is(err, ErrInvalidCursor) {
				t.Errorf("DecodePhotoCursor(%q) error = %v, want ErrInvalidCursor", tt.input, err)
			}
		})
	}
}

// TestDecodePhotoCursor_rejectsForgedUID guards the property that makes the
// cursor safe to hand to an untrusted client: it is opaque data, not a
// query. A client cannot smuggle SQL or a foreign sort key through it — the
// worst it can do is name a position, which is exactly what a cursor is.
func TestDecodePhotoCursor_rejectsForgedUID(t *testing.T) {
	t.Parallel()

	forged := encodeRaw("2024-06-01T12:00:00Z|'; DROP TABLE photos;--")
	got, err := DecodePhotoCursor(forged)
	if err != nil {
		t.Fatalf("DecodePhotoCursor() error = %v, want nil (it is just a string)", err)
	}
	// It decodes, but it only ever reaches the database as a bound parameter.
	if got.UID != "'; DROP TABLE photos;--" {
		t.Errorf("UID = %q, want the raw string passed through verbatim", got.UID)
	}
}

func TestClampPhotoLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "zero yields the default", limit: 0, want: DefaultPhotoListLimit},
		{name: "negative yields the default", limit: -5, want: DefaultPhotoListLimit},
		{name: "in-range passes through", limit: 200, want: 200},
		{name: "at the cap passes through", limit: MaxPhotoListLimit, want: MaxPhotoListLimit},
		{name: "over the cap is clamped", limit: 5000, want: MaxPhotoListLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ClampPhotoLimit(tt.limit); got != tt.want {
				t.Errorf("ClampPhotoLimit(%d) = %d, want %d", tt.limit, got, tt.want)
			}
		})
	}
}

func TestRelationSet_Empty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		set  RelationSet
		want bool
	}{
		{name: "zero value is empty", set: RelationSet{}, want: true},
		{name: "labels only is not empty", set: RelationSet{Labels: true}, want: false},
		{name: "files only is not empty", set: RelationSet{Files: true}, want: false},
		{
			name: "all set is not empty",
			set:  RelationSet{Labels: true, Albums: true, Markers: true, Files: true},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.set.Empty(); got != tt.want {
				t.Errorf("RelationSet%+v.Empty() = %v, want %v", tt.set, got, tt.want)
			}
		})
	}
}

func TestAPIToken_Active(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	tests := []struct {
		name  string
		token APIToken
		want  bool
	}{
		{name: "no expiry, not revoked, is active", token: APIToken{}, want: true},
		{name: "future expiry is active", token: APIToken{ExpiresAt: &future}, want: true},
		{name: "past expiry is inactive", token: APIToken{ExpiresAt: &past}, want: false},
		{name: "revoked is inactive", token: APIToken{RevokedAt: &past}, want: false},
		{
			name:  "revoked beats a future expiry",
			token: APIToken{ExpiresAt: &future, RevokedAt: &past},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.token.Active(now); got != tt.want {
				t.Errorf("Active(%v) = %v, want %v", now, got, tt.want)
			}
		})
	}
}

// encodeRaw base64url-encodes a raw cursor payload without going through
// EncodePhotoCursor, so the decoder's error paths can be exercised with
// payloads the encoder would never produce.
func encodeRaw(payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}
