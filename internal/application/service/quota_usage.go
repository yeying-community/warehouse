package service

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/yeying-community/warehouse/internal/domain/quota"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
)

// QuotaUsageSnapshot is one quota recalculation result.
type QuotaUsageSnapshot struct {
	ActiveUsed  int64
	RecycleUsed int64
	TotalUsed   int64
	UserDir     string
}

// CalculateQuotaUsage recalculates one user's used space using the current quota accounting semantics.
func CalculateQuotaUsage(
	ctx context.Context,
	cfg *config.Config,
	quotaSvc quota.Service,
	recycleRepo repository.RecycleRepository,
	u *user.User,
) (*QuotaUsageSnapshot, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if quotaSvc == nil {
		return nil, fmt.Errorf("quota service is nil")
	}
	if recycleRepo == nil {
		return nil, fmt.Errorf("recycle repository is nil")
	}
	if u == nil {
		return nil, fmt.Errorf("user is nil")
	}

	userDir := ResolveQuotaUserDirectory(cfg, u)
	activeUsed, err := quotaSvc.CalculateUsedSpace(ctx, userDir)
	if err != nil {
		return nil, fmt.Errorf("calculate active used space: %w", err)
	}

	items, err := recycleRepo.GetByUserID(ctx, u.ID)
	if err != nil {
		return nil, fmt.Errorf("load recycle items: %w", err)
	}

	var recycleUsed int64
	for _, item := range items {
		if item == nil || item.Size <= 0 {
			continue
		}
		recycleUsed += item.Size
	}

	return &QuotaUsageSnapshot{
		ActiveUsed:  activeUsed,
		RecycleUsed: recycleUsed,
		TotalUsed:   activeUsed + recycleUsed,
		UserDir:     userDir,
	}, nil
}

// ResolveQuotaUserDirectory returns the user root directory used by quota scanning.
func ResolveQuotaUserDirectory(cfg *config.Config, u *user.User) string {
	if u.Directory != "" {
		if filepath.IsAbs(u.Directory) {
			return u.Directory
		}
		return filepath.Join(cfg.WebDAV.Directory, u.Directory)
	}
	return cfg.WebDAV.Directory
}
