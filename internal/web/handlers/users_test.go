package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/auth"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// newTestUsersHandler wires a UsersHandler against an in-memory user repo.
func newTestUsersHandler(repo database.UserWriter) *UsersHandler {
	return NewUsersHandler(testConfig(), repo)
}

// withAuthInfo attaches an AuthInfo for `info` to the request context so
// handlers that read it (Delete, Me, ChangeMyPassword) behave as if the
// request had passed through RequireAuth.
func withAuthInfo(r *http.Request, uid, role string) *http.Request {
	ctx := middleware.SetAuthInfoInContext(r.Context(), &middleware.AuthInfo{
		UserUID: uid,
		Role:    role,
	})
	return r.WithContext(ctx)
}

// seedUser stores a user (hashing the supplied plaintext password) and
// returns it for inspection in assertions.
func seedUser(t *testing.T, repo *fakeUserRepo, uid, username, role, password string) database.User {
	t.Helper()
	u := database.User{
		UID:       uid,
		Username:  username,
		Role:      role,
		CreatedAt: time.Now().UTC(),
	}
	repo.add(t, u, password)
	return u
}

// TestUsersHandler_List_Success verifies List returns all seeded users
// sorted by username inside a `{ users: [...] }` envelope.
func TestUsersHandler_List_Success(t *testing.T) {
	repo := newFakeUserRepo()
	seedUser(t, repo, "u1", "zoe", auth.RoleAdmin, "passw0rd")
	seedUser(t, repo, "u2", "alice", auth.RoleEditor, "passw0rd")

	h := newTestUsersHandler(repo)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/users", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var resp struct {
		Users []UserResponse `json:"users"`
	}
	parseJSONResponse(t, rec, &resp)
	if len(resp.Users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(resp.Users))
	}
	if resp.Users[0].Username != "alice" || resp.Users[1].Username != "zoe" {
		t.Errorf("users not sorted by username asc: %+v", resp.Users)
	}
}

// TestUsersHandler_List_RoleGate verifies the RequireRole middleware in
// front of List returns 403 for a non-admin caller. The handler itself
// does not enforce role, so this exercises the wired pipeline.
func TestUsersHandler_List_RoleGate(t *testing.T) {
	repo := newFakeUserRepo()
	seedUser(t, repo, "u-edit", "editor", auth.RoleEditor, "passw0rd")
	h := newTestUsersHandler(repo)

	router := chi.NewRouter()
	router.With(middleware.RequireRole(auth.RoleAdmin)).Get("/users", h.List)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/users", nil)
	req = withAuthInfo(req, "u-edit", auth.RoleEditor)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("editor on /users = %d, want 403", rec.Code)
	}
}

// TestUsersHandler_List_NoRepo verifies the 503 surfaced when the user
// store is not configured.
func TestUsersHandler_List_NoRepo(t *testing.T) {
	h := newTestUsersHandler(nil)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/users", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	assertStatusCode(t, rec, http.StatusServiceUnavailable)
}

// TestUsersHandler_Create_Success exercises the happy path: the user is
// persisted, the response carries the new UID and role, and the password
// stored in the repo is a bcrypt hash (not the plaintext).
func TestUsersHandler_Create_Success(t *testing.T) {
	repo := newFakeUserRepo()
	h := newTestUsersHandler(repo)

	body := bytes.NewBufferString(`{
		"username": "newbie",
		"password": "passw0rd!",
		"display_name": "New Person",
		"email": "new@example.com",
		"role": "editor"
	}`)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/users", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	assertStatusCode(t, rec, http.StatusCreated)
	var resp UserResponse
	parseJSONResponse(t, rec, &resp)
	if resp.Username != "newbie" {
		t.Errorf("username = %q, want newbie", resp.Username)
	}
	if resp.Role != auth.RoleEditor {
		t.Errorf("role = %q, want editor", resp.Role)
	}
	if resp.UID == "" {
		t.Errorf("expected a generated UID")
	}
	// Body must never echo the plaintext password or a password_hash key.
	raw := rec.Body.String()
	if strings.Contains(raw, "passw0rd") {
		t.Errorf("response leaked plaintext password: %s", raw)
	}
	if strings.Contains(raw, "password_hash") {
		t.Errorf("response leaked password_hash key: %s", raw)
	}
	// The repo must hold a bcrypt hash (starts with $2) — not the plaintext.
	stored, ok := repo.byUsername["newbie"]
	if !ok {
		t.Fatal("user was not persisted")
	}
	if stored.PasswordHash == "passw0rd!" || !strings.HasPrefix(stored.PasswordHash, "$2") {
		t.Errorf("expected bcrypt hash in DB, got %q", stored.PasswordHash)
	}
	if !auth.CheckPassword("passw0rd!", stored.PasswordHash) {
		t.Errorf("stored hash does not match plaintext")
	}
}

