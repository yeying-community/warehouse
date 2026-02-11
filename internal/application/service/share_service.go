package service

import (
	"context"
	"fmt"
	"net/url"
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
	cleanPath, err := normalizeSharePath(rawPath, s.webdavPrefix())
	if err != nil {
		return nil, err
	}
	if err := enforceAppScope(ctx, s.config, cleanPath, "create"); err != nil {
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
	items, err := s.shareRepo.GetByUserID(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	scope, err := resolveAppScope(ctx, s.config)
	if err != nil {
		return nil, err
	}
	if !scope.active {
		for _, item := range items {
			normalized, err := s.normalizeItemPath(item.Path)
			if err != nil {
				s.logger.Warn("invalid share path",
					zap.String("username", u.Username),
					zap.String("path", item.Path),
					zap.Error(err))
				continue
			}
			item.Path = normalized
		}
		return items, nil
	}
	filtered := make([]*share.ShareItem, 0, len(items))
	for _, item := range items {
		normalized, err := s.normalizeItemPath(item.Path)
		if err != nil {
			s.logger.Warn("invalid share path",
				zap.String("username", u.Username),
				zap.String("path", item.Path),
				zap.Error(err))
			continue
		}
		item.Path = normalized
		if scope.allowsAny(normalized, "read") {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
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
	normalized, err := s.normalizeItemPath(item.Path)
	if err != nil {
		return err
	}
	item.Path = normalized
	if err := enforceAppScope(ctx, s.config, normalized, "delete"); err != nil {
		return err
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

	normalized, err := s.normalizeItemPath(item.Path)
	if err != nil {
		return nil, nil, nil, share.ErrInvalidShare
	}
	item.Path = normalized
	fullPath := s.resolveFullPath(u, normalized)
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

func (s *ShareService) webdavPrefix() string {
	if s == nil || s.config == nil {
		return ""
	}
	return s.config.WebDAV.Prefix
}

func (s *ShareService) normalizeItemPath(raw string) (string, error) {
	return normalizeSharePath(raw, s.webdavPrefix())
}

func normalizeSharePath(raw string, prefix string) (string, error) {
	raw = stripWebdavPrefix(raw, prefix)
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

func stripWebdavPrefix(rawPath string, prefix string) string {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "/"
	}
	if strings.HasPrefix(rawPath, "http://") || strings.HasPrefix(rawPath, "https://") {
		if u, err := url.Parse(rawPath); err == nil && u.Path != "" {
			rawPath = u.Path
		}
	}
	if !strings.HasPrefix(rawPath, "/") {
		rawPath = "/" + rawPath
	}

	prefix = strings.TrimSpace(prefix)
	if prefix == "" || prefix == "/" {
		return rawPath
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimSuffix(prefix, "/")
	if rawPath == prefix {
		return "/"
	}
	if strings.HasPrefix(rawPath, prefix+"/") {
		trimmed := strings.TrimPrefix(rawPath, prefix)
		if trimmed == "" {
			return "/"
		}
		return trimmed
	}
	return rawPath
}
