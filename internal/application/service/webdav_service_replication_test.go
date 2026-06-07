package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/permission"
	"github.com/yeying-community/warehouse/internal/domain/quota"
	"github.com/yeying-community/warehouse/internal/domain/recycle"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

func TestWebDAVServeHTTPPutDegradesWhenReplicationPeerUnavailable(t *testing.T) {
	rootDir := t.TempDir()
	cfg := &config.Config{
		WebDAV: config.WebDAVConfig{
			Prefix:              "/dav",
			Directory:           rootDir,
			AutoCreateDirectory: true,
			NoSniff:             true,
		},
	}

	userRepo := newTestUserRepo()
	u := user.NewUser("alice", "alice")
	u.Permissions = user.FullPermissions()
	u.Quota = 0
	if err := userRepo.Save(context.Background(), u); err != nil {
		t.Fatalf("save user: %v", err)
	}

	recorder := &testMutationRecorder{
		upsertFileErr: ErrReplicationPeerUnavailable,
	}
	svc := NewWebDAVService(
		cfg,
		allowPermissionChecker{},
		quota.NewService(userRepo),
		userRepo,
		&testRecycleRepo{},
		recorder,
		zap.NewNop(),
	)

	req := httptest.NewRequest(http.MethodPut, "/dav/personal/test.txt", strings.NewReader("hello"))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, u))
	resp := httptest.NewRecorder()

	svc.ServeHTTP(resp, req)

	if resp.Code < 200 || resp.Code >= 300 {
		t.Fatalf("expected PUT to succeed without standby, got status=%d body=%q", resp.Code, resp.Body.String())
	}
	fullPath := svc.resolveUserFullPath(svc.getUserDirectory(u), req.URL.Path)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("expected file to exist after PUT, got %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected file content: %q", string(data))
	}
	if recorder.upsertFileCalls != 1 {
		t.Fatalf("expected mutation recorder to be called once, got %d", recorder.upsertFileCalls)
	}
}

func TestWebDAVServeHTTPPutStillFailsOnNonReplicationMutationError(t *testing.T) {
	rootDir := t.TempDir()
	cfg := &config.Config{
		WebDAV: config.WebDAVConfig{
			Prefix:              "/dav",
			Directory:           rootDir,
			AutoCreateDirectory: true,
			NoSniff:             true,
		},
	}

	userRepo := newTestUserRepo()
	u := user.NewUser("alice", "alice")
	u.Permissions = user.FullPermissions()
	u.Quota = 0
	if err := userRepo.Save(context.Background(), u); err != nil {
		t.Fatalf("save user: %v", err)
	}

	recorder := &testMutationRecorder{
		upsertFileErr: errors.New("append outbox failed"),
	}
	svc := NewWebDAVService(
		cfg,
		allowPermissionChecker{},
		quota.NewService(userRepo),
		userRepo,
		&testRecycleRepo{},
		recorder,
		zap.NewNop(),
	)

	req := httptest.NewRequest(http.MethodPut, "/dav/personal/test.txt", strings.NewReader("hello"))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, u))
	resp := httptest.NewRecorder()

	svc.ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected PUT to fail on non-replication mutation error, got status=%d body=%q", resp.Code, resp.Body.String())
	}
}

func TestWebDAVServeHTTPDeleteDegradesWhenReplicationPeerUnavailable(t *testing.T) {
	rootDir := t.TempDir()
	cfg := &config.Config{
		WebDAV: config.WebDAVConfig{
			Prefix:              "/dav",
			Directory:           rootDir,
			AutoCreateDirectory: true,
			NoSniff:             true,
		},
	}

	userRepo := newTestUserRepo()
	u := user.NewUser("alice", "alice")
	u.Permissions = user.FullPermissions()
	u.Quota = 0
	if err := userRepo.Save(context.Background(), u); err != nil {
		t.Fatalf("save user: %v", err)
	}

	svc := NewWebDAVService(
		cfg,
		allowPermissionChecker{},
		quota.NewService(userRepo),
		userRepo,
		&testRecycleRepo{},
		&testMutationRecorder{movePathErr: ErrReplicationPeerUnavailable},
		zap.NewNop(),
	)

	userDir := svc.getUserDirectory(u)
	if err := os.MkdirAll(filepath.Join(userDir, "personal"), 0o755); err != nil {
		t.Fatalf("mkdir personal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "personal", "test.txt"), []byte("bye"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/dav/personal/test.txt", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, u))
	resp := httptest.NewRecorder()

	svc.ServeHTTP(resp, req)

	if resp.Code < 200 || resp.Code >= 300 {
		t.Fatalf("expected DELETE to succeed without standby, got status=%d body=%q", resp.Code, resp.Body.String())
	}
	if _, err := os.Stat(filepath.Join(userDir, "personal", "test.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected source file removed, got err=%v", err)
	}
	recycleEntries, err := os.ReadDir(svc.recycleDir)
	if err != nil {
		t.Fatalf("read recycle dir: %v", err)
	}
	if len(recycleEntries) != 1 {
		t.Fatalf("expected one recycle entry, got %d", len(recycleEntries))
	}
}

