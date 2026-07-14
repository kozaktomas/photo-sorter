package auth

import (
	"strings"
	"testing"
)

func TestGenerateAPIToken_shape(t *testing.T) {
	t.Parallel()

	raw, hash, err := GenerateAPIToken()
	if err != nil {
		t.Fatalf("GenerateAPIToken() error = %v, want nil", err)
	}
	if !strings.HasPrefix(raw, APITokenPrefix) {
		t.Errorf("raw token %q does not carry the %q prefix", raw, APITokenPrefix)
	}
	if !IsAPIToken(raw) {
		t.Errorf("IsAPIToken(%q) = false, want true", raw)
	}
	// 32 random bytes → 43 base64url chars (no padding), plus the prefix.
	if want := len(APITokenPrefix) + 43; len(raw) != want {
		t.Errorf("len(raw) = %d, want %d", len(raw), want)
	}
	// SHA-256 as lowercase hex.
	if len(hash) != 64 {
		t.Errorf("len(hash) = %d, want 64 hex chars", len(hash))
	}
	if hash != HashAPIToken(raw) {
		t.Errorf("returned hash does not match HashAPIToken(raw)")
	}
	if strings.Contains(hash, raw) {
		t.Error("hash leaks the raw token")
	}
}

// TestGenerateAPIToken_unique is the property that matters most: two tokens
// minted back to back must not collide, or one client could authenticate as
// another.
func TestGenerateAPIToken_unique(t *testing.T) {
	t.Parallel()

	const iterations = 100
	seen := make(map[string]struct{}, iterations)
	for range iterations {
		raw, _, err := GenerateAPIToken()
		if err != nil {
			t.Fatalf("GenerateAPIToken() error = %v", err)
		}
		if _, dup := seen[raw]; dup {
			t.Fatalf("GenerateAPIToken() produced a duplicate token: %q", raw)
		}
		seen[raw] = struct{}{}
	}
}

func TestHashAPIToken_deterministicAndDistinct(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    string
		b    string
		same bool
	}{
		{name: "same input hashes the same", a: "psat_abc", b: "psat_abc", same: true},
		{name: "different input hashes differently", a: "psat_abc", b: "psat_abd", same: false},
		{
			// The prefix is part of the hashed string, so a bare suffix can
			// never collide with the prefixed token it came from.
			name: "prefix participates in the hash",
			a:    "psat_abc",
			b:    "abc",
			same: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := HashAPIToken(tt.a) == HashAPIToken(tt.b)
			if got != tt.same {
				t.Errorf("HashAPIToken(%q) == HashAPIToken(%q) is %v, want %v",
					tt.a, tt.b, got, tt.same)
			}
		})
	}
}

func TestIsAPIToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		bearer string
		want   bool
	}{
		{name: "prefixed value is a token", bearer: "psat_xyz", want: true},
		{name: "bare prefix is a token", bearer: "psat_", want: true},
		{name: "empty is not a token", bearer: "", want: false},
		{
			// A session ID is base64 of 32 random bytes; it must never be
			// mistaken for a token, or session auth would break.
			name:   "session-looking value is not a token",
			bearer: "N2Y4YWJjZGVmMDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODk=",
			want:   false,
		},
		{name: "prefix must be at the start", bearer: "xpsat_abc", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsAPIToken(tt.bearer); got != tt.want {
				t.Errorf("IsAPIToken(%q) = %v, want %v", tt.bearer, got, tt.want)
			}
		})
	}
}

// TestAPIToken_readScopeIsNotWriteCapable pins the invariant that the whole
// read-only design rests on: a token principal carries the viewer role, and
// viewer has no write access.
func TestAPIToken_readScopeIsNotWriteCapable(t *testing.T) {
	t.Parallel()

	if HasWriteAccess(RoleViewer) {
		t.Fatal("RoleViewer has write access — an API token principal could mutate data")
	}
	if APITokenScopeRead != "read" {
		t.Errorf("APITokenScopeRead = %q, want \"read\"", APITokenScopeRead)
	}
}
