package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yeying-community/webdav/internal/domain/recycle"
	"github.com/yeying-community/webdav/internal/domain/user"
	"github.com/yeying-community/webdav/internal/infrastructure/config"
	"github.com/yeying-community/webdav/internal/infrastructure/repository"
	"go.uber.org/zap"
)

// RecycleService 回收站服务
type RecycleService struct {
	recycleRepo repository.RecycleRepository
	userRepo    user.Repository
	config      *config.Config
	logger      *zap.Logger
}

// NewRecycleService 创建回收站服务
func NewRecycleService(
	recycleRepo repository.RecycleRepository,
	userRepo user.Repository,
	cfg *config.Config,
	logger *zap.Logger,
) *RecycleService {
	return &RecycleService{
		recycleRepo: recycleRepo,
		userRepo:    userRepo,
		config:      cfg,
		logger:      logger,
	}
}

// RecycleItemResponse 回收站项目响应
type RecycleItemResponse struct {
	Hash      string `json:"hash"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	DeletedAt string `json:"deletedAt"`
	Directory string `json:"directory"`
}

// ListResponse 列表响应
type ListResponse struct {
	Items []*RecycleItemResponse `json:"items"`
}

// AddToRecycle 将文件添加到回收站
func (s *RecycleService) AddToRecycle(
	ctx context.Context,
	u *user.User,
	filePath string, // 相对于用户目录的路径
	directory string, // 目录名
) error {
	// 获取文件信息
	fullPath := filepath.Join(s.getUserRootDir(u), directory, filePath)
	info, err := os.Stat(fullPath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	if info.IsDir() {
		return fmt.Errorf("directory deletion not supported in recycle bin")
	}

	// 提取文件名
	name := filepath.Base(filePath)

	// 创建回收站项目
	item := recycle.NewRecycleItem(u.ID, u.Username, directory, name, filePath, info.Size())

	// 保存到数据库
	if err := s.recycleRepo.Create(ctx, item); err != nil {
		return fmt.Errorf("failed to save to recycle bin: %w", err)
	}

	s.logger.Info("file added to recycle bin",
		zap.String("username", u.Username),
		zap.String("file", filePath),
		zap.String("hash", item.Hash),
	)

	return nil
}

// List 获取用户的回收站列表
func (s *RecycleService) List(ctx context.Context, u *user.User) (*ListResponse, error) {
	items, err := s.recycleRepo.GetByUserID(ctx, u.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list recycle items: %w", err)
	}

	response := &ListResponse{
		Items: make([]*RecycleItemResponse, 0, len(items)),
	}

	for _, item := range items {
		response.Items = append(response.Items, &RecycleItemResponse{
			Hash:      item.Hash,
			Name:      item.Name,
			Path:      item.Path,
			Size:      item.Size,
			DeletedAt: item.DeletedAt.Format("2006-01-02T15:04:05Z07:00"),
			Directory: item.Directory,
		})
	}

	return response, nil
}

// Recover 恢复文件
func (s *RecycleService) Recover(ctx context.Context, u *user.User, hash string) error {
	// 获取回收站项目
	item, err := s.recycleRepo.GetByHash(ctx, hash)
	if err != nil {
		return err
	}

	// 验证所有权
	if item.UserID != u.ID {
		return fmt.Errorf("permission denied: not your file")
	}

	// 检查原路径是否已存在文件
	relPath := strings.TrimPrefix(item.Path, "/")
	relPath = filepath.Clean(filepath.FromSlash(relPath))
	if relPath == "." || strings.HasPrefix(relPath, "..") {
		return fmt.Errorf("invalid original path: %s", item.Path)
	}
	fullPath := filepath.Join(s.getUserRootDir(u), relPath)
	if _, err := os.Stat(fullPath); err == nil {
		return fmt.Errorf("file already exists at original path: %s", item.Path)
	}

	// 确保目标目录存在
	targetDir := filepath.Dir(fullPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	// 从回收站存储目录恢复
	recyclePath, err := s.findRecyclePath(item)
	if err != nil {
		return fmt.Errorf("failed to locate recycle file: %w", err)
	}
	if err := os.Rename(recyclePath, fullPath); err != nil {
		return fmt.Errorf("failed to restore file: %w", err)
	}

	s.logger.Info("recovering file",
		zap.String("username", u.Username),
		zap.String("file", item.Path),
		zap.String("hash", hash),
	)

	// 从数据库中删除记录（标记为已恢复）
	if err := s.recycleRepo.DeleteByHash(ctx, hash); err != nil {
		return fmt.Errorf("failed to remove from recycle bin: %w", err)
	}

	return nil
}

// Remove 永久删除
func (s *RecycleService) Remove(ctx context.Context, u *user.User, hash string) error {
	// 获取回收站项目
	item, err := s.recycleRepo.GetByHash(ctx, hash)
	if err != nil {
		return err
	}

	// 验证所有权
	if item.UserID != u.ID {
		return fmt.Errorf("permission denied: not your file")
	}

	// 删除回收站中的实际文件
	if recyclePath, err := s.findRecyclePath(item); err == nil {
		if err := os.Remove(recyclePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to delete recycle file: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to locate recycle file: %w", err)
	}

	// 从数据库中删除
	if err := s.recycleRepo.DeleteByHash(ctx, hash); err != nil {
		return fmt.Errorf("failed to remove from recycle bin: %w", err)
	}

	s.logger.Info("file permanently deleted from recycle bin",
		zap.String("username", u.Username),
		zap.String("file", item.Path),
		zap.String("hash", hash),
	)

	return nil
}

// getUserRootDir 获取用户的根目录
func (s *RecycleService) getUserRootDir(u *user.User) string {
	userDir := u.Directory
	if userDir == "" {
		userDir = u.Username
	}
	// 如果是绝对路径，直接使用
	if filepath.IsAbs(userDir) {
		return userDir
	}
	return filepath.Join(s.config.WebDAV.Directory, userDir)
}

func (s *RecycleService) getRecycleDir() string {
	return filepath.Join(s.config.WebDAV.Directory, ".recycle")
}

// findRecyclePath 根据记录定位回收站实际文件位置
func (s *RecycleService) findRecyclePath(item *recycle.RecycleItem) (string, error) {
	recycleDir := s.getRecycleDir()

	// 新命名规则：{hash}_{原文件名}
	newPath := filepath.Join(recycleDir, fmt.Sprintf("%s_%s", item.Hash, item.Name))
	if _, err := os.Stat(newPath); err == nil {
		return newPath, nil
	}

	// 旧命名规则：{用户名}_{目录}_{原文件名}_{时间戳}
	legacyPrefix := filepath.Join(recycleDir, fmt.Sprintf("%s_%s_%s_", item.Username, item.Directory, item.Name))
	var matches []string
	_ = filepath.WalkDir(recycleDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasPrefix(path, legacyPrefix) {
			matches = append(matches, path)
		}
		return nil
	})

	if len(matches) == 0 {
		return "", os.ErrNotExist
	}

	// 多个匹配时，选择与删除时间最接近的文件
	best := matches[0]
	bestDelta := time.Duration(math.MaxInt64)
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		delta := info.ModTime().Sub(item.DeletedAt)
		if delta < 0 {
			delta = -delta
		}
		if delta < bestDelta {
			bestDelta = delta
			best = m
		}
	}

	return best, nil
}

// CleanExpired 清理过期文件（可由定时任务调用）
func (s *RecycleService) CleanExpired(ctx context.Context, retentionDays int) (int64, error) {
	retentionPeriod := time.Duration(retentionDays) * 24 * time.Hour
	if retentionDays <= 0 {
		retentionPeriod = 30 * 24 * time.Hour // 默认30天
	}
	deleted, err := s.recycleRepo.DeleteExpiredItems(ctx, retentionPeriod)
	if err != nil {
		return 0, fmt.Errorf("failed to clean expired items: %w", err)
	}

	if deleted > 0 {
		s.logger.Info("cleaned expired recycle items",
			zap.Int64("count", deleted),
			zap.Duration("retention_period", retentionPeriod),
		)
	}

	return deleted, nil
}
