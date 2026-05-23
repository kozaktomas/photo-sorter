package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/auth"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// fakeUserRepo is an in-memory database.UserWriter sufficient to drive the
// auth handler paths. It is keyed by both UID and username for symmetry
// with the production repository, and tracks TouchLastLogin so the
// fire-and-forget path can be observed in tests.
type fakeUserRepo struct {
	mu         sync.Mutex
	byUsername map[string]*database.UserWithSecret
	byUID      map[string]*database.User
	touched    []string
	getErr     error
}

// newFakeUserRepo builds an empty repo.
func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{
		byUsername: map[string]*database.UserWithSecret{},
		byUID:      map[string]*database.User{},
	}
}

// add seeds the repo with a user. password is bcrypt-hashed before
// storage so the handler exercises the real CheckPassword path.
func (r *fakeUserRepo) add(t *testing.T, u database.User, password string) {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	ws := &database.UserWithSecret{User: u, PasswordHash: hash}
	r.byUsername[u.Username] = ws
	r.byUID[u.UID] = &ws.User
}

func (r *fakeUserRepo) GetUser(_ context.Context, uid string) (*database.User, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	u, ok := r.byUID[uid]
	if !ok {
		return nil, database.ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (r *fakeUserRepo) GetUserByUsername(
	_ context.Context, username string,
) (*database.UserWithSecret, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	u, ok := r.byUsername[username]
	if !ok {
		return nil, database.ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (r *fakeUserRepo) ListUsers(_ context.Context) ([]database.User, error) {
	out := make([]database.User, 0, len(r.byUID))
	for _, u := range r.byUID {
		out = append(out, *u)
	}
	return out, nil
}

func (r *fakeUserRepo) CountUsers(_ context.Context) (int, error) {
	return len(r.byUID), nil
}

func (r *fakeUserRepo) CreateUser(_ context.Context, u *database.UserWithSecret) error {
	if _, ok := r.byUsername[u.Username]; ok {
		return database.ErrUsernameTaken
	}
	// Mirror the production repo: synthesize a UID when the caller did not
	// supply one. Handlers rely on this side effect when constructing the
	// response payload.
	if u.UID == "" {
		u.UID = "u-fake-" + u.Username
	}
	cp := *u
	r.byUsername[cp.Username] = &cp
	r.byUID[cp.UID] = &cp.User
	return nil
}

func (r *fakeUserRepo) UpdateUser(_ context.Context, u *database.User) error {
	r.byUID[u.UID] = u
	return nil
}

func (r *fakeUserRepo) SetPassword(_ context.Context, uid, newHash string) error {
	for _, u := range r.byUsername {
		if u.UID == uid {
			u.PasswordHash = newHash
			return nil
		}
	}
	return database.ErrNotFound
}

func (r *fakeUserRepo) SetDisabled(_ context.Context, uid string, disabled bool) error {
	if u, ok := r.byUID[uid]; ok {
		u.Disabled = disabled
	}
	if u, ok := r.byUsername[uidToUsername(r, uid)]; ok {
		u.Disabled = disabled
	}
	return nil
}

func (r *fakeUserRepo) TouchLastLogin(_ context.Context, uid string) error {
	r.mu.Lock()
	r.touched = append(r.touched, uid)
	r.mu.Unlock()
	return nil
}

func (r *fakeUserRepo) DeleteUser(_ context.Context, uid string) error {
	if _, ok := r.byUID[uid]; !ok {
		return database.ErrNotFound
	}
	delete(r.byUID, uid)
	for k, u := range r.byUsername {
		if u.UID == uid {
			delete(r.byUsername, k)
		}
	}
	return nil
}

// uidToUsername resolves a UID to its username for the test repo. Helper
// for SetDisabled which addresses by UID.
func uidToUsername(r *fakeUserRepo, uid string) string {
	for _, u := range r.byUsername {
		if u.UID == uid {
			return u.Username
		}
	}
	return ""
}

// touchedCount returns how many times TouchLastLogin has been invoked.
// Used by tests to wait for the fire-and-forget goroutine to complete.
func (r *fakeUserRepo) touchedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.touched)
}

// waitForTouch blocks until TouchLastLogin has been called at least once
// or the timeout fires. Avoids racing with the goroutine the handler
// dispatches without resorting to time.Sleep.
func (r *fakeUserRepo) waitForTouch(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if r.touchedCount() > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("TouchLastLogin not invoked within %s", timeout)
}

// newTestAuthHandler constructs an AuthHandler wired against the supplied
// fake repo and a fresh in-memory SessionManager.
func newTestAuthHandler(repo *fakeUserRepo) (*AuthHandler, *middleware.SessionManager) {
	sm := middleware.NewSessionManager("test-secret", nil)
	cfg := testConfig()
	return NewAuthHandler(cfg, sm, repo, repo), sm
}

// loginRequestBody returns a JSON login body for the given credentials.
func loginRequestBody(username, password string) *bytes.Buffer {
	body := `{"username":"` + username + `","password":"` + password + `"}`
	return bytes.NewBufferString(body)
}

// TestAuthHandler_Login_Success exercises the happy path: an existing,
// enabled user supplies the correct password and gets a session cookie
// plus a user payload that carries the role.
func TestAuthHandler_Login_Success(t *testing.T) {
	repo := newFakeUserRepo()
	repo.add(t, database.User{
		UID: "u-admin", Username: "admin", DisplayName: "Admin",
		Role: auth.RoleAdmin,
	}, "s3cret")
	handler, _ := newTestAuthHandler(repo)

	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/auth/login",
		loginRequestBody("admin", "s3cret"),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.Login(w, req)

	assertStatusCode(t, w, http.StatusOK)

	var resp loginResponse
	parseJSONResponse(t, w, &resp)
	if resp.User.UID != "u-admin" {
		t.Errorf("user.uid = %q, want u-admin", resp.User.UID)
	}
	if resp.User.Username != "admin" {
		t.Errorf("user.username = %q, want admin", resp.User.Username)
	}
	if resp.User.Role != auth.RoleAdmin {
		t.Errorf("user.role = %q, want admin", resp.User.Role)
	}
	if resp.User.DisplayName != "Admin" {
		t.Errorf("user.display_name = %q, want Admin", resp.User.DisplayName)
	}

	if len(w.Result().Cookies()) == 0 {
		t.Error("expected a session cookie to be set")
	}
	repo.waitForTouch(t, time.Second)
}

// TestAuthHandler_Login_WrongPassword verifies the generic 401 is returned
// for a valid username with an incorrect password and that the response
// does not leak which factor was wrong.
func TestAuthHandler_Login_WrongPassword(t *testing.T) {
	repo := newFakeUserRepo()
	repo.add(t, database.User{
		UID: "u-edit", Username: "editor", Role: auth.RoleEditor,
	}, "correct-horse")
	handler, _ := newTestAuthHandler(repo)

	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/auth/login",
		loginRequestBody("editor", "wrong"),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.Login(w, req)

	assertStatusCode(t, w, http.StatusUnauthorized)
	assertJSONError(t, w, errInvalidCredentials)
}

// TestAuthHandler_Login_UnknownUser verifies that a missing username
// yields the same 401 / generic message as a wrong password.
func TestAuthHandler_Login_UnknownUser(t *testing.T) {
	repo := newFakeUserRepo()
	handler, _ := newTestAuthHandler(repo)

	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/auth/login",
		loginRequestBody("nobody", "anything"),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.Login(w, req)

	assertStatusCode(t, w, http.StatusUnauthorized)
	assertJSONError(t, w, errInvalidCredentials)
}

// TestAuthHandler_Login_DisabledUser verifies that disabled accounts
// cannot log in even with the correct password.
func TestAuthHandler_Login_DisabledUser(t *testing.T) {
	repo := newFakeUserRepo()
	repo.add(t, database.User{
		UID: "u-off", Username: "ghost",
		Role: auth.RoleViewer, Disabled: true,
	}, "still-valid")
	handler, _ := newTestAuthHandler(repo)

	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/auth/login",
		loginRequestBody("ghost", "still-valid"),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.Login(w, req)

	assertStatusCode(t, w, http.StatusUnauthorized)
	assertJSONError(t, w, errInvalidCredentials)
}

// TestAuthHandler_Login_MissingCredentials covers the 400 path for
// blank username, blank password, and both.
func TestAuthHandler_Login_MissingCredentials(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"missing username", `{"username": "", "password": "p"}`},
		{"missing password", `{"username": "u", "password": ""}`},
		{"missing both", `{"username": "", "password": ""}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, _ := newTestAuthHandler(newFakeUserRepo())
			req := httptest.NewRequestWithContext(
				context.Background(), http.MethodPost, "/api/v1/auth/login",
				bytes.NewBufferString(tt.body),
			)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler.Login(w, req)
			assertStatusCode(t, w, http.StatusBadRequest)
			assertJSONError(t, w, "username and password are required")
		})
	}
}

// TestAuthHandler_Login_InvalidJSON verifies a malformed body returns 400.
func TestAuthHandler_Login_InvalidJSON(t *testing.T) {
	handler, _ := newTestAuthHandler(newFakeUserRepo())
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/auth/login",
		bytes.NewBufferString("{not-json"),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.Login(w, req)
	assertStatusCode(t, w, http.StatusBadRequest)
	assertJSONError(t, w, errInvalidRequestBody)
}

// TestAuthHandler_Login_NilUserReader verifies the handler 500s gracefully
// when constructed without a user store (transient startup state).
func TestAuthHandler_Login_NilUserReader(t *testing.T) {
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := NewAuthHandler(testConfig(), sm, nil, nil)
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/auth/login",
		loginRequestBody("a", "b"),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.Login(w, req)
	assertStatusCode(t, w, http.StatusInternalServerError)
}

// TestAuthHandler_Login_RotatesSessionID is the session-fixation guard:
// when the login request carries a pre-existing session cookie, that
// session must be deleted server-side before the new one is minted.
// Otherwise an attacker who had pre-set the victim's cookie would
// retain access through the old ID after the victim authenticated.
func TestAuthHandler_Login_RotatesSessionID(t *testing.T) {
	repo := newFakeUserRepo()
	repo.add(t, database.User{
		UID: "u-admin", Username: "admin", Role: auth.RoleAdmin,
	}, "s3cret")
	handler, sm := newTestAuthHandler(repo)

	// An attacker-fixed session exists in the store before login.
	planted, err := sm.CreateSession("", "", "u-victim-prior", auth.RoleViewer)
	if err != nil {
		t.Fatalf("planted session: %v", err)
	}

	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/auth/login",
		loginRequestBody("admin", "s3cret"),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+planted.ID)
	w := httptest.NewRecorder()
	handler.Login(w, req)

	assertStatusCode(t, w, http.StatusOK)

	// The planted session must be unusable after a successful login.
	if sm.GetSession(planted.ID) != nil {
		t.Error("planted session survived login — session-fixation guard failed")
	}
}

// TestAuthHandler_Login_TimingConstantForUnknownUser is a coarse smoke
// check that the unknown-user branch still runs bcrypt against the
// dummy hash (so it takes a comparable amount of time to a real
// wrong-password path). A wide tolerance keeps the test stable on slow
// CI — the real defence is that a bcrypt call happens, not its exact
// duration.
func TestAuthHandler_Login_TimingConstantForUnknownUser(t *testing.T) {
	repo := newFakeUserRepo()
	repo.add(t, database.User{
		UID: "u-real", Username: "real", Role: auth.RoleEditor,
	}, "right-password")
	handler, _ := newTestAuthHandler(repo)

	timeReq := func(body *bytes.Buffer) time.Duration {
		req := httptest.NewRequestWithContext(
			context.Background(), http.MethodPost, "/api/v1/auth/login", body,
		)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		start := time.Now()
		handler.Login(w, req)
		return time.Since(start)
	}

	// Warmup so the first call doesn't pay outsized init costs.
	timeReq(loginRequestBody("real", "wrong"))

	wrongPassword := timeReq(loginRequestBody("real", "wrong"))
	unknownUser := timeReq(loginRequestBody("nobody", "anything"))

	// Both paths must spend non-trivial time on a real bcrypt compare.
	// 30ms is a generous floor that still excludes the no-bcrypt
	// regression (which would return in microseconds on a Pi).
	const floor = 30 * time.Millisecond
	if wrongPassword < floor {
		t.Errorf("wrong-password path too fast (%v) — bcrypt skipped?", wrongPassword)
	}
	if unknownUser < floor {
		t.Errorf("unknown-user path too fast (%v) — bcrypt skipped?", unknownUser)
	}
}

// TestAuthHandler_Login_TruncatesAuditActor verifies that an absurdly
// long username submitted to /login does not get persisted verbatim
// into metadata.actor — the truncation cap keeps audit rows bounded.
func TestAuthHandler_Login_TruncatesAuditActor(t *testing.T) {
	if maxFailedLoginActorLen <= 0 {
		t.Fatal("maxFailedLoginActorLen must be > 0")
	}
	long := strings.Repeat("a", maxFailedLoginActorLen+512)
	got := truncateForAudit(long)
	if len(got) != maxFailedLoginActorLen {
		t.Errorf("len = %d, want %d", len(got), maxFailedLoginActorLen)
	}
	short := "alice"
	if truncateForAudit(short) != short {
		t.Error("truncateForAudit altered a short input")
	}
}

// TestAuthHandler_Logout_HardDeletesSession verifies the session row is
// removed (not soft-deleted) and the cookie is cleared.
func TestAuthHandler_Logout_HardDeletesSession(t *testing.T) {
	repo := newFakeUserRepo()
	handler, sm := newTestAuthHandler(repo)

	session, _ := sm.CreateSession("", "", "u-x", auth.RoleAdmin)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+session.ID)
	w := httptest.NewRecorder()
	handler.Logout(w, req)

	assertStatusCode(t, w, http.StatusOK)
	if sm.GetSession(session.ID) != nil {
		t.Error("session still present after logout")
	}

	// Verify the cookie is cleared (MaxAge < 0).
	found := false
	for _, c := range w.Result().Cookies() {
		if c.Name != "photo_sorter_session" {
			continue
		}
		found = true
		if c.MaxAge >= 0 {
			t.Errorf("clear cookie MaxAge = %d, want < 0", c.MaxAge)
		}
	}
	if !found {
		t.Error("expected session cookie to be cleared")
	}
}

// TestAuthHandler_Logout_NoSession verifies the contract that logout
// without a session is a successful no-op.
func TestAuthHandler_Logout_NoSession(t *testing.T) {
	handler, _ := newTestAuthHandler(newFakeUserRepo())
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/logout", nil)
	w := httptest.NewRecorder()
	handler.Logout(w, req)
	assertStatusCode(t, w, http.StatusOK)
}

// TestAuthHandler_Status_Authenticated verifies that an active session
// resolves the underlying user and returns the full payload.
func TestAuthHandler_Status_Authenticated(t *testing.T) {
	repo := newFakeUserRepo()
	repo.add(t, database.User{
		UID: "u-alice", Username: "alice", DisplayName: "Alice A",
		Role: auth.RoleEditor,
	}, "x")
	handler, sm := newTestAuthHandler(repo)

	session, _ := sm.CreateSession("", "", "u-alice", auth.RoleEditor)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/auth/status", nil)
	req.Header.Set("Authorization", "Bearer "+session.ID)
	w := httptest.NewRecorder()
	handler.Status(w, req)

	assertStatusCode(t, w, http.StatusOK)
	var resp statusResponse
	parseJSONResponse(t, w, &resp)

	if !resp.Authenticated {
		t.Error("expected authenticated = true")
	}
	if resp.User == nil {
		t.Fatal("expected user payload in response")
	}
	if resp.User.UID != "u-alice" {
		t.Errorf("user.uid = %q, want u-alice", resp.User.UID)
	}
	if resp.User.Username != "alice" {
		t.Errorf("user.username = %q, want alice", resp.User.Username)
	}
	if resp.User.Role != auth.RoleEditor {
		t.Errorf("user.role = %q, want editor", resp.User.Role)
	}
	if resp.User.DisplayName != "Alice A" {
		t.Errorf("user.display_name = %q, want Alice A", resp.User.DisplayName)
	}
}

// TestAuthHandler_Status_Unauthenticated verifies the no-session path
// returns 200 with authenticated = false.
func TestAuthHandler_Status_Unauthenticated(t *testing.T) {
	handler, _ := newTestAuthHandler(newFakeUserRepo())
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/auth/status", nil)
	w := httptest.NewRecorder()
	handler.Status(w, req)
	assertStatusCode(t, w, http.StatusOK)

	var resp statusResponse
	parseJSONResponse(t, w, &resp)
	if resp.Authenticated {
		t.Error("expected authenticated = false")
	}
	if resp.User != nil {
		t.Errorf("expected user to be omitted, got %+v", resp.User)
	}
}

// TestAuthHandler_Status_OrphanedSession covers the rare case where a
// session row points at a UID no longer present in the users table —
// the handler returns whatever it has from the session.
func TestAuthHandler_Status_OrphanedSession(t *testing.T) {
	repo := newFakeUserRepo()
	handler, sm := newTestAuthHandler(repo)
	session, _ := sm.CreateSession("", "", "u-gone", auth.RoleViewer)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/auth/status", nil)
	req.Header.Set("Authorization", "Bearer "+session.ID)
	w := httptest.NewRecorder()
	handler.Status(w, req)
	assertStatusCode(t, w, http.StatusOK)

	var resp statusResponse
	parseJSONResponse(t, w, &resp)
	if !resp.Authenticated || resp.User == nil {
		t.Fatalf("expected authenticated user payload, got %+v", resp)
	}
	if resp.User.UID != "u-gone" || resp.User.Role != auth.RoleViewer {
		t.Errorf("user = %+v, want UID=u-gone Role=viewer", resp.User)
	}
}

// TestAuthHandler_Status_RepoError verifies the handler still responds
// (with what the session knows) when the user store errors out — we do
// not want a transient DB blip to log the user out.
func TestAuthHandler_Status_RepoError(t *testing.T) {
	repo := newFakeUserRepo()
	repo.add(t, database.User{
		UID: "u-bob", Username: "bob", Role: auth.RoleEditor,
	}, "x")
	repo.getErr = errors.New("db down")
	handler, sm := newTestAuthHandler(repo)
	session, _ := sm.CreateSession("", "", "u-bob", auth.RoleEditor)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/auth/status", nil)
	req.Header.Set("Authorization", "Bearer "+session.ID)
	w := httptest.NewRecorder()
	handler.Status(w, req)
	assertStatusCode(t, w, http.StatusOK)

	var resp statusResponse
	parseJSONResponse(t, w, &resp)
	if !resp.Authenticated || resp.User == nil {
		t.Fatalf("expected fallback user payload, got %+v", resp)
	}
	if resp.User.UID != "u-bob" || resp.User.Role != auth.RoleEditor {
		t.Errorf("fallback user = %+v, want UID=u-bob Role=editor", resp.User)
	}
}
