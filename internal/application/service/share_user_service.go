package service

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/addressbook"
	"github.com/yeying-community/warehouse/internal/domain/shareuser"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
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

// Create 创建定向分享（单地址包装）
func (s *ShareUserService) Create(ctx context.Context, owner *user.User, targetWallet string, rawPath string, permissions string, expiry ShareExpiryInput) (*shareuser.ShareUserItem, error) {
	targetWallet = strings.TrimSpace(targetWallet)
	if targetWallet == "" {
		return nil, fmt.Errorf("target wallet is required")
	}
	targetUsers, err := s.resolveTargetUsers(ctx, owner, []string{targetWallet})
	if err != nil {
		return nil, err
	}
	return s.createWithAudiences(ctx, owner, rawPath, permissions, expiry, targetUsers, false, "addresses")
}

// CreateByGroups 按地址簿多分组创建共享（受众会在创建时展开为用户快照）
func (s *ShareUserService) CreateByGroups(ctx context.Context, owner *user.User, groupIDs []string, rawPath string, permissions string, expiry ShareExpiryInput) (*shareuser.ShareUserItem, error) {
	targetUsers, err := s.resolveTargetUsersByGroups(ctx, owner, groupIDs)
	if err != nil {
		return nil, err
	}
	return s.createWithAudiences(ctx, owner, rawPath, permissions, expiry, targetUsers, false, "groups")
}

// CreateByGroup 按单个分组创建共享（内部兼容包装）
func (s *ShareUserService) CreateByGroup(ctx context.Context, owner *user.User, groupID string, rawPath string, permissions string, expiry ShareExpiryInput) (*shareuser.ShareUserItem, error) {
	return s.CreateByGroups(ctx, owner, []string{groupID}, rawPath, permissions, expiry)
}

// CreateByWallets 按地址列表创建共享
func (s *ShareUserService) CreateByWallets(ctx context.Context, owner *user.User, wallets []string, rawPath string, permissions string, expiry ShareExpiryInput) (*shareuser.ShareUserItem, error) {
	targetUsers, err := s.resolveTargetUsers(ctx, owner, wallets)
	if err != nil {
		return nil, err
	}
	return s.createWithAudiences(ctx, owner, rawPath, permissions, expiry, targetUsers, false, "addresses")
}

// CreateForAllUsers 创建全员共享
func (s *ShareUserService) CreateForAllUsers(ctx context.Context, owner *user.User, rawPath string, permissions string, expiry ShareExpiryInput) (*shareuser.ShareUserItem, error) {
	return s.createWithAudiences(ctx, owner, rawPath, permissions, expiry, nil, true, shareuser.AudienceTypeAllUsers)
}

func (s *ShareUserService) createWithAudiences(
	ctx context.Context,
	owner *user.User,
	rawPath string,
	permissions string,
	expiry ShareExpiryInput,
	targetUsers []repository.UserShareAudience,
	allUsers bool,
	targetType string,
) (*shareuser.ShareUserItem, error) {
	cleanPath, err := normalizeSharePath(rawPath, s.webdavPrefix())
	if err != nil {
		return nil, err
	}
	if err := enforceAppScope(ctx, s.config, cleanPath, "create"); err != nil {
		return nil, err
	}

	fullPath := s.resolveFullPath(owner, cleanPath)
	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}

	name := filepath.Base(cleanPath)
	isDir := info.IsDir()
	expiresAt, err := expiry.Resolve(time.Now())
	if err != nil {
		return nil, err
	}

	item := shareuser.NewInternalShareItem(
		owner.ID,
		owner.Username,
		cleanPath,
		name,
		isDir,
		permissions,
		expiresAt,
	)

	audiences := make([]repository.UserShareAudience, 0, len(targetUsers)+1)
	if allUsers {
		audiences = append(audiences, repository.UserShareAudience{
			AudienceType: shareuser.AudienceTypeAllUsers,
		})
	}
	audiences = append(audiences, targetUsers...)
	if len(audiences) == 0 {
		return nil, fmt.Errorf("at least one target audience is required")
	}

	if err := s.repo.CreateWithAudiences(ctx, item, audiences); err != nil {
		return nil, err
	}
	item.AudienceCount = len(audiences)
	item.TargetCount = len(targetUsers)
	item.AllUsers = allUsers
	switch strings.TrimSpace(strings.ToLower(targetType)) {
	case "group", "groups":
		item.AudienceType = "groups"
	case "addresses":
		item.AudienceType = "addresses"
	case shareuser.AudienceTypeAllUsers:
		item.AudienceType = shareuser.AudienceTypeAllUsers
	default:
		if allUsers {
			item.AudienceType = shareuser.AudienceTypeAllUsers
		} else {
			item.AudienceType = "addresses"
		}
	}
	if len(targetUsers) > 0 {
		item.TargetUserID = targetUsers[0].TargetUserID
		item.TargetWalletAddress = targetUsers[0].TargetWallet
	}

	s.logger.Info("share user created",
		zap.String("owner", owner.Username),
		zap.String("path", cleanPath),
		zap.String("share_id", item.ID),
		zap.String("target_type", item.AudienceType),
		zap.Int("audience_count", item.AudienceCount),
		zap.Bool("all_users", item.AllUsers),
	)
	return item, nil
}

