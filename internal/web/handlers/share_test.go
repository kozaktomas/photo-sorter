package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/kozaktomas/photo-sorter/internal/storage"
)

// fakeShareLinkRepo is an in-memory database.ShareLinkWriter for the
// share-handler tests. It honours the slug UNIQUE PK by returning
// ErrShareLinkSlugTaken on collision so the auto-retry path can be
// exercised without a database.
type fakeShareLinkRepo struct {
	mu    sync.Mutex
	links map[string]*database.ShareLink
}

func newFakeShareLinkRepo() *fakeShareLinkRepo {
	return &fakeShareLinkRepo{links: map[string]*database.ShareLink{}}
}

func (f *fakeShareLinkRepo) GetShareLink(_ context.Context, slug string) (*database.ShareLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	link, ok := f.links[slug]
	if !ok {
		return nil, database.ErrNotFound
	}
	cp := *link
	return &cp, nil
}

func (f *fakeShareLinkRepo) ListShareLinksForAlbum(
	_ context.Context, albumUID string,
) ([]database.ShareLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []database.ShareLink
	for _, l := range f.links {
		if l.AlbumUID == albumUID {
			out = append(out, *l)
		}
	}
	return out, nil
}

func (f *fakeShareLinkRepo) CreateShareLink(_ context.Context, link *database.ShareLink) error {
	if !postgres.IsValidShareSlug(link.Slug) {
		return database.ErrShareLinkInvalidSlug
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.links[link.Slug]; exists {
		return database.ErrShareLinkSlugTaken
	}
	cp := *link
	cp.CreatedAt = time.Now().UTC()
	f.links[link.Slug] = &cp
	link.CreatedAt = cp.CreatedAt
	return nil
}

func (f *fakeShareLinkRepo) DeleteShareLink(_ context.Context, slug string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.links[slug]; !ok {
		return database.ErrNotFound
	}
	delete(f.links, slug)
	return nil
}

func TestShareRateLimiter_AllowsUpToMax(t *testing.T) {
	limiter := newShareRateLimiter(3, time.Minute)
	now := time.Unix(1_700_000_000, 0)

	for i := range 3 {
		if retry, blocked := limiter.allow("1.2.3.4", now); blocked {
			t.Fatalf("attempt %d should be allowed, got blocked (retry=%v)", i, retry)
		}
	}
	retry, blocked := limiter.allow("1.2.3.4", now)
	if !blocked {
		t.Fatalf("4th attempt should be blocked")
	}
	if retry <= 0 {
		t.Fatalf("expected positive Retry-After, got %v", retry)
	}
}

func TestShareRateLimiter_WindowSlides(t *testing.T) {
	limiter := newShareRateLimiter(2, time.Minute)
	t0 := time.Unix(1_700_000_000, 0)
	_, _ = limiter.allow("ip", t0)
	_, _ = limiter.allow("ip", t0)
	if _, blocked := limiter.allow("ip", t0); !blocked {
		t.Fatalf("third attempt in window should be blocked")
	}
	// Move past the window — both prior attempts age out.
	if _, blocked := limiter.allow("ip", t0.Add(2*time.Minute)); blocked {
		t.Fatalf("attempt after window should be allowed")
	}
}

func TestShareRateLimiter_PerKeyIsolation(t *testing.T) {
	limiter := newShareRateLimiter(1, time.Minute)
	now := time.Unix(1_700_000_000, 0)
	if _, blocked := limiter.allow("a", now); blocked {
		t.Fatalf("first attempt for a should pass")
	}
	if _, blocked := limiter.allow("a", now); !blocked {
		t.Fatalf("second attempt for a should block")
	}
	if _, blocked := limiter.allow("b", now); blocked {
		t.Fatalf("first attempt for b should pass even when a is blocked")
	}
}

func TestIsValidShareSlug(t *testing.T) {
	cases := []struct {
		slug string
		ok   bool
	}{
		{"abc", true},
		{"summer-2025", true},
		{"a-b-c-d-1-2-3", true},
		{"ab", false},
		{"Bad-Case", false},
		{"with spaces", false},
		{"with_underscore", false},
		{"diacrítica", false},
		{"", false},
		{"verylongsluuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuug-over-64-chars", false},
	}
	for _, c := range cases {
		if got := postgres.IsValidShareSlug(c.slug); got != c.ok {
			t.Errorf("IsValidShareSlug(%q) = %v, want %v", c.slug, got, c.ok)
		}
	}
}

func TestSlugifyShareTitle(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"Summer 2025", "summer-2025"},
		{"Rodina v Praze", "rodina-v-praze"},
		{"Příliš žluťoučký kůň", "prilis-zlutoucky-kun"},
		{"!!!", "album"},
		{"a", "album"},
		{"Ab", "album"},
		{"Abc", "abc"},
	}
	for _, c := range cases {
		got := postgres.SlugifyShareTitle(c.title)
		if got != c.want {
			t.Errorf("SlugifyShareTitle(%q) = %q, want %q", c.title, got, c.want)
		}
	}
}

