package quota

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/yeying-community/warehouse/internal/domain/user"
)

// QuotaInfo 配额信息
type QuotaInfo struct {
	UserID    string `json:"user_id"`
	Quota     int64  `json:"quota"`     // 配额大小（字节），0 表示无限制
	Used      int64  `json:"used"`      // 已使用（字节）
	Available int64  `json:"available"` // 可用空间（字节）
}

// Service 配额服务
type Service interface {
	// GetQuota 获取用户配额信息
	GetQuota(ctx context.Context, userID string) (*QuotaInfo, error)

	// CalculateUsedSpace 计算用户已使用空间
	CalculateUsedSpace(ctx context.Context, userDirectory string) (int64, error)

	// CheckQuota 检查用户配额
	CheckQuota(ctx context.Context, u *user.User, additionalSize int64) error

	// UpdateUserSpace 更新用户空间使用情况
	UpdateUserSpace(ctx context.Context, u *user.User, userRepository user.Repository) error
}

type service struct {
	userRepo user.Repository
}

// NewService 创建配额服务
func NewService(userRepo user.Repository) Service {
	return &service{
		userRepo: userRepo,
	}
}

// GetQuota 获取用户配额信息
func (s *service) GetQuota(ctx context.Context, userID string) (*QuotaInfo, error) {
	// 获取用户信息
	u, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// 构建配额信息
	info := &QuotaInfo{
		UserID: userID,
		Quota:  u.Quota,
		Used:   u.UsedSpace,
	}

	// 计算可用空间
	if u.Quota > 0 {
		info.Available = u.Quota - u.UsedSpace
		if info.Available < 0 {
			info.Available = 0
		}
	} else {
		// 无限制
		info.Available = -1
	}

	return info, nil
}

// CalculateUsedSpace 计算用户已使用空间
func (s *service) CalculateUsedSpace(ctx context.Context, userDirectory string) (int64, error) {
	var totalSize int64

	err := filepath.WalkDir(userDirectory, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// 忽略无法访问的文件/目录
			return nil
		}

		// 检查上下文是否被取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if !d.IsDir() {
			info, err := d.Info()
			if err != nil {
				// 忽略无法获取信息的文件
				return nil
			}
			totalSize += info.Size()
		}

		return nil
	})

	if err != nil {
		return 0, fmt.Errorf("failed to calculate used space: %w", err)
	}

	return totalSize, nil
}

// CheckQuota 检查用户配额
func (s *service) CheckQuota(ctx context.Context, u *user.User, additionalSize int64) error {
	if u == nil {
		return fmt.Errorf("user is nil")
	}

	// 如果用户没有配额限制，直接返回
	if !u.HasQuota() {
		return nil
	}

	// 检查是否会超过配额
	return u.CanUpload(additionalSize)
}

// UpdateUserSpace 更新用户空间使用情况
func (s *service) UpdateUserSpace(ctx context.Context, u *user.User, userRepository user.Repository) error {
	if u == nil {
		return fmt.Errorf("user is nil")
	}

	// 计算已使用空间
	usedSpace, err := s.CalculateUsedSpace(ctx, u.Directory)
	if err != nil {
		return fmt.Errorf("failed to calculate used space: %w", err)
	}

	// 更新用户已使用空间
	if err := userRepository.UpdateUsedSpace(ctx, u.Username, usedSpace); err != nil {
		return fmt.Errorf("failed to update used space: %w", err)
	}

	// 更新内存中的用户对象
	u.UpdateUsedSpace(usedSpace)

	return nil
}

// GetFileSize 获取文件大小
func GetFileSize(filePath string) (int64, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