func TestWebDAVServeHTTPDeleteHardDeletesSyncArtifacts(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		targetPath string
		isDir      bool
	}{
		{
			name:       "lock metadata file",
			targetPath: "apps/localhost-3020/backup.__sync_mutex_v1.__sync_lock_v1/lock.json",
		},
		{
			name:       "lock directory",
			targetPath: "apps/localhost-3020/backup.__sync_mutex_v1.__sync_lock_v1",
			isDir:      true,
		},
		{
			name:       "transaction payload",
			targetPath: "apps/localhost-3020/backup.__sync_txn_data_v1.123-abc.json",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rootDir := t.TempDir()
			cfg := &config.Config{
				WebDAV: config.WebDAVConfig{
					Prefix:              "/dav",
					Directory:           rootDir,
					AutoCreateDirectory: true,
					NoSniff:             true,
				},
			}

			userRepo := newTestUserRepo()
			u := user.NewUser("alice", "alice")
			u.Permissions = user.FullPermissions()
			u.Quota = 0
			if err := userRepo.Save(context.Background(), u); err != nil {
				t.Fatalf("save user: %v", err)
			}

			recycleRepo := &testRecycleRepo{}
			recorder := &testMutationRecorder{}
			svc := NewWebDAVService(
				cfg,
				allowPermissionChecker{},
				quota.NewService(userRepo),
				userRepo,
				recycleRepo,
				recorder,
				zap.NewNop(),
			)

			userDir := svc.getUserDirectory(u)
			fullPath := filepath.Join(userDir, filepath.FromSlash(tc.targetPath))
			if tc.isDir {
				if err := os.MkdirAll(filepath.Join(fullPath, "nested"), 0o755); err != nil {
					t.Fatalf("seed lock dir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(fullPath, "nested", "marker.txt"), []byte("x"), 0o644); err != nil {
					t.Fatalf("seed nested file: %v", err)
				}
			} else {
				if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
					t.Fatalf("mkdir parent: %v", err)
				}
				if err := os.WriteFile(fullPath, []byte("sync"), 0o644); err != nil {
					t.Fatalf("seed file: %v", err)
				}
			}

			req := httptest.NewRequest(http.MethodDelete, "/dav/"+tc.targetPath, nil)
			req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, u))
			resp := httptest.NewRecorder()

			svc.ServeHTTP(resp, req)

			if resp.Code < 200 || resp.Code >= 300 {
				t.Fatalf("expected hard delete to succeed, got status=%d body=%q", resp.Code, resp.Body.String())
			}
			if _, err := os.Stat(fullPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("expected target removed, got err=%v", err)
			}
			if _, err := os.Stat(svc.recycleDir); err == nil {
				recycleEntries, readErr := os.ReadDir(svc.recycleDir)
				if readErr != nil {
					t.Fatalf("read recycle dir: %v", readErr)
				}
				if len(recycleEntries) != 0 {
					t.Fatalf("expected no recycle entries, got %d", len(recycleEntries))
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("stat recycle dir: %v", err)
			}
			if recycleRepo.createCalls != 0 {
				t.Fatalf("expected recycle repo to stay untouched, got %d create calls", recycleRepo.createCalls)
			}
			if recorder.removePathCalls != 1 {
				t.Fatalf("expected one hard delete mutation record, got %d", recorder.removePathCalls)
			}
		})
	}
}

