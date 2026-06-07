package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/yeying-community/warehouse/internal/domain/notification"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
	"go.uber.org/zap"
)

type NotificationService struct {
	repo     repository.NotificationRepository
	userRepo user.Repository
	logger   *zap.Logger
}

type AnnouncementInput struct {
	RecipientRole   string
	TargetUsernames []string
	Title           string
	Content         string
	Severity        string
	ActionURL       string
}

func NewNotificationService(repo repository.NotificationRepository, userRepo user.Repository, logger *zap.Logger) *NotificationService {
	return &NotificationService{
		repo:     repo,
		userRepo: userRepo,
		logger:   logger,
	}
}

func (s *NotificationService) ListForUser(ctx context.Context, u *user.User, limit int) ([]*notification.Notification, error) {
	if s == nil || s.repo == nil || u == nil {
		return nil, nil
	}
	return s.repo.ListForUser(ctx, u.ID, limit)
}

func (s *NotificationService) ListForAdmin(ctx context.Context, limit int) ([]*notification.Notification, error) {
	if s == nil || s.repo == nil {
		return nil, nil
	}
	if err := s.EnsureAdminQuotaNotifications(ctx); err != nil {
		s.logger.Warn("failed to ensure admin quota notifications", zap.Error(err))
	}
	return s.repo.ListForRole(ctx, notification.RecipientRoleAdmin, limit)
}

func (s *NotificationService) UnreadCountForUser(ctx context.Context, u *user.User) (int, error) {
	if s == nil || s.repo == nil || u == nil {
		return 0, nil
	}
	return s.repo.UnreadCountForUser(ctx, u.ID)
}

func (s *NotificationService) UnreadCountForAdmin(ctx context.Context) (int, error) {
	if s == nil || s.repo == nil {
		return 0, nil
	}
	if err := s.EnsureAdminQuotaNotifications(ctx); err != nil {
		s.logger.Warn("failed to ensure admin quota notifications", zap.Error(err))
	}
	return s.repo.UnreadCountForRole(ctx, notification.RecipientRoleAdmin)
}

func (s *NotificationService) MarkReadForUser(ctx context.Context, u *user.User, ids []string) error {
	if s == nil || s.repo == nil || u == nil {
		return nil
	}
	return s.repo.MarkReadForUser(ctx, u.ID, ids)
}

func (s *NotificationService) MarkAllReadForUser(ctx context.Context, u *user.User) error {
	if s == nil || s.repo == nil || u == nil {
		return nil
	}
	return s.repo.MarkAllReadForUser(ctx, u.ID)
}

func (s *NotificationService) MarkReadForAdmin(ctx context.Context, ids []string) error {
	if s == nil || s.repo == nil {
		return nil
	}
	return s.repo.MarkReadForRole(ctx, notification.RecipientRoleAdmin, ids)
}

func (s *NotificationService) MarkAllReadForAdmin(ctx context.Context) error {
	if s == nil || s.repo == nil {
		return nil
	}
	return s.repo.MarkAllReadForRole(ctx, notification.RecipientRoleAdmin)
}