func TestShareLinkResponse_NoPasswordHashLeak(t *testing.T) {
	now := time.Now()
	exp := now.Add(7 * 24 * time.Hour)
	link := database.ShareLink{
		Slug:             "abcd-link",
		AlbumUID:         "a123",
		PasswordHash:     "$2a$12$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN.OP",
		ExpiresAt:        &exp,
		CreatedAt:        now,
		CreatedByUserUID: "u1",
	}
	resp := shareLinkToResponse(link, "https://example.test")
	if !resp.HasPassword {
		t.Fatalf("expected HasPassword=true when hash is set")
	}
	if resp.URL != "https://example.test/share/abcd-link" {
		t.Fatalf("URL = %q, want %q",
			resp.URL, "https://example.test/share/abcd-link")
	}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, banned := range []string{"password_hash", "PasswordHash", "$2a$"} {
		if strings.Contains(string(body), banned) {
			t.Fatalf("share link JSON leaks %q: %s", banned, string(body))
		}
	}
}

// TestShareRateLimit_KeyedOnIPAndSlug verifies that the verify limiter
// bucket is keyed on (IP, slug) rather than IP alone: an attacker who
// exhausts attempts on one slug must not be locked out of another
// (and an attacker rotating slugs from the same IP must still hit the
// cap on each individual slug).
func TestShareRateLimit_KeyedOnIPAndSlug(t *testing.T) {
	limiter := newShareRateLimiter(2, time.Minute)
	now := time.Unix(1_700_000_000, 0)
	ip := "10.0.0.1"

	// Exhaust the bucket for (ip, "alpha").
	keyAlpha := shareRateLimitKey(ip, "alpha")
	keyBeta := shareRateLimitKey(ip, "beta")
	_, _ = limiter.allow(keyAlpha, now)
	_, _ = limiter.allow(keyAlpha, now)
	if _, blocked := limiter.allow(keyAlpha, now); !blocked {
		t.Fatalf("third attempt on (ip, alpha) should be blocked")
	}
	// Same IP, different slug — must still be allowed.
	if _, blocked := limiter.allow(keyBeta, now); blocked {
		t.Fatalf("first attempt on (ip, beta) should NOT be blocked by (ip, alpha) exhaustion")
	}
}

// TestShareRateLimitKey_SlugSeparator confirms that the bucket key is
// unambiguous: a slug cannot collide with an IP by accident because the
// `|` separator is not legal in either.
func TestShareRateLimitKey_SlugSeparator(t *testing.T) {
	key := shareRateLimitKey("1.2.3.4", "summer-2025")
	if !strings.Contains(key, "|") {
		t.Fatalf("expected key to contain `|` separator, got %q", key)
	}
}

