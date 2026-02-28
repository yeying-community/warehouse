package auth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeying-community/warehouse/internal/application/assetspace"
	domainauth "github.com/yeying-community/warehouse/internal/domain/auth"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

func TestWeb3AuthenticatorAuthenticateByJWTWallet(t *testing.T) {
	repo := newStubUserRepo()

	u := user.NewUser("alice", "alice")
	if err := u.SetWalletAddress("0x1111111111111111111111111111111111111111"); err != nil {
		t.Fatalf("SetWalletAddress failed: %v", err)
	}
	if err := repo.Save(context.Background(), u); err != nil {
		t.Fatalf("Save user failed: %v", err)
	}

	tmpDir := t.TempDir()
	authenticator := newJWTTestAuthenticator(t, repo, tmpDir)

	token, err := authenticator.GenerateAccessToken(u.WalletAddress)
	if err != nil {
		t.Fatalf("GenerateAccessToken failed: %v", err)
	}

	got, err := authenticator.Authenticate(context.Background(), &domainauth.BearerCredentials{Token: token.Value})
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if got.Username != u.Username {
		t.Fatalf("unexpected username: want %s, got %s", u.Username, got.Username)
	}

	assertDirExists(t, filepath.Join(tmpDir, "alice", "personal"))
	assertDirExists(t, filepath.Join(tmpDir, "alice", "apps"))
}

func TestWeb3AuthenticatorAuthenticateByJWTEmail(t *testing.T) {
	repo := newStubUserRepo()

	u := user.NewUser("bob", "bob")
	if err := u.SetEmail("bob@example.com"); err != nil {
		t.Fatalf("SetEmail failed: %v", err)
	}
	if err := repo.Save(context.Background(), u); err != nil {
		t.Fatalf("Save user failed: %v", err)
	}

	tmpDir := t.TempDir()
	authenticator := newJWTTestAuthenticator(t, repo, tmpDir)

	token, err := authenticator.GenerateAccessTokenForEmail("bob@example.com")
	if err != nil {
		t.Fatalf("GenerateAccessTokenForEmail failed: %v", err)
	}

	got, err := authenticator.Authenticate(context.Background(), &domainauth.BearerCredentials{Token: token.Value})
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if got.Email != "bob@example.com" {
		t.Fatalf("unexpected email: %s", got.Email)
	}
}

func TestWeb3AuthenticatorEnrichContext_JWTShouldNotInjectUcanContext(t *testing.T) {
	repo := newStubUserRepo()
	u := user.NewUser("charlie", "charlie")
	if err := u.SetWalletAddress("0x2222222222222222222222222222222222222222"); err != nil {
		t.Fatalf("SetWalletAddress failed: %v", err)
	}
	if err := repo.Save(context.Background(), u); err != nil {
		t.Fatalf("Save user failed: %v", err)
	}

	authenticator := newJWTTestAuthenticator(t, repo, t.TempDir())
	token, err := authenticator.GenerateAccessToken(u.WalletAddress)
	if err != nil {
		t.Fatalf("GenerateAccessToken failed: %v", err)
	}

	ctx := authenticator.EnrichContext(context.Background(), &domainauth.BearerCredentials{Token: token.Value})
	if _, ok := middleware.GetUcanContext(ctx); ok {
		t.Fatalf("JWT token should not inject UCAN context")
	}
}

func newJWTTestAuthenticator(t *testing.T, repo user.Repository, webdavRoot string) *Web3Authenticator {
	t.Helper()

	cfg := &config.Config{
		WebDAV: config.WebDAVConfig{Directory: webdavRoot},
		Web3: config.Web3Config{
			UCAN: config.UCANConfig{
				AppScope: config.AppScopeConfig{
					PathPrefix: "/apps",
				},
			},
		},
	}
	manager := assetspace.NewManager(cfg, zap.NewNop())

	return NewWeb3Authenticator(
		repo,
		"abcdefghijklmnopqrstuvwxyz012345",
		time.Hour,
		24*time.Hour,
		nil,
		manager,
		zap.NewNop(),
		false,
	)
}

