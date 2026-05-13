package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	appservice "github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/quota"
	"github.com/yeying-community/warehouse/internal/domain/recycle"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
)

func TestResolveQuotaUserDirectoryUsesAbsoluteDirectory(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WebDAV.Directory = "/data/root"

	u := user.NewUser("alice", "/mnt/custom/alice")
	if got := appservice.ResolveQuotaUserDirectory(cfg, u); got != "/mnt/custom/alice" {
		t.Fatalf("unexpected user dir: %q", got)
	}
}

func TestResolveQuotaUserDirectoryUsesRelativeDirectory(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WebDAV.Directory = "/data/root"

	u := user.NewUser("alice", "alice")
	if got := appservice.ResolveQuotaUserDirectory(cfg, u); got != filepath.Join("/data/root", "alice") {
		t.Fatalf("unexpected user dir: %q", got)
	}
}

func TestCalculateQuotaUsedSpaceIncludesRecycleItems(t *testing.T) {
	rootDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.WebDAV.Directory = rootDir

	u := user.NewUser("alice", "alice")
	userDir := filepath.Join(rootDir, "alice", "personal")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("mkdir user dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "a.txt"), []byte("12345"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	quotaSvc := quota.NewService(&quotaCLIUserRepo{})
	recycleRepo := &quotaCLIRecycleRepo{
		items: []*recycle.RecycleItem{
			recycle.NewRecycleItem(u.ID, u.Username, "alice", "old.txt", "/personal/old.txt", false, 7),
		},
	}

	snapshot, err := appservice.CalculateQuotaUsage(context.Background(), cfg, quotaSvc, recycleRepo, u)
	if err != nil {
		t.Fatalf("calculate quota used space: %v", err)
	}
	if snapshot.ActiveUsed != 5 {
		t.Fatalf("unexpected active used: %d", snapshot.ActiveUsed)
	}
	if snapshot.RecycleUsed != 7 {
		t.Fatalf("unexpected recycle used: %d", snapshot.RecycleUsed)
	}
	if snapshot.TotalUsed != 12 {
		t.Fatalf("unexpected total used: %d", snapshot.TotalUsed)
	}
}

type quotaCLIUserRepo struct{}

func (*quotaCLIUserRepo) FindByUsername(context.Context, string) (*user.User, error) {
	return nil, user.ErrUserNotFound
}
func (*quotaCLIUserRepo) FindByWalletAddress(context.Context, string) (*user.User, error) {
	return nil, user.ErrUserNotFound
}
func (*quotaCLIUserRepo) FindByEmail(context.Context, string) (*user.User, error) {
	return nil, user.ErrUserNotFound
}
func (*quotaCLIUserRepo) FindByID(context.Context, string) (*user.User, error) {
	return nil, user.ErrUserNotFound
}
func (*quotaCLIUserRepo) Save(context.Context, *user.User) error               { return nil }
func (*quotaCLIUserRepo) Delete(context.Context, string) error                 { return nil }
func (*quotaCLIUserRepo) List(context.Context) ([]*user.User, error)           { return nil, nil }
func (*quotaCLIUserRepo) UpdateUsedSpace(context.Context, string, int64) error { return nil }
func (*quotaCLIUserRepo) UpdateUsedSpaceDelta(context.Context, string, int64) (int64, error) {
	return 0, nil
}
func (*quotaCLIUserRepo) UpdateQuota(context.Context, string, int64) error { return nil }

type quotaCLIRecycleRepo struct {
	items []*recycle.RecycleItem
}

func (*quotaCLIRecycleRepo) Create(context.Context, *recycle.RecycleItem) error { return nil }
func (*quotaCLIRecycleRepo) GetByHash(context.Context, string) (*recycle.RecycleItem, error) {
	return nil, recycle.ErrRecycleItemNotFound
}
func (r *quotaCLIRecycleRepo) GetByUserID(context.Context, string) ([]*recycle.RecycleItem, error) {
	return r.items, nil
}
func (r *quotaCLIRecycleRepo) GetByUserIDPaged(context.Context, string, int, int, string) ([]*recycle.RecycleItem, int, error) {
	return r.items, len(r.items), nil
}
func (*quotaCLIRecycleRepo) DeleteByHash(context.Context, string) error { return nil }
func (*quotaCLIRecycleRepo) DeleteByUserID(context.Context, string) error {
	return nil
}
func (*quotaCLIRecycleRepo) DeleteExpiredItems(context.Context, time.Duration) (int64, error) {
	return 0, nil
}
