package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	// APITokenPrefix marks a bearer credential as an api_tokens row rather
	// than a session ID. Both arrive in the same `Authorization: Bearer <v>`
	// header, so the prefix is what lets the auth path route the value to the
	// right store without a speculative lookup in each.
	APITokenPrefix = "psat_"

	// apiTokenRandBytes is the entropy behind a token. 32 bytes = 256 bits,
	// which is why the SHA-256 storage in migration 044 is sound: there is no
	// low-entropy structure to brute-force, so a slow KDF buys nothing while
	// costing a bcrypt round on every single API request.
	apiTokenRandBytes = 32

	// APITokenScopeRead is the only scope that exists today. A token with
	// this scope authenticates as the viewer role and is additionally barred
	// from unsafe HTTP methods by the auth middleware.
	APITokenScopeRead = "read"
)

// GenerateAPIToken mints a new raw API token and returns it alongside its
// SHA-256 hash. The raw value is the only copy that will ever exist — it is
// shown to the operator once and cannot be recovered from the stored hash.
//
// Returns an error only when the system random source fails.
func GenerateAPIToken() (raw, hash string, err error) {
	buf := make([]byte, apiTokenRandBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate api token: read random: %w", err)
	}
	raw = APITokenPrefix + base64.RawURLEncoding.EncodeToString(buf)
	return raw, HashAPIToken(raw), nil
}

// HashAPIToken returns the lowercase-hex SHA-256 of a raw token. It is the
// value stored in api_tokens.token_hash and the key the auth path looks up
// on every request.
//
// Hashing the *prefixed* string (rather than the random suffix alone) means a
// value that merely looks token-shaped can never collide with a stored hash.
func HashAPIToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// IsAPIToken reports whether a bearer value carries the API-token prefix and
// therefore should be resolved against api_tokens rather than the session
// store. It does not validate the token — only classifies it.
func IsAPIToken(bearer string) bool {
	return strings.HasPrefix(bearer, APITokenPrefix)
}