// TestUsersHandler_Create_Validation covers the explicit 400 paths:
// short password, invalid role, invalid username shape.
func TestUsersHandler_Create_Validation(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "weak password",
			body: `{"username":"shortpw","password":"abc","role":"viewer"}`,
			want: "password too short",
		},
		{
			name: "invalid role",
			body: `{"username":"badrole","password":"passw0rd!","role":"superuser"}`,
			want: "invalid role",
		},
		{
			name: "invalid username (uppercase)",
			body: `{"username":"BadName","password":"passw0rd!","role":"viewer"}`,
			want: "invalid username",
		},
		{
			name: "invalid username (too short)",
			body: `{"username":"ab","password":"passw0rd!","role":"viewer"}`,
			want: "invalid username",
		},
		{
			name: "invalid username (forbidden char)",
			body: `{"username":"bad name","password":"passw0rd!","role":"viewer"}`,
			want: "invalid username",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeUserRepo()
			h := newTestUsersHandler(repo)
			req := httptest.NewRequestWithContext(
				context.Background(), http.MethodPost, "/api/v1/users",
				bytes.NewBufferString(tt.body),
			)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.Create(rec, req)
			assertStatusCode(t, rec, http.StatusBadRequest)
			assertJSONError(t, rec, tt.want)
		})
	}
}

// TestUsersHandler_Create_TakenUsername verifies the 409 returned when the
// repo reports ErrUsernameTaken.
func TestUsersHandler_Create_TakenUsername(t *testing.T) {
	repo := newFakeUserRepo()
	seedUser(t, repo, "u-existing", "existing", auth.RoleEditor, "passw0rd!")
	h := newTestUsersHandler(repo)

	body := bytes.NewBufferString(`{
		"username": "existing",
		"password": "passw0rd!",
		"role": "viewer"
	}`)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/users", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	assertStatusCode(t, rec, http.StatusConflict)
	assertJSONError(t, rec, "username already taken")
}

// TestUsersHandler_Get_Success exercises the per-user GET happy path.
func TestUsersHandler_Get_Success(t *testing.T) {
	repo := newFakeUserRepo()
	seedUser(t, repo, "u-bob", "bob", auth.RoleViewer, "passw0rd!")
	h := newTestUsersHandler(repo)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/users/u-bob", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "u-bob"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var resp UserResponse
	parseJSONResponse(t, rec, &resp)
	if resp.UID != "u-bob" || resp.Username != "bob" {
		t.Errorf("got %+v", resp)
	}
}

// TestUsersHandler_Get_NotFound confirms a missing UID 404s.
func TestUsersHandler_Get_NotFound(t *testing.T) {
	repo := newFakeUserRepo()
	h := newTestUsersHandler(repo)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/users/u-missing", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "u-missing"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	assertStatusCode(t, rec, http.StatusNotFound)
	assertJSONError(t, rec, "user not found")
}

// TestUsersHandler_Update_Success updates display_name, email, and role.
func TestUsersHandler_Update_Success(t *testing.T) {
	repo := newFakeUserRepo()
	seedUser(t, repo, "u-x", "alex", auth.RoleViewer, "passw0rd!")
	h := newTestUsersHandler(repo)

	body := bytes.NewBufferString(`{"display_name":"Alex","email":"alex@example.com","role":"editor"}`)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/api/v1/users/u-x", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "u-x"})
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var resp UserResponse
	parseJSONResponse(t, rec, &resp)
	if resp.DisplayName != "Alex" || resp.Email != "alex@example.com" || resp.Role != auth.RoleEditor {
		t.Errorf("update did not apply: %+v", resp)
	}
}

// TestUsersHandler_Update_UsernameForbidden verifies the handler rejects an
// attempt to change the username with 400.
func TestUsersHandler_Update_UsernameForbidden(t *testing.T) {
	repo := newFakeUserRepo()
	seedUser(t, repo, "u-x", "alex", auth.RoleViewer, "passw0rd!")
	h := newTestUsersHandler(repo)

	body := bytes.NewBufferString(`{"username":"renamed"}`)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/api/v1/users/u-x", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "u-x"})
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "username cannot be changed")
}

// TestUsersHandler_Update_InvalidRole verifies role validation runs after
// fetching the user.
func TestUsersHandler_Update_InvalidRole(t *testing.T) {
	repo := newFakeUserRepo()
	seedUser(t, repo, "u-x", "alex", auth.RoleViewer, "passw0rd!")
	h := newTestUsersHandler(repo)

	body := bytes.NewBufferString(`{"role":"god"}`)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/api/v1/users/u-x", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "u-x"})
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "invalid role")
}

