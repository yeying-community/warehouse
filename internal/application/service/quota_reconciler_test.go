package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/yeying-community/warehouse/internal/domain/quota"
	"github.com/yeying-community/warehouse/internal/domain/recycle"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestQuotaReconcilerRepairsDriftUsingActiveAndRecycleUsage(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	cfg := &config.Config{
		Node: config.NodeConfig{
			Role: "active",
		},
		Quota: config.QuotaConfig{
			AutoReconcileEnabled:  true,
			AutoReconcileInterval: 1,
		},
		WebDAV: config.WebDAVConfig{
			Directory: rootDir,
		},
	}

	userRepo := newTestUserRepo()
	u := user.NewUser("alice", "alice")
	u.Permissions = user.FullPermissions()
	u.Directory = "alice"
	u.UsedSpace = 1
	if err := userRepo.Save(context.Background(), u); err != nil {
		t.Fatalf("save user: %v", err)
	}

	activeFilePath := filepath.Join(rootDir, "alice", "personal", "doc.txt")
	if err := os.MkdirAll(filepath.Dir(activeFilePath), 0o755); err != nil {
		t.Fatalf("mkdir active dir: %v", err)
	}
	if err := os.WriteFile(activeFilePath, []byte("12345"), 0o644); err != nil {
		t.Fatalf("seed active file: %v", err)
	}

	recycleRepo := &memoryRecycleRepo{items: map[string]*recycle.RecycleItem{}}
	item := recycle.NewRecycleItem(u.ID, u.Username, "alice", "old.txt", "/personal/old.txt", false, 7)
	recycleRepo.items[item.Hash] = item

	reconciler := NewQuotaReconciler(
		cfg,
		userRepo,
		recycleRepo,
		quota.NewService(userRepo),
		zap.NewNop(),
	)

	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("reconcile once: %v", err)
	}

	reloaded, err := userRepo.FindByUsername(context.Background(), u.Username)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.UsedSpace != 12 {
		t.Fatalf("expected used space repaired to 12, got %d", reloaded.UsedSpace)
	}
}

func TestQuotaReconcilerDisabledOnStandby(t *testing.T) {
	t.Parallel()

	reconciler := NewQuotaReconciler(
		&config.Config{
			Node: config.NodeConfig{Role: "standby"},
			Quota: config.QuotaConfig{
				AutoReconcileEnabled:  true,
				AutoReconcileInterval: 1,
			},
		},
		newTestUserRepo(),
		&memoryRecycleRepo{items: map[string]*recycle.RecycleItem{}},
		quota.NewService(newTestUserRepo()),
		zap.NewNop(),
	)

	if reconciler.Enabled() {
		t.Fatal("expected quota reconciler to be disabled on standby")
	}
}

func TestQuotaReconcilerLogsNearLimitAndOverQuotaRisk(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	cfg := &config.Config{
		Node: config.NodeConfig{
			Role: "active",
		},
		Quota: config.QuotaConfig{
			AutoReconcileEnabled:  true,
			AutoReconcileInterval: 1,
		},
		WebDAV: config.WebDAVConfig{
			Directory: rootDir,
		},
	}

	userRepo := newTestUserRepo()
	near := user.NewUser("near", "near")
	near.Permissions = user.FullPermissions()
	near.Directory = "near"
	near.Quota = 10
	near.UsedSpace = 8
	if err := userRepo.Save(context.Background(), near); err != nil {
		t.Fatalf("save near user: %v", err)
	}

	over := user.NewUser("over", "over")
	over.Permissions = user.FullPermissions()
	over.Directory = "over"
	over.Quota = 10
	over.UsedSpace = 11
	if err := userRepo.Save(context.Background(), over); err != nil {
		t.Fatalf("save over user: %v", err)
	}

	for _, tc := range []struct {
		dir  string
		name string
		size []byte
	}{
		{dir: "near", name: "near.txt", size: []byte("12345678")},
		{dir: "over", name: "over.txt", size: []byte("12345678901")},
	} {
		fullPath := filepath.Join(rootDir, tc.dir, "personal", tc.name)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", tc.dir, err)
		}
		if err := os.WriteFile(fullPath, tc.size, 0o644); err != nil {
			t.Fatalf("write %s: %v", tc.dir, err)
		}
	}

	core, recorded := observer.New(zap.InfoLevel)
	reconciler := NewQuotaReconciler(
		cfg,
		userRepo,
		&memoryRecycleRepo{items: map[string]*recycle.RecycleItem{}},
		quota.NewService(userRepo),
		zap.New(core),
	)

	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("reconcile once: %v", err)
	}

	if recorded.FilterMessage("quota user is nearing storage limit").Len() != 1 {
		t.Fatalf("expected one near limit log, got %d", recorded.FilterMessage("quota user is nearing storage limit").Len())
	}
	if recorded.FilterMessage("quota user exceeded storage limit").Len() != 1 {
		t.Fatalf("expected one over quota log, got %d", recorded.FilterMessage("quota user exceeded storage limit").Len())
	}
}
