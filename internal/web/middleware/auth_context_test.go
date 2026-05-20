package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/auth"
)

// TestGetAuthInfo_present verifies that AuthInfo placed on the context by
// SetAuthInfoInContext is round-tripped correctly.
func TestGetAuthInfo_present(t *testing.T) {
	t.Parallel()
	ctx := SetAuthInfoInContext(context.Background(), &AuthInfo{
		UserUID: "u123", Username: "alice", Role: auth.RoleAdmin,
	})
	got, ok := GetAuthInfo(ctx)
	if !ok {
		t.Fatal("GetAuthInfo returned false for a populated context")
	}
	if got.UserUID != "u123" || got.Role != auth.RoleAdmin {
		t.Errorf("GetAuthInfo = %+v, want {UID=u123 Role=admin}", got)
	}
}

// TestGetAuthInfo_missing verifies the second return value is false when
// the context has no AuthInfo attached.
func TestGetAuthInfo_missing(t *testing.T) {
	t.Parallel()
	if got, ok := GetAuthInfo(context.Background()); ok || got != nil {
		t.Errorf("GetAuthInfo on empty context = (%v, %v), want (nil, false)", got, ok)
	}
}

// TestMustGetAuthInfo_writes401 verifies that the helper writes a 401
// response and returns nil when no AuthInfo is present.
func TestMustGetAuthInfo_writes401(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	if got := MustGetAuthInfo(context.Background(), w); got != nil {
		t.Errorf("MustGetAuthInfo = %v, want nil", got)
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestRequireRole covers the three observable outcomes of the gate:
// allowed, forbidden, and unauthorized.
func TestRequireRole(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		setupCtx   func(ctx context.Context) context.Context
		allowed    []string
		wantStatus int
	}{
		{
			name: "admin allowed by admin gate",
			setupCtx: func(ctx context.Context) context.Context {
				return SetAuthInfoInContext(ctx, &AuthInfo{
					UserUID: "u1", Role: auth.RoleAdmin,
				})
			},
			allowed:    []string{auth.RoleAdmin},
			wantStatus: http.StatusOK,
		},
		{
			name: "editor blocked by admin gate",
			setupCtx: func(ctx context.Context) context.Context {
				return SetAuthInfoInContext(ctx, &AuthInfo{
					UserUID: "u2", Role: auth.RoleEditor,
				})
			},
			allowed:    []string{auth.RoleAdmin},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "viewer blocked by admin gate",
			setupCtx: func(ctx context.Context) context.Context {
				return SetAuthInfoInContext(ctx, &AuthInfo{
					UserUID: "u3", Role: auth.RoleViewer,
				})
			},
			allowed:    []string{auth.RoleAdmin},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "editor allowed by editor+admin gate",
			setupCtx: func(ctx context.Context) context.Context {
				return SetAuthInfoInContext(ctx, &AuthInfo{
					UserUID: "u4", Role: auth.RoleEditor,
				})
			},
			allowed:    []string{auth.RoleAdmin, auth.RoleEditor},
			wantStatus: http.StatusOK,
		},
		{
			name: "no auth info yields 401",
			setupCtx: func(ctx context.Context) context.Context {
				return ctx
			},
			allowed:    []string{auth.RoleAdmin},
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			})
			handler := RequireRole(tt.allowed...)(next)
			req := httptest.NewRequestWithContext(
				tt.setupCtx(context.Background()), http.MethodGet, "/", nil,
			)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			wantCalled := tt.wantStatus == http.StatusOK
			if called != wantCalled {
				t.Errorf("next called = %v, want %v", called, wantCalled)
			}
		})
	}
}

// TestRequireAuth_setsAuthInfo confirms RequireAuth populates AuthInfo from
// the session row so downstream handlers can role-gate without a DB hit.
func TestRequireAuth_setsAuthInfo(t *testing.T) {
	sm := NewSessionManager("test-secret", nil)
	session, _ := sm.CreateSession("", "", "u-test", auth.RoleEditor)

	var got *AuthInfo
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		info, _ := GetAuthInfo(r.Context())
		got = info
	})

	handler := RequireAuth(sm)(next)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+session.ID)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if got == nil {
		t.Fatal("AuthInfo not populated by RequireAuth")
	}
	if got.UserUID != "u-test" || got.Role != auth.RoleEditor {
		t.Errorf("AuthInfo = %+v, want {UID=u-test Role=editor}", got)
	}
}
