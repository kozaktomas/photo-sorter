package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/auth"
	"github.com/kozaktomas/photo-sorter/internal/database"
)

// fakeAPITokenStore is an in-memory APITokenStore keyed by RAW token, so a
// test can hand a token straight to the middleware.
//
// It is mutex-guarded because the real store is genuinely called from two
// goroutines: ResolveAPIToken on the request path, and TouchAPIToken on the
// detached bookkeeping goroutine that sessionFromAPIToken spawns. A plain map
// here trips the race detector — which is the fake being honest about the
// concurrency the production interface actually has.
type fakeAPITokenStore struct {
	mu sync.Mutex
	// live maps a raw token to the row it resolves to. A token absent here
	// is treated as unknown/revoked/expired — the store returns (nil, nil),
	// exactly as the SQL implementation does for all three cases.
	live    map[string]*database.APIToken
	err     error
	touched []string
}

func newFakeAPITokenStore() *fakeAPITokenStore {
	return &fakeAPITokenStore{live: map[string]*database.APIToken{}}
}

// touchedUIDs returns a snapshot of the UIDs TouchAPIToken has been called
// with, safe to read while the detached goroutine may still be running.
func (f *fakeAPITokenStore) touchedUIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.touched...)
}

// ResolveAPIToken returns (nil, nil) for an unknown token, matching the
// APITokenStore contract: "unknown", "revoked", and "expired" must all be
// indistinguishable to the caller, so none of them is an error.
//
//nolint:nilnil // (nil, nil) is the interface's documented "no live token".
func (f *fakeAPITokenStore) ResolveAPIToken(
	_ context.Context, raw string,
) (*database.APIToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	token, ok := f.live[raw]
	if !ok {
		return nil, nil
	}
	return token, nil
}

func (f *fakeAPITokenStore) TouchAPIToken(_ context.Context, uid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.touched = append(f.touched, uid)
	return nil
}

// managerWithToken returns a SessionManager wired to a store holding one live
// read-scope token, plus the raw token string.
func managerWithToken(t *testing.T) (*SessionManager, *fakeAPITokenStore, string) {
	t.Helper()
	raw, hash, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatalf("GenerateAPIToken: %v", err)
	}
	store := newFakeAPITokenStore()
	store.live[raw] = &database.APIToken{
		UID:              "t1",
		Name:             "kukatko-migration",
		TokenHash:        hash,
		Scope:            auth.APITokenScopeRead,
		CreatedByUserUID: "u-admin",
		CreatedAt:        time.Now().Add(-time.Hour),
	}
	sm := NewSessionManager("test-secret", nil)
	sm.SetAPITokenStore(store)
	return sm, store, raw
}

// bearerRequest builds a request carrying the given bearer credential.
func bearerRequest(method, bearer string) *http.Request {
	req := httptest.NewRequestWithContext(context.Background(), method, "/api/v1/photos", nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	return req
}

func TestGetSessionFromRequest_APIToken(t *testing.T) {
	sm, _, raw := managerWithToken(t)

	session := sm.GetSessionFromRequest(bearerRequest(http.MethodGet, raw))
	if session == nil {
		t.Fatal("a live API token did not authenticate")
	}
	if !session.ReadOnly {
		t.Error("session.ReadOnly = false for an API token principal")
	}
	if session.Role != auth.RoleViewer {
		t.Errorf("session.Role = %q, want %q", session.Role, auth.RoleViewer)
	}
	if session.APITokenUID != "t1" {
		t.Errorf("session.APITokenUID = %q, want %q", session.APITokenUID, "t1")
	}
	// The synthetic session must never be mistaken for a real one.
	if sm.GetSession(session.ID) != nil {
		t.Error("the token's synthetic session leaked into the session store")
	}
}

func TestGetSessionFromRequest_APITokenRejected(t *testing.T) {
	sm, _, _ := managerWithToken(t)

	tests := []struct {
		name   string
		bearer string
	}{
		{name: "unknown token", bearer: auth.APITokenPrefix + "nope"},
		{name: "empty after prefix", bearer: auth.APITokenPrefix},
		{name: "revoked or expired resolves to nothing", bearer: auth.APITokenPrefix + "revoked"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sm.GetSessionFromRequest(bearerRequest(http.MethodGet, tt.bearer)); got != nil {
				t.Errorf("bearer %q authenticated, want rejection", tt.bearer)
			}
		})
	}
}