func TestShouldHardDeleteSyncArtifact(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		path string
		want bool
	}{
		{path: "/apps/localhost-3020/backup.__sync_mutex_v1.__sync_lock_v1", want: true},
		{path: "/apps/localhost-3020/backup.__sync_mutex_v1.__sync_lock_v1/lock.json", want: true},
		{path: "/apps/localhost-3020/backup.__sync_txn_data_v1.123-abc.json", want: true},
		{path: "/apps/localhost-3020/backup.json", want: false},
		{path: "/personal/backup.__sync_txn_data_v1.123-abc.json", want: false},
		{path: "/apps/localhost-3020/notes/lock.json", want: false},
	}

	for _, tc := range testCases {
		if got := shouldHardDeleteSyncArtifact(tc.path); got != tc.want {
			t.Fatalf("shouldHardDeleteSyncArtifact(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

type allowPermissionChecker struct{}

func (allowPermissionChecker) Check(context.Context, *user.User, string, permission.Operation) error {
	return nil
}

type testMutationRecorder struct {
	ensureDirErr    error
	upsertFileErr   error
	movePathErr     error
	copyPathErr     error
	removePathErr   error
	ensureDirCalls  int
	upsertFileCalls int
	removePathCalls int
}

func (r *testMutationRecorder) EnsureDir(context.Context, string) error {
	r.ensureDirCalls++
	return r.ensureDirErr
}

func (r *testMutationRecorder) UpsertFile(context.Context, string) error {
	r.upsertFileCalls++
	return r.upsertFileErr
}

func (r *testMutationRecorder) MovePath(context.Context, string, string, bool) error {
	return r.movePathErr
}

func (r *testMutationRecorder) CopyPath(context.Context, string, string, bool) error {
	return r.copyPathErr
}

func (r *testMutationRecorder) RemovePath(context.Context, string, bool) error {
	r.removePathCalls++
	return r.removePathErr
}

type testRecycleRepo struct {
	createCalls int
}

func (r *testRecycleRepo) Create(context.Context, *recycle.RecycleItem) error {
	r.createCalls++
	return nil
}
func (*testRecycleRepo) GetByHash(context.Context, string) (*recycle.RecycleItem, error) {
	return nil, recycle.ErrRecycleItemNotFound
}
func (*testRecycleRepo) GetByUserID(context.Context, string) ([]*recycle.RecycleItem, error) {
	return nil, nil
}
func (*testRecycleRepo) GetByUserIDPaged(context.Context, string, int, int, string) ([]*recycle.RecycleItem, int, error) {
	return nil, 0, nil
}
func (*testRecycleRepo) DeleteByHash(context.Context, string) error   { return nil }
func (*testRecycleRepo) DeleteByUserID(context.Context, string) error { return nil }
func (*testRecycleRepo) DeleteExpiredItems(context.Context, time.Duration) (int64, error) {
	return 0, nil
}

type testUserRepo struct {
	byID       map[string]*user.User
	byUsername map[string]*user.User
}

func newTestUserRepo() *testUserRepo {
	return &testUserRepo{
		byID:       make(map[string]*user.User),
		byUsername: make(map[string]*user.User),
	}
}

func (r *testUserRepo) FindByUsername(_ context.Context, username string) (*user.User, error) {
	if u, ok := r.byUsername[username]; ok {
		copy := *u
		return &copy, nil
	}
	return nil, user.ErrUserNotFound
}
func (r *testUserRepo) FindByWalletAddress(context.Context, string) (*user.User, error) {
	return nil, user.ErrUserNotFound
}
func (r *testUserRepo) FindByEmail(context.Context, string) (*user.User, error) {
	return nil, user.ErrUserNotFound
}
func (r *testUserRepo) FindByID(_ context.Context, id string) (*user.User, error) {
	if u, ok := r.byID[id]; ok {
		copy := *u
		return &copy, nil
	}
	return nil, user.ErrUserNotFound
}
func (r *testUserRepo) Save(_ context.Context, u *user.User) error {
	copy := *u
	r.byID[copy.ID] = &copy
	r.byUsername[copy.Username] = &copy
	return nil
}
func (r *testUserRepo) Delete(context.Context, string) error { return nil }
func (r *testUserRepo) List(context.Context) ([]*user.User, error) {
	users := make([]*user.User, 0, len(r.byID))
	for _, u := range r.byID {
		copy := *u
		users = append(users, &copy)
	}
	return users, nil
}
func (r *testUserRepo) UpdateUsedSpace(_ context.Context, username string, usedSpace int64) error {
	if u, ok := r.byUsername[username]; ok {
		return u.UpdateUsedSpace(usedSpace)
	}
	return user.ErrUserNotFound
}
func (r *testUserRepo) UpdateUsedSpaceDelta(_ context.Context, username string, delta int64) (int64, error) {
	if u, ok := r.byUsername[username]; ok {
		if err := u.UpdateUsedSpace(u.UsedSpace + delta); err != nil {
			return 0, err
		}
		return u.UsedSpace, nil
	}
	return 0, user.ErrUserNotFound
}
func (r *testUserRepo) UpdateQuota(context.Context, string, int64) error { return nil }
