package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/group"
	"github.com/yeying-community/warehouse/internal/domain/shareuser"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
	"go.uber.org/zap"
)

// ShareUserService 定向分享服务
type ShareUserService struct {
	repo         repository.UserShareRepository
	userRepo     user.Repository
	groupService *GroupService
	notification *NotificationService
	config       *config.Config
	logger       *zap.Logger
}

func (s *ShareUserService) Repository() repository.UserShareRepository {
	if s == nil {
		return nil
	}
	return s.repo
}

func (s *ShareUserService) Config() *config.Config {
	if s == nil {
		return nil
	}
	return s.config
}

// NewShareUserService 创建定向分享服务
func NewShareUserService(
	repo repository.UserShareRepository,
	userRepo user.Repository,
	groupService *GroupService,
	notificationService *NotificationService,
	cfg *config.Config,
	logger *zap.Logger,
) *ShareUserService {
	return &ShareUserService{
		repo:         repo,
		userRepo:     userRepo,
		groupService: groupService,
		notification: notificationService,
		config:       cfg,
		logger:       logger,
	}
}

// CreateByGroups 按共享分组创建共享（访问时按当前 active 分组成员动态授权）
func (s *ShareUserService) CreateByGroups(ctx context.Context, owner *user.User, groupIDs []string, rawPath string, permissions string, expiry ShareExpiryInput) (*shareuser.ShareUserItem, error) {
	targetGroups, notificationTargets, err := s.resolveTargetGroups(ctx, owner, groupIDs)
	if err != nil {
		return nil, err
	}
	return s.createWithAudiences(ctx, owner, rawPath, permissions, expiry, targetGroups, notificationTargets, false, "groups")
}

// CreateByWallets 按地址列表创建共享
func (s *ShareUserService) CreateByWallets(ctx context.Context, owner *user.User, wallets []string, rawPath string, permissions string, expiry ShareExpiryInput) (*shareuser.ShareUserItem, error) {
	targetUsers, err := s.resolveTargetUsers(ctx, wallets)
	if err != nil {
		return nil, err
	}
	return s.createWithAudiences(ctx, owner, rawPath, permissions, expiry, targetUsers, targetUsers, false, "addresses")
}

// CreateForAllUsers 创建全员共享
func (s *ShareUserService) CreateForAllUsers(ctx context.Context, owner *user.User, rawPath string, permissions string, expiry ShareExpiryInput) (*shareuser.ShareUserItem, error) {
	return s.createWithAudiences(ctx, owner, rawPath, permissions, expiry, nil, nil, true, shareuser.AudienceTypeAllUsers)
}