// TestNextShareSlugCandidate covers the suffix generation used by the
// auto-retry path: the dedup suffix grows monotonically and the base is
// trimmed so the combined string never exceeds the 64-byte column
// limit (with no trailing "-").
func TestNextShareSlugCandidate(t *testing.T) {
	cases := []struct {
		name string
		base string
		n    int
		want string
	}{
		{"first attempt returns base", "summer-2025", 1, "summer-2025"},
		{"second attempt appends -2", "summer-2025", 2, "summer-2025-2"},
		{"large attempt appends -42", "x", 42, "x-42"},
		{
			"long base gets trimmed",
			strings.Repeat("a", postgres.ShareSlugMaxLen()),
			11,
			strings.Repeat("a", postgres.ShareSlugMaxLen()-3) + "-11",
		},
	}
	for _, c := range cases {
		got := nextShareSlugCandidate(c.base, c.n)
		if got != c.want {
			t.Errorf("nextShareSlugCandidate(%q, %d) = %q, want %q",
				c.base, c.n, got, c.want)
		}
		if len(got) > postgres.ShareSlugMaxLen() {
			t.Errorf("candidate %q exceeds column width %d",
				got, postgres.ShareSlugMaxLen())
		}
	}
}

// TestPhotoIsPublic covers the per-photo gate used by the public
// thumbnail and download endpoints. Archived (soft-deleted) and
// private photos must always be hidden, even when their UID is leaked
// by another channel.
func TestPhotoIsPublic(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name  string
		photo *database.Photo
		want  bool
	}{
		{"nil is not public", nil, false},
		{"plain photo is public", &database.Photo{UID: "p1"}, true},
		{"private photo is hidden", &database.Photo{UID: "p2", Private: true}, false},
		{"archived photo is hidden", &database.Photo{UID: "p3", ArchivedAt: &now}, false},
		{
			"private + archived photo is hidden",
			&database.Photo{UID: "p4", Private: true, ArchivedAt: &now},
			false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := photoIsPublic(c.photo); got != c.want {
				t.Errorf("photoIsPublic = %v, want %v", got, c.want)
			}
		})
	}
}

// TestPublicPhotoFilter ensures the shared filter applied to public
// listings always excludes archived and private photos. The defaults
// would otherwise let private photos slip through.
func TestPublicPhotoFilter(t *testing.T) {
	f := publicPhotoFilter("a123", 100, 0)
	if f.Private == nil || *f.Private {
		t.Errorf("Private must be explicitly false; got %+v", f.Private)
	}
	if f.Archived == nil || *f.Archived {
		t.Errorf("Archived must be explicitly false; got %+v", f.Archived)
	}
	if f.AlbumUID != "a123" {
		t.Errorf("AlbumUID = %q, want a123", f.AlbumUID)
	}
	if f.SortBy != "newest" {
		t.Errorf("SortBy = %q, want newest", f.SortBy)
	}
}

// TestClientIP_TrustsRemoteAddr asserts that the rate-limit IP key
// comes from r.RemoteAddr (already de-proxied by chi's RealIP
// middleware) rather than from a fresh re-read of X-Forwarded-For,
// which would let an unproxied attacker bypass the bucket by rotating
// the header value.
func TestClientIP_TrustsRemoteAddr(t *testing.T) {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/public/share/abc/verify", nil)
	r.RemoteAddr = "203.0.113.5:54321"
	r.Header.Set("X-Forwarded-For", "192.0.2.99")
	if got := clientIP(r); got != "203.0.113.5" {
		t.Fatalf("clientIP = %q, want 203.0.113.5 (RemoteAddr without port)", got)
	}
}

// TestClientIP_BareRemoteAddr handles the case where RealIP wrote a
// bare IP into r.RemoteAddr (no port). The function must return the
// address verbatim instead of mangling it.
func TestClientIP_BareRemoteAddr(t *testing.T) {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/public/share/abc", nil)
	r.RemoteAddr = "203.0.113.5"
	if got := clientIP(r); got != "203.0.113.5" {
		t.Fatalf("clientIP = %q, want 203.0.113.5", got)
	}
}

// newShareHandlerForTest wires together a ShareHandler with in-memory
// repositories so the test exercises the real HTTP surface (auth, role
// gates, response shapes, JSON marshalling) instead of poking at
// internals.
func newShareHandlerForTest(t *testing.T) (*ShareHandler, *fakeShareLinkRepo, *fakeAlbumRepo, *fakePhotoReader) {
	t.Helper()
	share := newFakeShareLinkRepo()
	albums := newFakeAlbumRepo()
	photos := newFakePhotoReader()
	// A real on-disk storage isn't needed for the gating tests — they
	// short-circuit before any file IO. But the handler refuses to enter
	// preparePhotoRequest without a non-nil *Storage, so the harness
	// hands it a throwaway root rooted in t.TempDir().
	store, err := storage.New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	h := NewShareHandler(testConfig(), nil, share, albums, photos, store)
	return h, share, albums, photos
}

