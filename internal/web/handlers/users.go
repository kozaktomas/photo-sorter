package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/audit"
	"github.com/kozaktomas/photo-sorter/internal/auth"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// minPasswordLength is the minimum length accepted for new passwords. The
// floor matches the requirement called out in the user-management spec.
const minPasswordLength = 8

// usernameRegexp matches a valid username: lowercase alphanumerics plus _,
// ., and -, length 3-64. The same shape is documented in the spec and
// already enforced by the bootstrap admin flow.
var usernameRegexp = regexp.MustCompile(`^[a-z0-9_.-]{3,64}$`)

// UsersHandler hosts the admin-only user-management endpoints plus the
// `/me` self-service routes. Every method assumes RequireAuth has populated
// the AuthInfo on the request context; the admin routes additionally pass
// through RequireRole("admin") in the router.
type UsersHandler struct {
	config *config.Config
	repo   database.UserWriter
}

// NewUsersHandler returns a handler bound to the supplied user repository.
// repo may be nil when the user store is unavailable at startup — every
// endpoint then surfaces a 503 rather than blocking server boot.
func NewUsersHandler(cfg *config.Config, repo database.UserWriter) *UsersHandler {
	return &UsersHandler{config: cfg, repo: repo}
}

// UserResponse is the wire shape returned by every user endpoint. It is a
// strict subset of database.User — password_hash is never serialised.
type UserResponse struct {
	UID         string  `json:"uid"`
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	Email       string  `json:"email"`
	Role        string  `json:"role"`
	Disabled    bool    `json:"disabled"`
	CreatedAt   string  `json:"created_at"`
	LastLoginAt *string `json:"last_login_at"`
}

// toUserResponse maps a database.User into the wire shape. LastLoginAt is
// emitted as null when the user has never logged in.
func toUserResponse(u database.User) UserResponse {
	resp := UserResponse{
		UID:         u.UID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Email:       u.Email,
		Role:        u.Role,
		Disabled:    u.Disabled,
		CreatedAt:   u.CreatedAt.UTC().Format(time.RFC3339),
	}
	if u.LastLoginAt != nil && !u.LastLoginAt.IsZero() {
		s := u.LastLoginAt.UTC().Format(time.RFC3339)
		resp.LastLoginAt = &s
	}
	return resp
}

// requireRepo returns the configured user repo, or writes a 503 and returns
// nil when the repo was not wired up at startup.
func (h *UsersHandler) requireRepo(w http.ResponseWriter) database.UserWriter {
	if h.repo != nil {
		return h.repo
	}
	respondError(w, http.StatusServiceUnavailable, "user store unavailable")
	return nil
}

// List returns every user, sorted by username ascending.
func (h *UsersHandler) List(w http.ResponseWriter, r *http.Request) {
	repo := h.requireRepo(w)
	if repo == nil {
		return
	}
	users, err := repo.ListUsers(r.Context())
	if err != nil {
		log.Printf("users list: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	// ListUsers already orders by username in the production repo; sort
	// again so an in-memory test fake (which iterates a map) still produces
	// the documented order.
	sort.Slice(users, func(i, j int) bool {
		return users[i].Username < users[j].Username
	})
	out := make([]UserResponse, 0, len(users))
	for _, u := range users {
		out = append(out, toUserResponse(u))
	}
	respondJSON(w, http.StatusOK, map[string]any{"users": out})
}

// Get returns a single user by UID.
func (h *UsersHandler) Get(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "uid is required")
		return
	}
	repo := h.requireRepo(w)
	if repo == nil {
		return
	}
	user, err := repo.GetUser(r.Context(), uid)
	if errors.Is(err, database.ErrNotFound) {
		respondError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		log.Printf("users get %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get user")
		return
	}
	respondJSON(w, http.StatusOK, toUserResponse(*user))
}

// CreateUserRequest is the JSON body accepted by POST /api/v1/users.
type CreateUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Role        string `json:"role"`
}

