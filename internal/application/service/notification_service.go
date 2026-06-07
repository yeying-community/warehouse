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
			s.upsert(ctx, notification.CreateInput{
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
		s.upsert(ctx, notification.CreateInput{
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
	return s.upsert(ctx, notification.CreateInput{
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
