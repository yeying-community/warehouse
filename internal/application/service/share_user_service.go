package service

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/yeying-community/webdav/internal/domain/addressbook"
	"github.com/yeying-community/webdav/internal/domain/shareuser"
	"github.com/yeying-community/webdav/internal/domain/user"
	"github.com/yeying-community/webdav/internal/infrastructure/config"
	"github.com/yeying-community/webdav/internal/infrastructure/repository"
	"go.uber.org/zap"
)

// ShareUserService 定向分享服务
type ShareUserService struct {
	repo               repository.UserShareRepository
	userRepo           user.Repository
	addressBookService *AddressBookService
	config             *config.Config
	logger             *zap.Logger
}

// NewShareUserService 创建定向分享服务
func NewShareUserService(
	repo repository.UserShareRepository,
	userRepo user.Repository,
	addressBookService *AddressBookService,
	cfg *config.Config,
	logger *zap.Logger,
) *ShareUserService {
	return &ShareUserService{
		repo:               repo,
		userRepo:           userRepo,
		addressBookService: addressBookService,
		config:             cfg,
		logger:             logger,
	}
}

// Create 创建定向分享
func (s *ShareUserService) Create(ctx context.Context, owner *user.User, targetWallet string, rawPath string, permissions string, expiresIn int64) (*shareuser.ShareUserItem, error) {
	cleanPath, err := normalizeSharePath(rawPath, s.webdavPrefix())
	if err != nil {
		return nil, err
	}
	if err := enforceAppScope(ctx, s.config, cleanPath, "create"); err != nil {
		return nil, err
	}

	target, err := s.userRepo.FindByWalletAddress(ctx, targetWallet)
	if err != nil {
		return nil, err
	}

	fullPath := s.resolveFullPath(owner, cleanPath)
	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}

	name := filepath.Base(cleanPath)
	isDir := info.IsDir()

	var expiresAt *time.Time
	if expiresIn > 0 {
		t := time.Now().Add(time.Duration(expiresIn) * time.Second)
		expiresAt = &t
	}

	item := shareuser.NewShareUserItem(
		owner.ID,
		owner.Username,
		target.ID,
		target.WalletAddress,
		cleanPath,
		name,
		isDir,
		permissions,
		expiresAt,
	)
	if err := s.repo.Create(ctx, item); err != nil {
		return nil, err
	}

	s.autoTrackAddress(ctx, owner, target)

	s.logger.Info("share user created",
		zap.String("owner", owner.Username),
		zap.String("target", target.WalletAddress),
		zap.String("path", cleanPath),
		zap.String("share_id", item.ID),
	)

	return item, nil
}

func (s *ShareUserService) autoTrackAddress(ctx context.Context, owner *user.User, target *user.User) {
	if s.addressBookService == nil || owner == nil || target == nil {
		return
	}
	name := strings.TrimSpace(target.Username)
	if name == "" {
		name = shortenWallet(target.WalletAddress)
	}
	if name == "" {
		name = "联系人"
	}
	if _, err := s.addressBookService.CreateContact(ctx, owner, name, target.WalletAddress, "", nil); err != nil {
		if err == addressbook.ErrDuplicateWallet {
			return
		}
		s.logger.Warn("failed to auto track address",
			zap.String("owner", owner.Username),
			zap.String("target", target.WalletAddress),
			zap.Error(err),
		)
	}
}

func (s *ShareUserService) webdavPrefix() string {
	if s == nil || s.config == nil {
		return ""
	}
	return s.config.WebDAV.Prefix
}

func (s *ShareUserService) normalizeItemPath(raw string) (string, error) {
	return normalizeSharePath(raw, s.webdavPrefix())
}

func shortenWallet(address string) string {
	trimmed := strings.TrimSpace(address)
	if len(trimmed) <= 10 {
		return trimmed
	}
	return trimmed[:6] + "..." + trimmed[len(trimmed)-4:]
}