// Create inserts a new user. Validates username / password / role; the
// generated UID and bcrypt hash are set by the repo, and a username
// collision yields 409.
func (h *UsersHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if !usernameRegexp.MatchString(req.Username) {
		respondError(w, http.StatusBadRequest, "invalid username")
		return
	}
	if len(req.Password) < minPasswordLength {
		respondError(w, http.StatusBadRequest, "password too short")
		return
	}
	if !auth.IsValidRole(req.Role) {
		respondError(w, http.StatusBadRequest, "invalid role")
		return
	}
	repo := h.requireRepo(w)
	if repo == nil {
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		log.Printf("users create hash: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}
	user := &database.UserWithSecret{
		User: database.User{
			Username:    req.Username,
			DisplayName: req.DisplayName,
			Email:       req.Email,
			Role:        req.Role,
		},
		PasswordHash: hash,
	}
	if err := repo.CreateUser(r.Context(), user); err != nil {
		if errors.Is(err, database.ErrUsernameTaken) {
			respondError(w, http.StatusConflict, "username already taken")
			return
		}
		log.Printf("users create: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	audit.FromContext(r.Context()).Log(
		r.Context(), audit.ActionUserCreate, audit.EntityUser, user.UID,
		map[string]any{"username": user.Username, "role": user.Role},
	)
	respondJSON(w, http.StatusCreated, toUserResponse(user.User))
}

// UpdateUserRequest is the JSON body accepted by PUT /api/v1/users/{uid}.
// Pointers preserve the distinction between "key omitted" and "key set to
// the zero value", so a caller can clear display_name while leaving role
// alone.
type UpdateUserRequest struct {
	DisplayName *string `json:"display_name,omitempty"`
	Email       *string `json:"email,omitempty"`
	Role        *string `json:"role,omitempty"`
	Username    *string `json:"username,omitempty"`
}

// applyUserUpdateFields copies the supplied request fields into the target
// user. Returns false (after writing the error response) when the request
// attempts to change the immutable username or supplies an invalid role.
func applyUserUpdateFields(w http.ResponseWriter, user *database.User, req UpdateUserRequest) bool {
	if req.Username != nil && *req.Username != user.Username {
		respondError(w, http.StatusBadRequest, "username cannot be changed")
		return false
	}
	if req.Role != nil {
		if !auth.IsValidRole(*req.Role) {
			respondError(w, http.StatusBadRequest, "invalid role")
			return false
		}
		user.Role = *req.Role
	}
	if req.DisplayName != nil {
		user.DisplayName = *req.DisplayName
	}
	if req.Email != nil {
		user.Email = *req.Email
	}
	return true
}

// Update changes display_name / email / role. Attempting to change the
// username is rejected with 400.
func (h *UsersHandler) Update(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "uid is required")
		return
	}
	var req UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	repo := h.requireRepo(w)
	if repo == nil {
		return
	}
	user, err := repo.GetUser(r.Context(), uid)
	if errors.Is(err, database.ErrNotFound) {
		respondError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		log.Printf("users update get %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get user")
		return
	}
	if !applyUserUpdateFields(w, user, req) {
		return
	}
	if err := repo.UpdateUser(r.Context(), user); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			respondError(w, http.StatusNotFound, "user not found")
			return
		}
		log.Printf("users update %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to update user")
		return
	}
	audit.FromContext(r.Context()).Log(
		r.Context(), audit.ActionUserUpdate, audit.EntityUser, user.UID,
		map[string]any{"username": user.Username, "role": user.Role},
	)
	respondJSON(w, http.StatusOK, toUserResponse(*user))
}

// SetPasswordRequest is the JSON body accepted by POST /users/{uid}/password.
type SetPasswordRequest struct {
	Password string `json:"password"`
}

// SetPassword hashes and stores a new password for the target user.
func (h *UsersHandler) SetPassword(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "uid is required")
		return
	}
	var req SetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if len(req.Password) < minPasswordLength {
		respondError(w, http.StatusBadRequest, "password too short")
		return
	}
	repo := h.requireRepo(w)
	if repo == nil {
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		log.Printf("users set password hash: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}
	if err := repo.SetPassword(r.Context(), uid, hash); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			respondError(w, http.StatusNotFound, "user not found")
			return
		}
		log.Printf("users set password %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to set password")
		return
	}
	audit.FromContext(r.Context()).Log(
		r.Context(), audit.ActionUserPasswordReset, audit.EntityUser, uid, nil,
	)
	respondJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// SetDisabledRequest is the JSON body accepted by POST /users/{uid}/disable.
type SetDisabledRequest struct {
	Disabled bool `json:"disabled"`
}

// SetDisabled toggles the disabled flag for the target user.
func (h *UsersHandler) SetDisabled(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "uid is required")
		return
	}
	var req SetDisabledRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	repo := h.requireRepo(w)
	if repo == nil {
		return
	}
	if err := repo.SetDisabled(r.Context(), uid, req.Disabled); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			respondError(w, http.StatusNotFound, "user not found")
			return
		}
		log.Printf("users set disabled %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to set disabled")
		return
	}
	action := audit.ActionUserEnable
	if req.Disabled {
		action = audit.ActionUserDisable
	}
	audit.FromContext(r.Context()).Log(
		r.Context(), action, audit.EntityUser, uid, nil,
	)
	respondJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ensureNotLastAdmin returns true when removing `targetUID` would leave the
