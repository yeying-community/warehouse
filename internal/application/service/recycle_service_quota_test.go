package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/recycle"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"go.uber.org/zap"
)

func TestRecycleRemoveReleasesUsedSpace(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	cfg := &config.Config{
		WebDAV: config.WebDAVConfig{
			Directory: rootDir,
		},
	}

	userRepo := newTestUserRepo()
	u := user.NewUser("alice", "alice")
	u.Permissions = user.FullPermissions()
	u.Quota = 100
	u.UsedSpace = 95
	if err := userRepo.Save(context.Background(), u); err != nil {
		t.Fatalf("save user: %v", err)
	}

	recycleRepo := &memoryRecycleRepo{items: map[string]*recycle.RecycleItem{}}
	item := recycle.NewRecycleItem(u.ID, u.Username, "alice", "old.txt", "/personal/old.txt", false, 10)
	recycleRepo.items[item.Hash] = item

	recycleDir := filepath.Join(rootDir, ".recycle")
	if err := os.MkdirAll(recycleDir, 0o755); err != nil {
		t.Fatalf("mkdir recycle dir: %v", err)
	}
	recyclePath := filepath.Join(recycleDir, item.Hash+"_"+item.Name)
	if err := os.WriteFile(recyclePath, []byte("1234567890"), 0o644); err != nil {
		t.Fatalf("seed recycle file: %v", err)
	}

	svc := NewRecycleService(
		recycleRepo,
		userRepo,
		nil,
		cfg,
		zap.NewNop(),
	)

	if err := svc.Remove(context.Background(), u, item.Hash); err != nil {
		t.Fatalf("remove recycle item: %v", err)
	}

	if u.UsedSpace != 85 {
		t.Fatalf("expected used space to become 85, got %d", u.UsedSpace)
	}
}

type memoryRecycleRepo struct {
	items map[string]*recycle.RecycleItem
}

func (r *memoryRecycleRepo) Create(context.Context, *recycle.RecycleItem) error { return nil }

func (r *memoryRecycleRepo) GetByHash(_ context.Context, hash string) (*recycle.RecycleItem, error) {
	item, ok := r.items[hash]
	if !ok {
		return nil, recycle.ErrRecycleItemNotFound
	}
	copy := *item
	return &copy, nil
}

func (r *memoryRecycleRepo) GetByUserID(_ context.Context, userID string) ([]*recycle.RecycleItem, error) {
	items := make([]*recycle.RecycleItem, 0)
	for _, item := range r.items {
		if item.UserID != userID {
			continue
		}
		copy := *item
		items = append(items, &copy)
	}
	return items, nil
}

func (r *memoryRecycleRepo) GetByUserIDPaged(ctx context.Context, userID string, page, pageSize int, search string) ([]*recycle.RecycleItem, int, error) {
	items, err := r.GetByUserID(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	return items, len(items), nil
}

func (r *memoryRecycleRepo) DeleteByHash(_ context.Context, hash string) error {
	delete(r.items, hash)
	return nil
}

func (r *memoryRecycleRepo) DeleteByUserID(_ context.Context, userID string) error {
	for hash, item := range r.items {
		if item.UserID == userID {
			delete(r.items, hash)
		}
	}
	return nil
}

func (r *memoryRecycleRepo) DeleteExpiredItems(context.Context, time.Duration) (int64, error) {
	return 0, nil
}