func (s *NotificationService) GetPreferences(ctx context.Context, u *user.User) ([]notification.Preference, error) {
	defaults := defaultPreferences()
	if s == nil || s.repo == nil || u == nil {
		return defaults, nil
	}
	stored, err := s.repo.GetPreferences(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	byType := make(map[string]bool, len(stored))
	for _, item := range stored {
		byType[item.Type] = item.Enabled
	}
	for i := range defaults {
		if enabled, ok := byType[defaults[i].Type]; ok {
			defaults[i].Enabled = enabled
		}
	}
	return defaults, nil
}

func (s *NotificationService) SetPreference(ctx context.Context, u *user.User, notificationType string, enabled bool) error {
	if s == nil || s.repo == nil || u == nil {
		return nil
	}
	notificationType = strings.TrimSpace(notificationType)
	if !isKnownPreferenceType(notificationType) {
		return fmt.Errorf("unsupported notification type: %s", notificationType)
	}
	return s.repo.SetPreference(ctx, u.ID, notificationType, enabled)
}

func (s *NotificationService) CreateAnnouncement(ctx context.Context, input AnnouncementInput) (int, error) {
	if s == nil || s.repo == nil {
		return 0, nil
	}
	title := strings.TrimSpace(input.Title)
	content := strings.TrimSpace(input.Content)
	if title == "" {
		return 0, fmt.Errorf("title is required")
	}
	if content == "" {
		return 0, fmt.Errorf("content is required")
	}
	severity := strings.TrimSpace(input.Severity)
	if severity == "" {
		severity = notification.SeverityInfo
	}
	role := strings.TrimSpace(input.RecipientRole)
	if role == "" {
		role = notification.RecipientRoleAll
	}

	switch role {
	case notification.RecipientRoleAdmin:
		if err := s.repo.Create(ctx, notification.New(notification.CreateInput{
			RecipientRole: notification.RecipientRoleAdmin,
			Type:          notification.TypeAdminNotice,
			Title:         title,
			Content:       content,
			Severity:      severity,
			ActionURL:     input.ActionURL,
		})); err != nil {
			return 0, err
		}
		return 1, nil
	case notification.RecipientRoleAll:
		users, err := s.userRepo.List(ctx)
		if err != nil {
			return 0, err
		}
		count := 0
		for _, target := range users {
			if target == nil {
				continue
			}
			if err := s.createForUserIfEnabled(ctx, target.ID, notification.CreateInput{
				RecipientUserID: target.ID,
				RecipientRole:   notification.RecipientRoleUser,
				Type:            notification.TypeAdminNotice,
				Title:           title,
				Content:         content,
				Severity:        severity,
				ActionURL:       input.ActionURL,
			}); err != nil {
				return count, err
			}
			count++
		}
		return count, nil
	case notification.RecipientRoleUser:
		count := 0
		seen := make(map[string]struct{}, len(input.TargetUsernames))
		for _, username := range input.TargetUsernames {
			username = strings.TrimSpace(username)
			if username == "" {
				continue
			}
			if _, ok := seen[username]; ok {
				continue
			}
			seen[username] = struct{}{}
			target, err := s.userRepo.FindByUsername(ctx, username)
			if err != nil {
				return count, err
			}
			if err := s.createForUserIfEnabled(ctx, target.ID, notification.CreateInput{
				RecipientUserID: target.ID,
				RecipientRole:   notification.RecipientRoleUser,
				Type:            notification.TypeAdminNotice,
				Title:           title,
				Content:         content,
				Severity:        severity,
				ActionURL:       input.ActionURL,
			}); err != nil {
				return count, err
			}
			count++
		}
		if count == 0 {
			return 0, fmt.Errorf("target usernames are required")
		}
		return count, nil
	default:
		return 0, fmt.Errorf("unsupported recipient role: %s", role)
	}
}

func (s *NotificationService) NotifyShareCreated(ctx context.Context, owner *user.User, itemID, itemName, itemPath string, targetUsers []repository.UserShareAudience, allUsers bool) {
	if s == nil || s.repo == nil || owner == nil {
		return
	}
	title := "收到新的定向分享"
	content := fmt.Sprintf("%s 向你分享了 %s", owner.Username, displayShareName(itemName, itemPath))
	actionURL := "#shared-with-me"
	if allUsers {
		users, err := s.userRepo.List(ctx)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("failed to list users for share notification",
					zap.String("share_id", itemID),
					zap.Error(err))
			}
			return
		}
		for _, target := range users {
			if target == nil || target.ID == owner.ID {
				continue
			}
			s.upsertForUserIfEnabled(ctx, target.ID, notification.CreateInput{
				RecipientUserID: target.ID,
				RecipientRole:   notification.RecipientRoleUser,
				Type:            notification.TypeShare,
				Title:           title,
				Content:         content,
				Severity:        notification.SeverityInfo,
				ActionURL:       actionURL,
				DedupeKey:       "share:user:" + itemID + ":" + target.ID,
			})
		}
		return
	}
	seen := make(map[string]struct{}, len(targetUsers))
	for _, target := range targetUsers {
		userID := strings.TrimSpace(target.TargetUserID)
		if userID == "" || userID == owner.ID {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		s.upsertForUserIfEnabled(ctx, userID, notification.CreateInput{
			RecipientUserID: userID,
			RecipientRole:   notification.RecipientRoleUser,
			Type:            notification.TypeShare,
			Title:           title,
			Content:         content,
			Severity:        notification.SeverityInfo,
			ActionURL:       actionURL,
			DedupeKey:       "share:user:" + itemID + ":" + userID,
		})
	}
}