// users table with no enabled admin remaining. The check is a no-op when
// the target is not an admin. Repo errors are surfaced as a 500 response
// and the function returns true so the caller bails out.
func ensureNotLastAdmin(
	w http.ResponseWriter, r *http.Request, repo database.UserWriter,
	target *database.User, targetUID string,
) bool {
	if target.Role != auth.RoleAdmin {
		return false
	}
	users, err := repo.ListUsers(r.Context())
	if err != nil {
		log.Printf("users delete list: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to list users")
		return true
	}
	for _, u := range users {
		if u.UID == targetUID {
			continue
		}
		if u.Role == auth.RoleAdmin && !u.Disabled {
			return false
		}
	}
	respondError(w, http.StatusBadRequest, "cannot delete the last admin")
	return true
}

// Delete hard-deletes a user. Rejects deleting the caller and the last admin.
func (h *UsersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "uid is required")
		return
	}
	info := middleware.MustGetAuthInfo(r.Context(), w)
	if info == nil {
		return
	}
	if uid == info.UserUID {
		respondError(w, http.StatusBadRequest, "cannot delete yourself")
		return
	}
	repo := h.requireRepo(w)
	if repo == nil {
		return
	}
	target, err := repo.GetUser(r.Context(), uid)
	if errors.Is(err, database.ErrNotFound) {
		respondError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		log.Printf("users delete get %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get user")
		return
	}
	if ensureNotLastAdmin(w, r, repo, target, uid) {
		return
	}
	if err := repo.DeleteUser(r.Context(), uid); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			respondError(w, http.StatusNotFound, "user not found")
			return
		}
		log.Printf("users delete %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}
	audit.FromContext(r.Context()).Log(
		r.Context(), audit.ActionUserDelete, audit.EntityUser, uid,
		map[string]any{"username": target.Username},
	)
	respondJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// Me returns the currently authenticated user.
func (h *UsersHandler) Me(w http.ResponseWriter, r *http.Request) {
	info := middleware.MustGetAuthInfo(r.Context(), w)
	if info == nil {
		return
	}
	repo := h.requireRepo(w)
	if repo == nil {
		return
	}
	user, err := repo.GetUser(r.Context(), info.UserUID)
	if errors.Is(err, database.ErrNotFound) {
		respondError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		log.Printf("users me %s: %v", sanitizeForLog(info.UserUID), err)
		respondError(w, http.StatusInternalServerError, "failed to get user")
		return
	}
	respondJSON(w, http.StatusOK, toUserResponse(*user))
}

// ChangeMyPasswordRequest is the JSON body accepted by POST /api/v1/me/password.
type ChangeMyPasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// verifyCurrentPassword resolves the caller by UID, re-fetches the bcrypt
// hash via the username-keyed lookup, and compares it to `current`. Returns
// true on success. Any failure path writes its own response and returns
// false; callers must return immediately.
func verifyCurrentPassword(
	w http.ResponseWriter, r *http.Request, repo database.UserWriter,
	uid, current string,
) bool {
	user, err := repo.GetUser(r.Context(), uid)
	if errors.Is(err, database.ErrNotFound) {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": errInvalidCredentials})
		return false
	}
	if err != nil {
		log.Printf("users change pwd get %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get user")
		return false
	}
	withSecret, err := repo.GetUserByUsername(r.Context(), user.Username)
	if err != nil {
		log.Printf("users change pwd secret %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get user")
		return false
	}
	if !auth.CheckPassword(current, withSecret.PasswordHash) {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": errInvalidCredentials})
		return false
	}
	return true
}

// ChangeMyPassword verifies the caller's current password against the stored
// hash and applies a new one. Wrong current password yields 401; a too-short
// new password yields 400.
func (h *UsersHandler) ChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	info := middleware.MustGetAuthInfo(r.Context(), w)
	if info == nil {
		return
	}
	var req ChangeMyPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if len(req.NewPassword) < minPasswordLength {
		respondError(w, http.StatusBadRequest, "password too short")
		return
	}
	repo := h.requireRepo(w)
	if repo == nil {
		return
	}
	if !verifyCurrentPassword(w, r, repo, info.UserUID, req.CurrentPassword) {
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		log.Printf("users change pwd hash: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}
	if err := repo.SetPassword(r.Context(), info.UserUID, hash); err != nil {
		log.Printf("users change pwd set %s: %v", sanitizeForLog(info.UserUID), err)
		respondError(w, http.StatusInternalServerError, "failed to set password")
		return
	}
	audit.FromContext(r.Context()).Log(
		r.Context(), audit.ActionPasswordChange, audit.EntityUser, info.UserUID, nil,
	)
	respondJSON(w, http.StatusOK, map[string]bool{"success": true})
}