// makeShareRequest builds an httptest request with chi URL params
// populated, since the handlers read the slug via chi.URLParam rather
// than the routing path.
func makeShareRequest(method, target, slug, photoUID string) *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), method, target, nil)
	ctx := chi.NewRouteContext()
	if slug != "" {
		ctx.URLParams.Add("slug", slug)
	}
	if photoUID != "" {
		ctx.URLParams.Add("photo_uid", photoUID)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, ctx))
}

// TestShareHandler_ListPhotos_ExcludesPrivateAndArchived asserts that
// the public listing only surfaces photos that pass photoIsPublic.
// Without the explicit filter, private rows would slip through (the
// default ListPhotos behaviour includes them) — and the recipient of a
// share link must never see anything marked private/archived even if
// the album row still references it.
func TestShareHandler_ListPhotos_ExcludesPrivateAndArchived(t *testing.T) {
	h, share, albums, photos := newShareHandlerForTest(t)
	now := time.Now()
	albums.add(&database.Album{UID: "a1", Slug: "sum", Title: "Summer"})
	_ = albums.AddPhotos(context.Background(), "a1", []string{"p-pub", "p-priv", "p-arch"})
	photos.add(&database.Photo{UID: "p-pub", Title: "public"})
	photos.add(&database.Photo{UID: "p-priv", Title: "private", Private: true})
	photos.add(&database.Photo{UID: "p-arch", Title: "archived", ArchivedAt: &now})
	if err := share.CreateShareLink(context.Background(), &database.ShareLink{
		Slug: "summer-share", AlbumUID: "a1", CreatedByUserUID: "u1",
	}); err != nil {
		t.Fatalf("seed share: %v", err)
	}

	w := httptest.NewRecorder()
	r := makeShareRequest(http.MethodGet, "/api/v1/public/share/summer-share/photos", "summer-share", "")
	h.ListPhotos(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Photos []publicPhoto `json:"photos"`
		Total  int           `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 || len(resp.Photos) != 1 {
		t.Fatalf("expected only 1 public photo, got total=%d items=%d (body=%s)",
			resp.Total, len(resp.Photos), w.Body.String())
	}
	if resp.Photos[0].UID != "p-pub" {
		t.Errorf("expected p-pub, got %q", resp.Photos[0].UID)
	}
}

// TestShareHandler_Thumbnail_RejectsPrivateAndArchived locks down the
// per-photo gate: even when a private/archived photo is still a member
// of the linked album, the public thumbnail endpoint must return 404.
// Before the hardening sweep, preparePhotoRequest only checked album
// membership — the recipient could iterate UIDs and pull hidden
// thumbnails.
func TestShareHandler_Thumbnail_RejectsPrivateAndArchived(t *testing.T) {
	h, share, albums, photos := newShareHandlerForTest(t)
	now := time.Now()
	albums.add(&database.Album{UID: "a1", Title: "Album"})
	_ = albums.AddPhotos(context.Background(), "a1", []string{"p-priv", "p-arch"})
	photos.add(&database.Photo{UID: "p-priv", FileHash: "deadbeefdeadbeefdeadbeefdeadbeef", Private: true})
	photos.add(&database.Photo{UID: "p-arch", FileHash: "cafebabecafebabecafebabecafebabe", ArchivedAt: &now})
	if err := share.CreateShareLink(context.Background(), &database.ShareLink{
		Slug: "hidden-pix", AlbumUID: "a1", CreatedByUserUID: "u1",
	}); err != nil {
		t.Fatalf("seed share: %v", err)
	}

	for _, photoUID := range []string{"p-priv", "p-arch"} {
		w := httptest.NewRecorder()
		r := makeShareRequest(
			http.MethodGet,
			"/api/v1/public/share/hidden-pix/photos/"+photoUID+"/thumb/fit_720",
			"hidden-pix", photoUID,
		)
		// chi.URLParam needs an extra param for the size.
		rCtx := chi.RouteContext(r.Context())
		rCtx.URLParams.Add("size", "fit_720")
		h.Thumbnail(w, r)
		if w.Code != http.StatusNotFound {
			t.Errorf("photo %s: expected 404, got %d (body=%s)",
				photoUID, w.Code, w.Body.String())
		}
	}
}

// TestInsertShareLinkWithRetry_RetriesGeneratedSlug confirms that an
// auto-derived slug (slugWasGenerated=true) walks past collisions on
// the same base. Two albums both titled "Summer" should both succeed —
// the second landing on "summer-2".
func TestInsertShareLinkWithRetry_RetriesGeneratedSlug(t *testing.T) {
	repo := newFakeShareLinkRepo()
	h := &ShareHandler{shareRepo: repo}

	link1 := &database.ShareLink{Slug: "summer", AlbumUID: "a1", CreatedByUserUID: "u"}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
	if err := h.insertShareLinkWithRetry(r, repo, link1, true); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	link2 := &database.ShareLink{Slug: "summer", AlbumUID: "a2", CreatedByUserUID: "u"}
	if err := h.insertShareLinkWithRetry(r, repo, link2, true); err != nil {
		t.Fatalf("second insert: %v", err)
	}
	if link2.Slug != "summer-2" {
		t.Errorf("second slug = %q, want summer-2", link2.Slug)
	}
}

// TestInsertShareLinkWithRetry_DoesNotRetryUserSlug asserts the
// opposite: when the caller pinned the slug, a collision is surfaced
// as a 409 rather than silently shifted to a different slug.
func TestInsertShareLinkWithRetry_DoesNotRetryUserSlug(t *testing.T) {
	repo := newFakeShareLinkRepo()
	h := &ShareHandler{shareRepo: repo}

	if err := h.insertShareLinkWithRetry(
		httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil),
		repo,
		&database.ShareLink{Slug: "winter", AlbumUID: "a1", CreatedByUserUID: "u"},
		false,
	); err != nil {
		t.Fatalf("seed insert: %v", err)
	}
	err := h.insertShareLinkWithRetry(
		httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil),
		repo,
		&database.ShareLink{Slug: "winter", AlbumUID: "a2", CreatedByUserUID: "u"},
		false,
	)
	if err == nil {
		t.Fatalf("expected ErrShareLinkSlugTaken")
	}
	if !errors.Is(err, database.ErrShareLinkSlugTaken) {
		t.Errorf("expected ErrShareLinkSlugTaken via errors.Is, got %v", err)
	}
}

// TestShareCookie_PathHasTrailingSlash locks down the cookie path
// scoping: RFC 6265 path-match treats a cookie path without a trailing
// slash as a prefix that can bleed onto a different share whose slug
// shares a leading substring. The trailing slash makes that impossible.
func TestShareCookie_PathHasTrailingSlash(t *testing.T) {
	h, _, _, _ := newShareHandlerForTest(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/public/share/abc/verify", nil)
	h.setShareCookie(w, r, "abc")

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Name != "share_abc" {
		t.Errorf("Name = %q, want share_abc", c.Name)
	}
	if !strings.HasSuffix(c.Path, "/") {
		t.Errorf("Path = %q, want trailing slash so prefix-match is scoped to the slug", c.Path)
	}
	if c.Path != "/api/v1/public/share/abc/" {
		t.Errorf("Path = %q, want /api/v1/public/share/abc/", c.Path)
	}
	if !c.HttpOnly {
		t.Error("cookie must be HttpOnly")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
}

func TestShareLink_IsExpired(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	if (&database.ShareLink{}).IsExpired(now) {
		t.Fatalf("NULL expiry must never be expired")
	}
	if !(&database.ShareLink{ExpiresAt: &past}).IsExpired(now) {
		t.Fatalf("past expiry should be expired")
	}
	if (&database.ShareLink{ExpiresAt: &future}).IsExpired(now) {
		t.Fatalf("future expiry should not be expired")
	}
}
