package service

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/yeying-community/warehouse/internal/domain/permission"
	"github.com/yeying-community/warehouse/internal/domain/quota"
	"github.com/yeying-community/warehouse/internal/domain/shareuser"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

func TestSyncUserSharePathsForOwnerMoveUpdatesStoragePaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.WebDAV.Directory = root

	owner := user.NewUser("alice", "alice")
	repo := &captureUserShareRepo{}

	fromPath := filepath.Join(root, owner.Username, "personal", "test")
	toPath := filepath.Join(root, owner.Username, "personal", "test_upload")
	if err := SyncUserSharePathsForOwnerMove(context.Background(), repo, cfg, owner, fromPath, toPath); err != nil {
		t.Fatalf("SyncUserSharePathsForOwnerMove: %v", err)
	}

	if repo.ownerID != owner.ID || repo.fromPath != "/personal/test" || repo.toPath != "/personal/test_upload" {
		t.Fatalf("unexpected sync args: owner=%q from=%q to=%q", repo.ownerID, repo.fromPath, repo.toPath)
	}
}

func TestWebDAVServeHTTPMoveSyncsSharePaths(t *testing.T) {
	t.Parallel()

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
	if err := userRepo.Save(context.Background(), u); err != nil {
		t.Fatalf("save user: %v", err)
	}

	shareRepo := &captureUserShareRepo{}
	svc := NewWebDAVService(
		cfg,
		allowPermissionChecker{},
		quota.NewService(userRepo),
		userRepo,
		&testRecycleRepo{},
		shareRepo,
		nil,
		zap.NewNop(),
	)

	userDir := svc.getUserDirectory(u)
	if err := os.MkdirAll(filepath.Join(userDir, "personal", "test"), 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}

	req := httptest.NewRequest("MOVE", "/dav/personal/test", nil)
	req.Header.Set("Destination", "/dav/personal/test_upload")
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, u))
	resp := httptest.NewRecorder()

	svc.ServeHTTP(resp, req)

	if resp.Code < 200 || resp.Code >= 300 {
		t.Fatalf("expected MOVE to succeed, got status=%d body=%q", resp.Code, resp.Body.String())
	}
	if shareRepo.fromPath != "/personal/test" || shareRepo.toPath != "/personal/test_upload" {
		t.Fatalf("unexpected share sync args: from=%q to=%q", shareRepo.fromPath, shareRepo.toPath)
	}
}

type captureUserShareRepo struct {
	ownerID  string
	fromPath string
	toPath   string
}

func (*captureUserShareRepo) CreateWithAudiences(context.Context, *shareuser.ShareUserItem, []repository.UserShareAudience) error {
	return nil
}

func (*captureUserShareRepo) GetByID(context.Context, string) (*shareuser.ShareUserItem, error) {
	return nil, shareuser.ErrShareNotFound
}

func (*captureUserShareRepo) GetByOwnerID(context.Context, string) ([]*shareuser.ShareUserItem, error) {
	return nil, nil
}

func (*captureUserShareRepo) GetByTargetID(context.Context, string) ([]*shareuser.ShareUserItem, error) {
	return nil, nil
}

func (r *captureUserShareRepo) UpdatePathsForOwnerMove(_ context.Context, ownerID, fromPath, toPath string) error {
	r.ownerID = ownerID
	r.fromPath = fromPath
	r.toPath = toPath
	return nil
}

func (*captureUserShareRepo) DeleteByID(context.Context, string) error {
	return nil
}

func (*captureUserShareRepo) ListAudiencesByShareID(context.Context, string) ([]repository.UserShareAudience, error) {
	return nil, nil
}

var _ permission.Checker = allowPermissionChecker{}
var _ repository.UserShareRepository = (*captureUserShareRepo)(nil)