func (s *NotificationService) EnsureUserQuotaNotification(ctx context.Context, u *user.User, quotaValue, used int64) error {
	if s == nil || s.repo == nil || u == nil || quotaValue <= 0 {
		return nil
	}
	percent := float64(used) / float64(quotaValue) * 100
	if percent < 90 {
		return nil
	}
	severity := notification.SeverityWarning
	title := "存储额度接近上限"
	if percent >= 100 {
		severity = notification.SeverityError
		title = "存储额度已超额"
	}
	return s.upsertForUserIfEnabled(ctx, u.ID, notification.CreateInput{
		RecipientUserID: u.ID,
		RecipientRole:   notification.RecipientRoleUser,
		Type:            notification.TypeQuota,
		Title:           title,
		Content:         fmt.Sprintf("当前已使用 %.2f%%，请清理文件或联系管理员调整额度。", percent),
		Severity:        severity,
		ActionURL:       "#quota",
		DedupeKey:       fmt.Sprintf("quota:user:%s:%s", u.ID, severity),
	})
}

func (s *NotificationService) EnsureAdminQuotaNotifications(ctx context.Context) error {
	if s == nil || s.repo == nil || s.userRepo == nil {
		return nil
	}
	users, err := s.userRepo.List(ctx)
	if err != nil {
		return err
	}
	for _, u := range users {
		if u == nil || u.Quota <= 0 {
			continue
		}
		percent := float64(u.UsedSpace) / float64(u.Quota) * 100
		if percent < 90 {
			continue
		}
		severity := notification.SeverityWarning
		title := "用户额度接近上限"
		if percent >= 100 {
			severity = notification.SeverityError
			title = "用户额度已超额"
		}
		if err := s.upsert(ctx, notification.CreateInput{
			RecipientRole: notification.RecipientRoleAdmin,
			Type:          notification.TypeQuota,
			Title:         title,
			Content:       fmt.Sprintf("用户 %s 当前已使用 %.2f%%。", u.Username, percent),
			Severity:      severity,
			ActionURL:     "#admin-quota",
			DedupeKey:     fmt.Sprintf("quota:admin:%s:%s", u.ID, severity),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *NotificationService) createForUserIfEnabled(ctx context.Context, userID string, input notification.CreateInput) error {
	if !s.preferenceEnabled(ctx, userID, input.Type) {
		return nil
	}
	return s.repo.Create(ctx, notification.New(input))
}

func (s *NotificationService) upsertForUserIfEnabled(ctx context.Context, userID string, input notification.CreateInput) error {
	if !s.preferenceEnabled(ctx, userID, input.Type) {
		return nil
	}
	return s.upsert(ctx, input)
}

func (s *NotificationService) upsert(ctx context.Context, input notification.CreateInput) error {
	item := notification.New(input)
	if err := s.repo.UpsertByDedupeKey(ctx, item); err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to save notification",
				zap.String("type", item.Type),
				zap.String("dedupe_key", item.DedupeKey),
				zap.Error(err))
		}
		return err
	}
	return nil
}

func (s *NotificationService) preferenceEnabled(ctx context.Context, userID, notificationType string) bool {
	if s == nil || s.repo == nil || userID == "" || notificationType == "" {
		return true
	}
	items, err := s.repo.GetPreferences(ctx, userID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to load notification preferences",
				zap.String("user_id", userID),
				zap.Error(err))
		}
		return true
	}
	for _, item := range items {
		if item.Type == notificationType {
			return item.Enabled
		}
	}
	return true
}

func defaultPreferences() []notification.Preference {
	items := make([]notification.Preference, 0, len(notification.PreferenceTypes))
	for _, itemType := range notification.PreferenceTypes {
		items = append(items, notification.Preference{
			Type:    itemType,
			Enabled: true,
		})
	}
	return items
}

func isKnownPreferenceType(itemType string) bool {
	for _, known := range notification.PreferenceTypes {
		if known == itemType {
			return true
		}
	}
	return false
}

func displayShareName(name, itemPath string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	itemPath = strings.TrimSpace(itemPath)
	if itemPath == "" {
		return "文件"
	}
	parts := strings.Split(strings.TrimRight(itemPath, "/"), "/")
	return parts[len(parts)-1]
}