func (s *ShareUserService) createWithAudiences(
	ctx context.Context,
	owner *user.User,
	rawPath string,
	permissions string,
	expiry ShareExpiryInput,
	targetAudiences []repository.UserShareAudience,
	notificationTargets []repository.UserShareAudience,
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

	audiences := make([]repository.UserShareAudience, 0, len(targetAudiences)+1)
	if allUsers {
		audiences = append(audiences, repository.UserShareAudience{
			AudienceType: shareuser.AudienceTypeAllUsers,
		})
	}
	audiences = append(audiences, targetAudiences...)
	if len(audiences) == 0 {
		return nil, fmt.Errorf("at least one target audience is required")
	}

	if err := s.repo.CreateWithAudiences(ctx, item, audiences); err != nil {
		return nil, err
	}
	item.AudienceCount = len(audiences)
	item.TargetCount = len(targetAudiences)
	item.AllUsers = allUsers
	switch strings.TrimSpace(strings.ToLower(targetType)) {
	case "groups":
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
	if len(targetAudiences) > 0 && strings.TrimSpace(targetAudiences[0].TargetUserID) != "" {
		item.TargetUserID = targetAudiences[0].TargetUserID
		item.TargetWalletAddress = targetAudiences[0].TargetWallet
	}

	if s.notification != nil {
		s.notification.NotifyShareCreated(ctx, owner, item.ID, item.Name, item.Path, notificationTargets, allUsers)
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

func (s *ShareUserService) resolveTargetUsers(ctx context.Context, wallets []string) ([]repository.UserShareAudience, error) {
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
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no valid target users found")
	}
	return result, nil
}

func (s *ShareUserService) resolveTargetGroups(ctx context.Context, owner *user.User, groupIDs []string) ([]repository.UserShareAudience, []repository.UserShareAudience, error) {
	if s.groupService == nil {
		return nil, nil, fmt.Errorf("group management service is not available")
	}
	if len(groupIDs) == 0 {
		return nil, nil, fmt.Errorf("no target groups provided")
	}
	members, err := s.groupService.ListMembers(ctx, owner)
	if err != nil {
		return nil, nil, err
	}
	selectedOrder := make([]string, 0, len(groupIDs))
	selected := make(map[string]struct{}, len(groupIDs))
	for _, raw := range groupIDs {
		groupID := strings.TrimSpace(raw)
		if groupID == "" {
			continue
		}
		if _, ok := selected[groupID]; ok {
			continue
		}
		selected[groupID] = struct{}{}
		selectedOrder = append(selectedOrder, groupID)
	}
	if len(selected) == 0 {
		return nil, nil, fmt.Errorf("no target groups provided")
	}

	wallets := make([]string, 0)
	groupByWallet := make(map[string]string)
	visibleGroups := make(map[string]struct{}, len(selected))
	activeGroups := make(map[string]struct{}, len(selected))
	for _, member := range members {
		groupID := strings.TrimSpace(member.GroupID)
		if groupID == "" {
			continue
		}
		if _, ok := selected[groupID]; !ok {
			continue
		}
		visibleGroups[groupID] = struct{}{}
		if group.NormalizeMemberStatus(member.Status) != group.MemberStatusActive {
			continue
		}
		activeGroups[groupID] = struct{}{}
		wallets = append(wallets, member.WalletAddress)
		walletKey := strings.ToLower(strings.TrimSpace(member.WalletAddress))
		if walletKey != "" {
			groupByWallet[walletKey] = groupID
		}
	}

	groupAudiences := make([]repository.UserShareAudience, 0, len(selectedOrder))
	for _, groupID := range selectedOrder {
		if _, ok := visibleGroups[groupID]; !ok {
			return nil, nil, fmt.Errorf("target group %s is not visible", groupID)
		}
		if _, ok := activeGroups[groupID]; !ok {
			return nil, nil, fmt.Errorf("no active members found in target group %s", groupID)
		}
		groupAudiences = append(groupAudiences, repository.UserShareAudience{
			AudienceType:  shareuser.AudienceTypeGroup,
			SourceGroupID: groupID,
		})
	}
	if len(groupAudiences) == 0 {
		return nil, nil, fmt.Errorf("no target groups provided")
	}

	return groupAudiences, s.resolveExistingTargetUsers(ctx, wallets, groupByWallet), nil
}

func (s *ShareUserService) resolveExistingTargetUsers(ctx context.Context, wallets []string, groupByWallet map[string]string) []repository.UserShareAudience {
	if s.userRepo == nil {
		return nil
	}
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
			if !errors.Is(err, user.ErrUserNotFound) && s.logger != nil {
				s.logger.Warn("failed to resolve group share notification target",
					zap.String("wallet", wallet),
					zap.Error(err))
			}
			continue
		}
		if _, ok := seenUserID[target.ID]; ok {
			continue
		}
		seenUserID[target.ID] = struct{}{}
		result = append(result, repository.UserShareAudience{
			AudienceType:  shareuser.AudienceTypeUser,
			TargetUserID:  target.ID,
			TargetWallet:  target.WalletAddress,
			SourceGroupID: groupByWallet[wallet],
		})
	}
	return result
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
		allowed, err := s.targetHasAudienceAccess(ctx, target, audiences)
		if err != nil {
			return nil, nil, err
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

func (s *ShareUserService) targetHasAudienceAccess(ctx context.Context, target *user.User, audiences []repository.UserShareAudience) (bool, error) {
	if target == nil {
		return false, nil
	}
	groupIDs := make(map[string]struct{})
	for _, aud := range audiences {
		sourceGroupID := strings.TrimSpace(aud.SourceGroupID)
		if sourceGroupID != "" {
			groupIDs[sourceGroupID] = struct{}{}
		}
		switch strings.TrimSpace(strings.ToLower(aud.AudienceType)) {
		case shareuser.AudienceTypeAllUsers:
			return true, nil
		case shareuser.AudienceTypeUser:
			if sourceGroupID == "" && strings.TrimSpace(aud.TargetUserID) == target.ID {
				return true, nil
			}
		}
	}
	if len(groupIDs) == 0 {
		return false, nil
	}
	return s.targetHasActiveGroupMembership(ctx, target, groupIDs)
}

func (s *ShareUserService) targetHasActiveGroupMembership(ctx context.Context, target *user.User, groupIDs map[string]struct{}) (bool, error) {
	if s.groupService == nil || target == nil {
		return false, nil
	}
	targetWallet := strings.ToLower(strings.TrimSpace(target.WalletAddress))
	if targetWallet == "" {
		return false, nil
	}
	members, err := s.groupService.ListMembers(ctx, target)
	if err != nil {
		return false, err
	}
	for _, member := range members {
		groupID := strings.TrimSpace(member.GroupID)
		if _, ok := groupIDs[groupID]; !ok {
			continue
		}
		if group.NormalizeMemberStatus(member.Status) != group.MemberStatusActive {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(member.WalletAddress), targetWallet) {
			return true, nil
		}
	}
	return false, nil
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