// TestGetSessionFromRequest_NonTokenBearerStillWorks pins the compatibility
// guarantee: a plain session ID in the Authorization header keeps meaning what
// it always meant. The `psat_` prefix is what routes a value to the token
// store, so an unprefixed value must never reach it.
func TestGetSessionFromRequest_NonTokenBearerStillWorks(t *testing.T) {
	sm, store, _ := managerWithToken(t)

	session, err := sm.CreateSession("", "", "u1", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	got := sm.GetSessionFromRequest(bearerRequest(http.MethodGet, session.ID))
	if got == nil {
		t.Fatal("a plain session ID bearer stopped authenticating")
	}
	if got.Role != auth.RoleAdmin {
		t.Errorf("Role = %q, want admin — session auth must be untouched", got.Role)
	}
	if got.ReadOnly {
		t.Error("a real session was marked ReadOnly")
	}
	if len(store.touchedUIDs()) != 0 {
		t.Error("a session-ID bearer was looked up in the API token store")
	}
}

// TestGetSessionFromRequest_APITokenTouched checks the usage bookkeeping
// fires. It runs on a detached goroutine, so we poll briefly.
func TestGetSessionFromRequest_APITokenTouched(t *testing.T) {
	sm, store, raw := managerWithToken(t)

	if sm.GetSessionFromRequest(bearerRequest(http.MethodGet, raw)) == nil {
		t.Fatal("token did not authenticate")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(store.touchedUIDs()) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("TouchAPIToken was never called; last_used_at would never advance")
}

// TestRequireAuth_APITokenIsReadOnly is the security-critical test: the token
// must be refused on every unsafe method, and allowed on the safe ones.
//
// This is the outermost of three independent write gates. It holds even for a
// handler that performs no role check of its own — which matters, because
// handlers.requireWriteRole treats a *missing* session as an admin, so a
// principal that failed to present as a viewer would sail straight through it.
func TestRequireAuth_APITokenIsReadOnly(t *testing.T) {
	sm, _, raw := managerWithToken(t)

	// A handler with NO authorization logic whatsoever.
	var reached bool
	handler := RequireAuth(sm)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		method     string
		wantStatus int
		wantReach  bool
	}{
		{method: http.MethodGet, wantStatus: http.StatusOK, wantReach: true},
		{method: http.MethodHead, wantStatus: http.StatusOK, wantReach: true},
		{method: http.MethodOptions, wantStatus: http.StatusOK, wantReach: true},
		{method: http.MethodPost, wantStatus: http.StatusForbidden, wantReach: false},
		{method: http.MethodPut, wantStatus: http.StatusForbidden, wantReach: false},
		{method: http.MethodPatch, wantStatus: http.StatusForbidden, wantReach: false},
		{method: http.MethodDelete, wantStatus: http.StatusForbidden, wantReach: false},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			reached = false
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, bearerRequest(tt.method, raw))

			if rec.Code != tt.wantStatus {
				t.Errorf("%s status = %d, want %d", tt.method, rec.Code, tt.wantStatus)
			}
			if reached != tt.wantReach {
				t.Errorf("%s reached handler = %v, want %v — a read-only token must not "+
					"reach a mutating handler even when that handler checks nothing",
					tt.method, reached, tt.wantReach)
			}
		})
	}
}

// TestRequireAuth_APITokenAuthInfo checks the AuthInfo the handlers see. The
// viewer role is what makes both auth.HasWriteAccess and
// handlers.requireWriteRole reject a write.
func TestRequireAuth_APITokenAuthInfo(t *testing.T) {
	sm, _, raw := managerWithToken(t)

	var info *AuthInfo
	var session *Session
	handler := RequireAuth(sm)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		info, _ = GetAuthInfo(r.Context())
		session = GetSessionFromContext(r.Context())
	}))
	handler.ServeHTTP(httptest.NewRecorder(), bearerRequest(http.MethodGet, raw))

	if info == nil {
		t.Fatal("no AuthInfo on the context")
	}
	if info.Role != auth.RoleViewer {
		t.Errorf("AuthInfo.Role = %q, want viewer", info.Role)
	}
	if !info.ReadOnly {
		t.Error("AuthInfo.ReadOnly = false")
	}
	if auth.HasWriteAccess(info.Role) {
		t.Error("HasWriteAccess(token role) = true — the token could write")
	}

	// The *Session must also be present. handlers.requireWriteRole reads it
	// and treats a nil session as an admin, so its absence would be an
	// outright privilege escalation.
	if session == nil {
		t.Fatal("no *Session on the context — requireWriteRole would fail open to admin")
	}
	if session.Role != auth.RoleViewer {
		t.Errorf("Session.Role = %q, want viewer", session.Role)
	}
}

// TestRequireAuth_NoTokenStore verifies a server without the api_tokens store
// wired simply refuses token bearers, rather than panicking.
func TestRequireAuth_NoTokenStore(t *testing.T) {
	sm := NewSessionManager("test-secret", nil) // no SetAPITokenStore

	rec := httptest.NewRecorder()
	RequireAuth(sm)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler reached with no token store configured")
	})).ServeHTTP(rec, bearerRequest(http.MethodGet, auth.APITokenPrefix+"whatever"))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestIsSafeMethod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		method string
		want   bool
	}{
		{method: http.MethodGet, want: true},
		{method: http.MethodHead, want: true},
		{method: http.MethodOptions, want: true},
		{method: http.MethodPost, want: false},
		{method: http.MethodPut, want: false},
		{method: http.MethodPatch, want: false},
		{method: http.MethodDelete, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			t.Parallel()
			if got := isSafeMethod(tt.method); got != tt.want {
				t.Errorf("isSafeMethod(%q) = %v, want %v", tt.method, got, tt.want)
			}
		})
	}
}
