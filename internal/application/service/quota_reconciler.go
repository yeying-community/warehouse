package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/quota"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
	"go.uber.org/zap"
)

// QuotaReconciler periodically recalculates used_space to repair drift.
type QuotaReconciler struct {
	config          *config.Config
	users           user.Repository
	recycleRepo     repository.RecycleRepository
	quotaSvc        quota.Service
	notificationSvc *NotificationService
	logger          *zap.Logger
	interval        time.Duration
}

// NewQuotaReconciler creates a background quota reconciliation worker.
func NewQuotaReconciler(
	cfg *config.Config,
	users user.Repository,
	recycleRepo repository.RecycleRepository,
	quotaSvc quota.Service,
	logger *zap.Logger,
) *QuotaReconciler {
	if cfg == nil || users == nil || recycleRepo == nil || quotaSvc == nil {
		return nil
	}
	return &QuotaReconciler{
		config:      cfg,
		users:       users,
		recycleRepo: recycleRepo,
		quotaSvc:    quotaSvc,
		logger:      logger,
		interval:    cfg.Quota.AutoReconcileInterval,
	}
}

// SetNotificationService wires quota notification generation into the
// background reconciliation pass without making notification reads do writes.
func (r *QuotaReconciler) SetNotificationService(notificationSvc *NotificationService) {
	if r == nil {
		return
	}
	r.notificationSvc = notificationSvc
}

// Enabled reports whether automatic quota reconciliation should run on this node.
func (r *QuotaReconciler) Enabled() bool {
	if r == nil || r.config == nil {
		return false
	}
	if !r.config.Quota.AutoReconcileEnabled {
		return false
	}
	if r.config.Quota.AutoReconcileInterval <= 0 {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(r.config.Node.Role), "standby")
}

// Run starts the periodic quota reconciliation loop until ctx is canceled.
func (r *QuotaReconciler) Run(ctx context.Context) {
	if !r.Enabled() {
		return
	}

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	if r.logger != nil {
		r.logger.Info("quota reconciler started",
			zap.Duration("interval", r.interval),
			zap.String("node_role", strings.TrimSpace(r.config.Node.Role)))
		defer r.logger.Info("quota reconciler stopped")
	}

	if err := r.ReconcileOnce(ctx); err != nil && !errors.Is(err, context.Canceled) && r.logger != nil {
		r.logger.Warn("quota reconcile pass failed", zap.Error(err))
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.ReconcileOnce(ctx); err != nil && !errors.Is(err, context.Canceled) && r.logger != nil {
				r.logger.Warn("quota reconcile pass failed", zap.Error(err))
			}
		}
	}
}

// ReconcileOnce scans all users once and repairs stored used_space drift.
func (r *QuotaReconciler) ReconcileOnce(ctx context.Context) error {
	if !r.Enabled() {
		return nil
	}

	users, err := r.users.List(ctx)
	if err != nil {
		return err
	}

	var repaired int
	for _, u := range users {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if u == nil {
			continue
		}
		changed, err := r.reconcileUser(ctx, u)
		if err != nil {
			if r.logger != nil {
				r.logger.Warn("quota reconcile user failed",
					zap.String("username", u.Username),
					zap.Error(err))
			}
			continue
		}
		if changed {
			repaired++
		}
		if r.notificationSvc != nil {
			if err := r.notificationSvc.EnsureUserQuotaNotification(ctx, u, u.Quota, u.UsedSpace); err != nil && !errors.Is(err, context.Canceled) && r.logger != nil {
				r.logger.Warn("failed to ensure user quota notification",
					zap.String("username", u.Username),
					zap.Error(err))
			}
		}
	}

	if r.notificationSvc != nil {
		if err := r.notificationSvc.EnsureAdminQuotaNotifications(ctx); err != nil && !errors.Is(err, context.Canceled) && r.logger != nil {
			r.logger.Warn("failed to ensure admin quota notifications", zap.Error(err))
		}
	}

	if r.logger != nil {
		r.logger.Info("quota reconcile pass finished",
			zap.Int("user_count", len(users)),
			zap.Int("repaired_count", repaired))
	}

	return nil
}

func (r *QuotaReconciler) reconcileUser(ctx context.Context, u *user.User) (bool, error) {
	snapshot, err := CalculateQuotaUsage(ctx, r.config, r.quotaSvc, r.recycleRepo, u)
	if err != nil {
		return false, err
	}
	r.logQuotaRisk(u, snapshot)
	if snapshot.TotalUsed == u.UsedSpace {
		return false, nil
	}
	before := u.UsedSpace
	if err := r.users.UpdateUsedSpace(ctx, u.Username, snapshot.TotalUsed); err != nil {
		return false, err
	}
	u.UsedSpace = snapshot.TotalUsed
	if r.logger != nil {
		r.logger.Info("quota drift repaired",
			zap.String("username", u.Username),
			zap.Int64("before_used_space", before),
			zap.Int64("after_used_space", snapshot.TotalUsed),
			zap.Int64("active_used", snapshot.ActiveUsed),
			zap.Int64("recycle_used", snapshot.RecycleUsed),
			zap.String("quota_status", quotaUsageStatus(u, snapshot)),
			zap.String("quota_usage_percent", quotaUsagePercentText(u, snapshot)))
	}
	return true, nil
}

func (r *QuotaReconciler) logQuotaRisk(u *user.User, snapshot *QuotaUsageSnapshot) {
	if r == nil || r.logger == nil || u == nil || snapshot == nil {
		return
	}

	status := quotaUsageStatus(u, snapshot)
	if status == "ok" || status == "unlimited" {
		return
	}

	fields := []zap.Field{
		zap.String("username", u.Username),
		zap.Int64("quota", u.Quota),
		zap.Int64("used_space", snapshot.TotalUsed),
		zap.Int64("active_used", snapshot.ActiveUsed),
		zap.Int64("recycle_used", snapshot.RecycleUsed),
		zap.String("quota_status", status),
		zap.String("quota_usage_percent", quotaUsagePercentText(u, snapshot)),
	}

	switch status {
	case "over_quota":
		r.logger.Warn("quota user exceeded storage limit", fields...)
	case "near_limit":
		r.logger.Info("quota user is nearing storage limit", fields...)
	}
}

func quotaUsageStatus(u *user.User, snapshot *QuotaUsageSnapshot) string {
	if u == nil || snapshot == nil {
		return "unknown"
	}
	if u.Quota <= 0 {
		return "unlimited"
	}
	if snapshot.TotalUsed > u.Quota {
		return "over_quota"
	}
	if float64(snapshot.TotalUsed)/float64(u.Quota) >= 0.8 {
		return "near_limit"
	}
	return "ok"
}

func quotaUsagePercentText(u *user.User, snapshot *QuotaUsageSnapshot) string {
	if u == nil || snapshot == nil || u.Quota <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.2f%%", float64(snapshot.TotalUsed)/float64(u.Quota)*100)
}
