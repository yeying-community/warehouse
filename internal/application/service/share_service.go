package service

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/yeying-community/webdav/internal/domain/share"
	"github.com/yeying-community/webdav/internal/domain/user"
	"github.com/yeying-community/webdav/internal/infrastructure/config"
	"github.com/yeying-community/webdav/internal/infrastructure/repository"
	"go.uber.org/zap"
)

// ShareService 文件分享服务
type ShareService struct {
	shareRepo repository.ShareRepository
	userRepo  user.Repository
	config    *config.Config
	logger    *zap.Logger
}

// NewShareService 创建分享服务
func NewShareService(
	shareRepo repository.ShareRepository,
	userRepo user.Repository,
	cfg *config.Config,
	logger *zap.Logger,
) *ShareService {
	return &ShareService{
		shareRepo: shareRepo,
		userRepo:  userRepo,
		config:    cfg,
		logger:    logger,
	}
}

// Create 创建分享链接
func (s *ShareService) Create(ctx context.Context, u *user.User, rawPath string, expiresIn int64) (*share.ShareItem, error) {
	cleanPath, err := normalizeSharePath(rawPath)
	if err != nil {
		return nil, err
	}

	fullPath := s.resolveFullPath(u, cleanPath)
	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("directory sharing not supported")
	}

	name := filepath.Base(fullPath)
	var expiresAt *time.Time
	if expiresIn > 0 {
		t := time.Now().Add(time.Duration(expiresIn) * time.Second)
		expiresAt = &t
	}

	item := share.NewShareItem(u.ID, u.Username, cleanPath, name, expiresAt)
	if err := s.shareRepo.Create(ctx, item); err != nil {
		return nil, err
	}

	s.logger.Info("share created",
		zap.String("username", u.Username),
		zap.String("path", cleanPath),
		zap.String("token", item.Token),
	)

	return item, nil
}

// List 获取用户分享列表
func (s *ShareService) List(ctx context.Context, u *user.User) ([]*share.ShareItem, error) {
	return s.shareRepo.GetByUserID(ctx, u.ID)
}

// Revoke 取消分享
func (s *ShareService) Revoke(ctx context.Context, u *user.User, token string) error {
	item, err := s.shareRepo.GetByToken(ctx, token)
	if err != nil {
		return err
	}
	if item.UserID != u.ID {
		return fmt.Errorf("permission denied: not your share")
	}
	return s.shareRepo.DeleteByToken(ctx, token)
}

// IncrementView 记录访问次数
func (s *ShareService) IncrementView(ctx context.Context, token string) error {
	return s.shareRepo.IncrementView(ctx, token)
}

// IncrementDownload 记录下载次数
func (s *ShareService) IncrementDownload(ctx context.Context, token string) error {
	return s.shareRepo.IncrementDownload(ctx, token)
}

// Resolve 根据 token 获取分享文件
func (s *ShareService) Resolve(ctx context.Context, token string) (*share.ShareItem, *os.File, os.FileInfo, error) {
	item, err := s.shareRepo.GetByToken(ctx, token)
	if err != nil {
		return nil, nil, nil, err
	}
	if item.IsExpired() {
		return nil, nil, nil, share.ErrShareExpired
	}

	u, err := s.userRepo.FindByID(ctx, item.UserID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get user: %w", err)
	}

	fullPath := s.resolveFullPath(u, item.Path)
	f, err := os.Open(fullPath)
	if err != nil {
		return nil, nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, nil, err
	}
	if info.IsDir() {
		f.Close()
		return nil, nil, nil, share.ErrInvalidShare
	}
	return item, f, info, nil
}

func (s *ShareService) resolveFullPath(u *user.User, sharePath string) string {
	rel := strings.TrimPrefix(sharePath, "/")
	rel = filepath.FromSlash(rel)
	return filepath.Join(s.getUserRootDir(u), rel)
}

func (s *ShareService) getUserRootDir(u *user.User) string {
	userDir := u.Directory
	if userDir == "" {
		userDir = u.Username
	}
	if filepath.IsAbs(userDir) {
		return userDir
	}
	return filepath.Join(s.config.WebDAV.Directory, userDir)
}

func normalizeSharePath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("path is required")
	}
	clean := path.Clean("/" + strings.TrimLeft(raw, "/"))
	if clean == "/" || strings.HasPrefix(clean, "/..") {
		return "", fmt.Errorf("invalid path")
	}
	clean = strings.TrimSuffix(clean, "/")
	return clean, nil
}