func (s *ShareUserService) resolveTargetUsers(ctx context.Context, owner *user.User, wallets []string) ([]repository.UserShareAudience, error) {
	seenWallet := make(map[string]struct{})
	seenUserID := make(map[string]struct{})
	result := make([]repository.UserShareAudience, 0, len(wallets))

	for _, wallet := range wallets {
		wallet = strings.ToLower(strings.TrimSpace(wallet))
		if wallet == "" {
			continue
		}
		if _, ok := seenWallet[wallet]; ok {
			continue
		}
		seenWallet[wallet] = struct{}{}

		target, err := s.userRepo.FindByWalletAddress(ctx, wallet)
		if err != nil {
			return nil, err
		}
		if _, ok := seenUserID[target.ID]; ok {
			continue
		}
		seenUserID[target.ID] = struct{}{}
		result = append(result, repository.UserShareAudience{
			AudienceType: shareuser.AudienceTypeUser,
			TargetUserID: target.ID,
			TargetWallet: target.WalletAddress,
		})
		s.autoTrackAddress(ctx, owner, target)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no valid target users found")
	}
	return result, nil
}

func (s *ShareUserService) resolveTargetUsersByGroups(ctx context.Context, owner *user.User, groupIDs []string) ([]repository.UserShareAudience, error) {
	if s.addressBookService == nil {
		return nil, fmt.Errorf("address book service is not available")
	}
	if len(groupIDs) == 0 {
		return nil, fmt.Errorf("no target groups provided")
	}
	contacts, err := s.addressBookService.ListContacts(ctx, owner)
	if err != nil {
		return nil, err
	}
	selected := make(map[string]struct{}, len(groupIDs))
	hasUngrouped := false
	for _, raw := range groupIDs {
		groupID := strings.TrimSpace(raw)
		if groupID == "" {
			hasUngrouped = true
			continue
		}
		selected[groupID] = struct{}{}
	}
	if !hasUngrouped && len(selected) == 0 {
		return nil, fmt.Errorf("no target groups provided")
	}

	wallets := make([]string, 0)
	groupByWallet := make(map[string]string)
	for _, contact := range contacts {
		groupID := strings.TrimSpace(contact.GroupID)
		if groupID == "" {
			if !hasUngrouped {
				continue
			}
		} else if _, ok := selected[groupID]; !ok {
			continue
		}
		wallets = append(wallets, contact.WalletAddress)
		walletKey := strings.ToLower(strings.TrimSpace(contact.WalletAddress))
		if walletKey != "" {
			groupByWallet[walletKey] = groupID
		}
	}
	if len(wallets) == 0 {
		return nil, fmt.Errorf("no contacts found in target groups")
	}
	targetUsers, err := s.resolveTargetUsers(ctx, owner, wallets)
	if err != nil {
		return nil, err
	}
	for i := range targetUsers {
		walletKey := strings.ToLower(strings.TrimSpace(targetUsers[i].TargetWallet))
		targetUsers[i].SourceGroupID = groupByWallet[walletKey]
	}
	return targetUsers, nil
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

// ListByTarget 获取分享给我的列表（包含全员共享）
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

// ListAudiences 返回共享受众明细（仅 owner 可查看）
func (s *ShareUserService) ListAudiences(ctx context.Context, owner *user.User, shareID string) ([]repository.UserShareAudience, error) {
	item, err := s.repo.GetByID(ctx, shareID)
	if err != nil {
		return nil, err
	}
	if item.OwnerUserID != owner.ID {
		return nil, fmt.Errorf("permission denied: not your share")
	}
	return s.repo.ListAudiencesByShareID(ctx, shareID)
}

// ResolveForTarget 校验分享并返回分享记录与拥有者（目标用户或分享者本人）
func (s *ShareUserService) ResolveForTarget(ctx context.Context, target *user.User, id string, requiredActions ...string) (*shareuser.ShareUserItem, *user.User, error) {
	item, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if item.IsExpired() {
		return nil, nil, shareuser.ErrShareExpired
	}

	if item.OwnerUserID != target.ID {
		audiences, err := s.repo.ListAudiencesByShareID(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		allowed := false
		for _, aud := range audiences {
			switch strings.TrimSpace(strings.ToLower(aud.AudienceType)) {
			case shareuser.AudienceTypeAllUsers:
				allowed = true
			case shareuser.AudienceTypeUser:
				if strings.TrimSpace(aud.TargetUserID) == target.ID {
					allowed = true
				}
			}
			if allowed {
				break
			}
		}
		if !allowed {
			return nil, nil, fmt.Errorf("permission denied: not your share")
		}
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
	if raw == "" || raw == "/" {
		return "", nil
	}
	clean := path.Clean("/" + strings.TrimLeft(raw, "/"))
	if strings.HasPrefix(clean, "/..") {
		return "", fmt.Errorf("invalid relative path")
	}
	clean = strings.TrimPrefix(clean, "/")
	if clean == "." {
		return "", nil
	}
	return clean, nil
}

func isPathWithin(basePath, targetPath string) bool {
	base := filepath.Clean(basePath)
	target := filepath.Clean(targetPath)
	if base == target {
		return true
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, "..")
}