type stubUserRepo struct {
	byID       map[string]*user.User
	byUsername map[string]*user.User
	byWallet   map[string]*user.User
	byEmail    map[string]*user.User
}

func newStubUserRepo() *stubUserRepo {
	return &stubUserRepo{
		byID:       make(map[string]*user.User),
		byUsername: make(map[string]*user.User),
		byWallet:   make(map[string]*user.User),
		byEmail:    make(map[string]*user.User),
	}
}

func (r *stubUserRepo) FindByUsername(ctx context.Context, username string) (*user.User, error) {
	if u, ok := r.byUsername[username]; ok {
		return cloneUser(u), nil
	}
	return nil, user.ErrUserNotFound
}

func (r *stubUserRepo) FindByWalletAddress(ctx context.Context, address string) (*user.User, error) {
	if u, ok := r.byWallet[address]; ok {
		return cloneUser(u), nil
	}
	return nil, user.ErrUserNotFound
}

func (r *stubUserRepo) FindByEmail(ctx context.Context, email string) (*user.User, error) {
	if u, ok := r.byEmail[email]; ok {
		return cloneUser(u), nil
	}
	return nil, user.ErrUserNotFound
}

func (r *stubUserRepo) FindByID(ctx context.Context, id string) (*user.User, error) {
	if u, ok := r.byID[id]; ok {
		return cloneUser(u), nil
	}
	return nil, user.ErrUserNotFound
}

func (r *stubUserRepo) Save(ctx context.Context, u *user.User) error {
	if u == nil {
		return nil
	}
	copy := cloneUser(u)
	r.byID[copy.ID] = copy
	r.byUsername[copy.Username] = copy
	if copy.WalletAddress != "" {
		r.byWallet[copy.WalletAddress] = copy
	}
	if copy.Email != "" {
		r.byEmail[copy.Email] = copy
	}
	return nil
}

func (r *stubUserRepo) Delete(ctx context.Context, username string) error {
	u, ok := r.byUsername[username]
	if !ok {
		return user.ErrUserNotFound
	}
	delete(r.byUsername, username)
	delete(r.byID, u.ID)
	if u.WalletAddress != "" {
		delete(r.byWallet, u.WalletAddress)
	}
	if u.Email != "" {
		delete(r.byEmail, u.Email)
	}
	return nil
}

func (r *stubUserRepo) List(ctx context.Context) ([]*user.User, error) {
	out := make([]*user.User, 0, len(r.byID))
	for _, u := range r.byID {
		out = append(out, cloneUser(u))
	}
	return out, nil
}

func (r *stubUserRepo) UpdateUsedSpace(ctx context.Context, username string, usedSpace int64) error {
	u, ok := r.byUsername[username]
	if !ok {
		return user.ErrUserNotFound
	}
	if err := u.UpdateUsedSpace(usedSpace); err != nil {
		return err
	}
	return nil
}

func (r *stubUserRepo) UpdateQuota(ctx context.Context, username string, quota int64) error {
	u, ok := r.byUsername[username]
	if !ok {
		return user.ErrUserNotFound
	}
	if err := u.SetQuota(quota); err != nil {
		return err
	}
	return nil
}

func cloneUser(in *user.User) *user.User {
	if in == nil {
		return nil
	}
	out := *in
	if in.Permissions != nil {
		p := *in.Permissions
		out.Permissions = &p
	}
	if len(in.Rules) > 0 {
		out.Rules = make([]*user.Rule, len(in.Rules))
		for i, rule := range in.Rules {
			if rule == nil {
				continue
			}
			rCopy := *rule
			if rule.Permissions != nil {
				p := *rule.Permissions
				rCopy.Permissions = &p
			}
			out.Rules[i] = &rCopy
		}
	}
	return &out
}

func assertDirExists(t *testing.T, dir string) {
	t.Helper()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("expected directory %s exists: %v", dir, err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %s is directory", dir)
	}
}