// ListByOwner 获取我分享的列表
func (s *ShareUserService) ListByOwner(ctx context.Context, owner *user.User) ([]*shareuser.ShareUserItem, error) {
	items, err := s.repo.GetByOwnerID(ctx, owner.ID)
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
				s.logger.Warn("invalid share user path",
					zap.String("owner", owner.Username),
					zap.String("path", item.Path),
					zap.Error(err))
				continue
			}
			item.Path = normalized
		}
		return items, nil
	}
	filtered := make([]*shareuser.ShareUserItem, 0, len(items))
	for _, item := range items {
		normalized, err := s.normalizeItemPath(item.Path)
		if err != nil {
			s.logger.Warn("invalid share user path",
				zap.String("owner", owner.Username),
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

// ListByTarget 获取分享给我的列表
func (s *ShareUserService) ListByTarget(ctx context.Context, target *user.User) ([]*shareuser.ShareUserItem, error) {
	items, err := s.repo.GetByTargetID(ctx, target.ID)
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
				s.logger.Warn("invalid share user path",
					zap.String("target", target.Username),
					zap.String("path", item.Path),
					zap.Error(err))
				continue
			}
			item.Path = normalized
		}
		return items, nil
	}
	filtered := make([]*shareuser.ShareUserItem, 0, len(items))
	for _, item := range items {
		normalized, err := s.normalizeItemPath(item.Path)
		if err != nil {
			s.logger.Warn("invalid share user path",
				zap.String("target", target.Username),
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
func (s *ShareUserService) Revoke(ctx context.Context, owner *user.User, id string) error {
	item, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if item.OwnerUserID != owner.ID {
		return fmt.Errorf("permission denied: not your share")
	}
	if err := enforceAppScope(ctx, s.config, item.Path, "delete"); err != nil {
		return err
	}
	return s.repo.DeleteByID(ctx, id)
}

// ResolveForTarget 校验分享并返回分享记录与拥有者
func (s *ShareUserService) ResolveForTarget(ctx context.Context, target *user.User, id string, requiredActions ...string) (*shareuser.ShareUserItem, *user.User, error) {
	item, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if item.IsExpired() {
		return nil, nil, shareuser.ErrShareExpired
	}
	if item.TargetUserID != target.ID {
		return nil, nil, fmt.Errorf("permission denied: not your share")
	}
	normalized, err := s.normalizeItemPath(item.Path)
	if err != nil {
		return nil, nil, err
	}
	item.Path = normalized
	if err := enforceAppScope(ctx, s.config, normalized, requiredActions...); err != nil {
		return nil, nil, err
	}
	owner, err := s.userRepo.FindByID(ctx, item.OwnerUserID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get owner: %w", err)
	}
	return item, owner, nil
}

// ResolveSharePath 解析分享路径并确保在分享范围内
func (s *ShareUserService) ResolveSharePath(owner *user.User, item *shareuser.ShareUserItem, relative string) (string, string, error) {
	normalized, err := s.normalizeItemPath(item.Path)
	if err != nil {
		return "", "", err
	}
	item.Path = normalized
	baseRel := strings.TrimPrefix(normalized, "/")
	baseRel = path.Clean("/" + baseRel)
	if baseRel == "/" || strings.HasPrefix(baseRel, "/..") {
		return "", "", fmt.Errorf("invalid share path")
	}
	baseRel = strings.TrimPrefix(baseRel, "/")

	baseFull := filepath.Clean(filepath.Join(s.getUserRootDir(owner), filepath.FromSlash(baseRel)))

	relClean, err := cleanRelativePath(relative)
	if err != nil {
		return "", "", err
	}

	var targetFull string
	if item.IsDir {
		if relClean != "" {
			targetFull = filepath.Clean(filepath.Join(baseFull, filepath.FromSlash(relClean)))
		} else {
			targetFull = baseFull
		}
	} else {
		if relClean != "" && relClean != path.Base(baseRel) {
			return "", "", fmt.Errorf("invalid path for file share")
		}
		targetFull = baseFull
	}

	if !isPathWithin(baseFull, targetFull) {
		return "", "", fmt.Errorf("invalid share path")
	}

	return baseFull, targetFull, nil
}

func (s *ShareUserService) resolveFullPath(u *user.User, sharePath string) string {
	rel := strings.TrimPrefix(sharePath, "/")
	rel = filepath.FromSlash(rel)
	return filepath.Join(s.getUserRootDir(u), rel)
}

func (s *ShareUserService) getUserRootDir(u *user.User) string {
	userDir := u.Directory
	if userDir == "" {
		userDir = u.Username
	}
	if filepath.IsAbs(userDir) {
		return userDir
	}
	return filepath.Join(s.config.WebDAV.Directory, userDir)
}

func cleanRelativePath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	clean := path.Clean("/" + strings.TrimLeft(raw, "/"))
	if clean == "/" {
		return "", nil
	}
	if strings.HasPrefix(clean, "/..") {
		return "", fmt.Errorf("invalid path")
	}
	return strings.TrimPrefix(clean, "/"), nil
}

func isPathWithin(base, target string) bool {
	baseClean := filepath.Clean(base)
	targetClean := filepath.Clean(target)
	if baseClean == targetClean {
		return true
	}
	return strings.HasPrefix(targetClean, baseClean+string(os.PathSeparator))
}
