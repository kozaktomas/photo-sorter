package auth

import (
	"context"
	"sync"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
)

// fakeBootstrapRepo is a tiny in-memory user store sufficient to drive
// BootstrapAdmin's two relevant paths: "no users exist" and "users
// already exist". It records every CreateUser call so the test can
// assert idempotency.
type fakeBootstrapRepo struct {
	mu       sync.Mutex
	users    map[string]*database.UserWithSecret
	creates  int
	countErr error
}

func newFakeBootstrapRepo() *fakeBootstrapRepo {
	return &fakeBootstrapRepo{users: map[string]*database.UserWithSecret{}}
}

func (r *fakeBootstrapRepo) CountUsers(_ context.Context) (int, error) {
	if r.countErr != nil {
		return 0, r.countErr
	}
	return len(r.users), nil
}

func (r *fakeBootstrapRepo) GetUser(_ context.Context, uid string) (*database.User, error) {
	for _, u := range r.users {
		if u.UID == uid {
			cp := u.User
			return &cp, nil
		}
	}
	return nil, database.ErrNotFound
}

func (r *fakeBootstrapRepo) GetUserByUsername(_ context.Context, username string) (*database.UserWithSecret, error) {
	if u, ok := r.users[username]; ok {
		cp := *u
		return &cp, nil
	}
	return nil, database.ErrNotFound
}

func (r *fakeBootstrapRepo) ListUsers(_ context.Context) ([]database.User, error) {
	out := make([]database.User, 0, len(r.users))
	for _, u := range r.users {
		out = append(out, u.User)
	}
	return out, nil
}

func (r *fakeBootstrapRepo) CreateUser(_ context.Context, u *database.UserWithSecret) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.users[u.Username]; ok {
		return database.ErrUsernameTaken
	}
	if u.UID == "" {
		u.UID = "u-fake-" + u.Username
	}
	cp := *u
	r.users[u.Username] = &cp
	r.creates++
	return nil
}

func (r *fakeBootstrapRepo) UpdateUser(_ context.Context, _ *database.User) error  { return nil }
func (r *fakeBootstrapRepo) SetPassword(_ context.Context, _, _ string) error      { return nil }
func (r *fakeBootstrapRepo) SetDisabled(_ context.Context, _ string, _ bool) error { return nil }
func (r *fakeBootstrapRepo) TouchLastLogin(_ context.Context, _ string) error      { return nil }
func (r *fakeBootstrapRepo) DeleteUser(_ context.Context, _ string) error          { return nil }

// TestBootstrapAdmin_Idempotent verifies the second call is a no-op
// once any user exists, even with the env vars still set — re-running
// must not create a duplicate or rotate the existing admin's password.
func TestBootstrapAdmin_Idempotent(t *testing.T) {
	repo := newFakeBootstrapRepo()
	t.Setenv("BOOTSTRAP_ADMIN_USERNAME", "rootadmin")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "passw0rd!")

	cfg := config.Config{}
	ctx := context.Background()

	if err := BootstrapAdmin(ctx, repo, repo, cfg); err != nil {
		t.Fatalf("first BootstrapAdmin: %v", err)
	}
	if repo.creates != 1 {
		t.Fatalf("first call creates = %d, want 1", repo.creates)
	}
	originalHash := repo.users["rootadmin"].PasswordHash

	if err := BootstrapAdmin(ctx, repo, repo, cfg); err != nil {
		t.Fatalf("second BootstrapAdmin: %v", err)
	}
	if repo.creates != 1 {
		t.Errorf("second call created a duplicate row (creates = %d)", repo.creates)
	}
	if repo.users["rootadmin"].PasswordHash != originalHash {
		t.Error("existing admin's password hash was rewritten by a second bootstrap")
	}
}

// TestBootstrapAdmin_RejectsInvalidUsername confirms the username
// shape gate runs: a value the DB CHECK constraint would refuse is
// flagged with a WARN at the boot stage instead.
func TestBootstrapAdmin_RejectsInvalidUsername(t *testing.T) {
	repo := newFakeBootstrapRepo()
	t.Setenv("BOOTSTRAP_ADMIN_USERNAME", "UPPER")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "passw0rd!")

	if err := BootstrapAdmin(context.Background(), repo, repo, config.Config{}); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	if repo.creates != 0 {
		t.Errorf("created %d users for an invalid username; want 0", repo.creates)
	}
}

// TestBootstrapAdmin_RejectsShortPassword confirms the password length
// floor matches MinPasswordLength.
func TestBootstrapAdmin_RejectsShortPassword(t *testing.T) {
	repo := newFakeBootstrapRepo()
	t.Setenv("BOOTSTRAP_ADMIN_USERNAME", "rootadmin")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "short")

	if err := BootstrapAdmin(context.Background(), repo, repo, config.Config{}); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	if repo.creates != 0 {
		t.Errorf("created %d users with a too-short password; want 0", repo.creates)
	}
}

// TestBootstrapAdmin_MissingEnvIsWarnNotError verifies the documented
// "no-op when env vars are missing" path: server still starts.
func TestBootstrapAdmin_MissingEnvIsWarnNotError(t *testing.T) {
	repo := newFakeBootstrapRepo()
	t.Setenv("BOOTSTRAP_ADMIN_USERNAME", "")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "")

	if err := BootstrapAdmin(context.Background(), repo, repo, config.Config{}); err != nil {
		t.Fatalf("BootstrapAdmin returned error on missing env: %v", err)
	}
	if repo.creates != 0 {
		t.Errorf("created %d users when env was empty; want 0", repo.creates)
	}
}