// TestUsersHandler_SetPassword_Success persists a new hash for the target.
func TestUsersHandler_SetPassword_Success(t *testing.T) {
	repo := newFakeUserRepo()
	seedUser(t, repo, "u-x", "alex", auth.RoleViewer, "old-password")
	h := newTestUsersHandler(repo)

	body := bytes.NewBufferString(`{"password":"brand-new-pw"}`)
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/users/u-x/password", body,
	)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "u-x"})
	rec := httptest.NewRecorder()
	h.SetPassword(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	stored := repo.byUsername["alex"]
	if stored == nil {
		t.Fatal("user disappeared from repo")
	}
	if !auth.CheckPassword("brand-new-pw", stored.PasswordHash) {
		t.Error("new password does not match stored hash")
	}
	if auth.CheckPassword("old-password", stored.PasswordHash) {
		t.Error("old password still verifies — hash was not rotated")
	}
}

// TestUsersHandler_SetPassword_TooShort confirms the length floor is
// re-applied for the admin endpoint as well.
func TestUsersHandler_SetPassword_TooShort(t *testing.T) {
	repo := newFakeUserRepo()
	seedUser(t, repo, "u-x", "alex", auth.RoleViewer, "passw0rd!")
	h := newTestUsersHandler(repo)

	body := bytes.NewBufferString(`{"password":"abc"}`)
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/users/u-x/password", body,
	)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "u-x"})
	rec := httptest.NewRecorder()
	h.SetPassword(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "password too short")
}

// TestUsersHandler_SetDisabled_Toggles confirms both true and false values
// flow through to the repo.
func TestUsersHandler_SetDisabled_Toggles(t *testing.T) {
	repo := newFakeUserRepo()
	seedUser(t, repo, "u-x", "alex", auth.RoleViewer, "passw0rd!")
	h := newTestUsersHandler(repo)

	for _, want := range []bool{true, false} {
		body := bytes.NewBufferString(`{"disabled":` + boolStr(want) + `}`)
		req := httptest.NewRequestWithContext(
			context.Background(), http.MethodPost, "/api/v1/users/u-x/disable", body,
		)
		req.Header.Set("Content-Type", "application/json")
		req = requestWithChiParams(req, map[string]string{"uid": "u-x"})
		rec := httptest.NewRecorder()
		h.SetDisabled(rec, req)
		assertStatusCode(t, rec, http.StatusOK)
		if repo.byUID["u-x"].Disabled != want {
			t.Errorf("Disabled = %v, want %v", repo.byUID["u-x"].Disabled, want)
		}
	}
}

// TestUsersHandler_Delete_Success removes a non-self, non-last-admin user.
func TestUsersHandler_Delete_Success(t *testing.T) {
	repo := newFakeUserRepo()
	seedUser(t, repo, "u-admin", "admin", auth.RoleAdmin, "passw0rd!")
	seedUser(t, repo, "u-victim", "victim", auth.RoleEditor, "passw0rd!")
	h := newTestUsersHandler(repo)

	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodDelete, "/api/v1/users/u-victim", nil,
	)
	req = withAuthInfo(req, "u-admin", auth.RoleAdmin)
	req = requestWithChiParams(req, map[string]string{"uid": "u-victim"})
	rec := httptest.NewRecorder()
	h.Delete(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	if _, ok := repo.byUID["u-victim"]; ok {
		t.Error("victim was not deleted")
	}
}

// TestUsersHandler_Delete_Self refuses to delete the caller themselves.
func TestUsersHandler_Delete_Self(t *testing.T) {
	repo := newFakeUserRepo()
	seedUser(t, repo, "u-admin", "admin", auth.RoleAdmin, "passw0rd!")
	seedUser(t, repo, "u-other", "other", auth.RoleAdmin, "passw0rd!")
	h := newTestUsersHandler(repo)

	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodDelete, "/api/v1/users/u-admin", nil,
	)
	req = withAuthInfo(req, "u-admin", auth.RoleAdmin)
	req = requestWithChiParams(req, map[string]string{"uid": "u-admin"})
	rec := httptest.NewRecorder()
	h.Delete(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "cannot delete yourself")
}

// TestUsersHandler_Delete_LastAdmin refuses to remove the only admin row,
// even when the caller is a different admin (which cannot happen in
// practice — if you are the only admin, you are also the caller, but the
// handler must defend against stale UIDs too).
func TestUsersHandler_Delete_LastAdmin(t *testing.T) {
	repo := newFakeUserRepo()
	seedUser(t, repo, "u-admin", "admin", auth.RoleAdmin, "passw0rd!")
	seedUser(t, repo, "u-editor", "editor", auth.RoleEditor, "passw0rd!")
	// The caller is a phantom admin not in the repo so the self-delete
	// guard does not trip first; only the last-admin guard is exercised.
	h := newTestUsersHandler(repo)

	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodDelete, "/api/v1/users/u-admin", nil,
	)
	req = withAuthInfo(req, "u-phantom-admin", auth.RoleAdmin)
	req = requestWithChiParams(req, map[string]string{"uid": "u-admin"})
	rec := httptest.NewRecorder()
	h.Delete(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "cannot delete the last admin")
	if _, ok := repo.byUID["u-admin"]; !ok {
		t.Error("admin was deleted despite being the last one")
	}
}

