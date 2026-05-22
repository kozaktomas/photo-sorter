package handlers

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
)

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