// TestUsersHandler_Me_Success returns the current user from the AuthInfo.
func TestUsersHandler_Me_Success(t *testing.T) {
	repo := newFakeUserRepo()
	seedUser(t, repo, "u-me", "currentuser", auth.RoleEditor, "passw0rd!")
	h := newTestUsersHandler(repo)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/me", nil)
	req = withAuthInfo(req, "u-me", auth.RoleEditor)
	rec := httptest.NewRecorder()
	h.Me(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	var resp UserResponse
	parseJSONResponse(t, rec, &resp)
	if resp.UID != "u-me" || resp.Username != "currentuser" || resp.Role != auth.RoleEditor {
		t.Errorf("Me payload = %+v", resp)
	}
}

// TestUsersHandler_Me_NoAuthInfo returns 401 when AuthInfo is absent
// (request did not pass through RequireAuth).
func TestUsersHandler_Me_NoAuthInfo(t *testing.T) {
	repo := newFakeUserRepo()
	h := newTestUsersHandler(repo)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()
	h.Me(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Me without AuthInfo = %d, want 401", rec.Code)
	}
}

// TestUsersHandler_ChangeMyPassword_Success rotates the caller's password.
func TestUsersHandler_ChangeMyPassword_Success(t *testing.T) {
	repo := newFakeUserRepo()
	seedUser(t, repo, "u-me", "currentuser", auth.RoleEditor, "old-password")
	h := newTestUsersHandler(repo)

	body := bytes.NewBufferString(`{"current_password":"old-password","new_password":"brand-new-pw"}`)
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/me/password", body,
	)
	req.Header.Set("Content-Type", "application/json")
	req = withAuthInfo(req, "u-me", auth.RoleEditor)
	rec := httptest.NewRecorder()
	h.ChangeMyPassword(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	stored := repo.byUsername["currentuser"]
	if !auth.CheckPassword("brand-new-pw", stored.PasswordHash) {
		t.Error("new password did not stick")
	}
}

// TestUsersHandler_ChangeMyPassword_WrongCurrent returns 401 with the
// generic message used by Login.
func TestUsersHandler_ChangeMyPassword_WrongCurrent(t *testing.T) {
	repo := newFakeUserRepo()
	seedUser(t, repo, "u-me", "currentuser", auth.RoleEditor, "old-password")
	h := newTestUsersHandler(repo)

	body := bytes.NewBufferString(`{"current_password":"wrong","new_password":"brand-new-pw"}`)
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/me/password", body,
	)
	req.Header.Set("Content-Type", "application/json")
	req = withAuthInfo(req, "u-me", auth.RoleEditor)
	rec := httptest.NewRecorder()
	h.ChangeMyPassword(rec, req)

	assertStatusCode(t, rec, http.StatusUnauthorized)
	assertJSONError(t, rec, errInvalidCredentials)
}

// TestUsersHandler_ChangeMyPassword_NewTooShort confirms the new password
// length requirement is enforced before any hash lookup.
func TestUsersHandler_ChangeMyPassword_NewTooShort(t *testing.T) {
	repo := newFakeUserRepo()
	seedUser(t, repo, "u-me", "currentuser", auth.RoleEditor, "old-password")
	h := newTestUsersHandler(repo)

	body := bytes.NewBufferString(`{"current_password":"old-password","new_password":"x"}`)
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/me/password", body,
	)
	req.Header.Set("Content-Type", "application/json")
	req = withAuthInfo(req, "u-me", auth.RoleEditor)
	rec := httptest.NewRecorder()
	h.ChangeMyPassword(rec, req)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "password too short")
}

// boolStr renders a Go bool as the JSON literal "true" or "false". Inlined
// formatter to keep the disable-toggle table-test bodies readable.
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// Tiny smoke test: response JSON serialises without exposing a
// password_hash field for any of the handlers that return UserResponse.
func TestUsersHandler_ResponseShape(t *testing.T) {
	u := database.User{
		UID:       "u-shape",
		Username:  "shape",
		Role:      auth.RoleAdmin,
		CreatedAt: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	b, err := json.Marshal(toUserResponse(u))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "password") {
		t.Errorf("UserResponse leaked a password-related key: %s", b)
	}
	if !strings.Contains(string(b), `"created_at":"2025-01-02T03:04:05Z"`) {
		t.Errorf("created_at not RFC3339: %s", b)
	}
	if !strings.Contains(string(b), `"last_login_at":null`) {
		t.Errorf("last_login_at should serialise as null when nil: %s", b)
	}
}
