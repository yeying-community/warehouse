package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"

	"github.com/yeying-community/warehouse/internal/domain/notification"
)

type NotificationRepository interface {
	Create(ctx context.Context, item *notification.Notification) error
	UpsertByDedupeKey(ctx context.Context, item *notification.Notification) error
	ListForUser(ctx context.Context, userID string, limit int) ([]*notification.Notification, error)
	ListForRole(ctx context.Context, role string, limit int) ([]*notification.Notification, error)
	UnreadCountForUser(ctx context.Context, userID string) (int, error)
	UnreadCountForRole(ctx context.Context, role string) (int, error)
	MarkReadForUser(ctx context.Context, userID string, ids []string) error
	MarkAllReadForUser(ctx context.Context, userID string) error
	MarkReadForRole(ctx context.Context, role string, ids []string) error
	MarkAllReadForRole(ctx context.Context, role string) error
	GetPreferences(ctx context.Context, userID string) ([]notification.Preference, error)
	SetPreference(ctx context.Context, userID, notificationType string, enabled bool) error
}

type PostgresNotificationRepository struct {
	db *sql.DB
}

func NewPostgresNotificationRepository(db *sql.DB) *PostgresNotificationRepository {
	return &PostgresNotificationRepository{db: db}
}

func (r *PostgresNotificationRepository) Create(ctx context.Context, item *notification.Notification) error {
	if item == nil {
		return fmt.Errorf("notification is required")
	}
	query := `
		INSERT INTO notifications (
			id, recipient_user_id, recipient_role, type, title, content, severity, action_url, dedupe_key, created_at, expires_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`
	_, err := r.db.ExecContext(ctx, query,
		item.ID,
		nullString(item.RecipientUserID),
		item.RecipientRole,
		item.Type,
		item.Title,
		item.Content,
		item.Severity,
		nullString(item.ActionURL),
		nullString(item.DedupeKey),
		item.CreatedAt,
		item.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create notification: %w", err)
	}
	return nil
}

func (r *PostgresNotificationRepository) UpsertByDedupeKey(ctx context.Context, item *notification.Notification) error {
	if item == nil {
		return fmt.Errorf("notification is required")
	}
	if strings.TrimSpace(item.DedupeKey) == "" {
		return r.Create(ctx, item)
	}
	query := `
		INSERT INTO notifications (
			id, recipient_user_id, recipient_role, type, title, content, severity, action_url, dedupe_key, created_at, expires_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (dedupe_key) DO UPDATE SET
			title = EXCLUDED.title,
			content = EXCLUDED.content,
			severity = EXCLUDED.severity,
			action_url = EXCLUDED.action_url,
			expires_at = EXCLUDED.expires_at
	`
	_, err := r.db.ExecContext(ctx, query,
		item.ID,
		nullString(item.RecipientUserID),
		item.RecipientRole,
		item.Type,
		item.Title,
		item.Content,
		item.Severity,
		nullString(item.ActionURL),
		item.DedupeKey,
		item.CreatedAt,
		item.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert notification: %w", err)
	}
	return nil
}

func (r *PostgresNotificationRepository) ListForUser(ctx context.Context, userID string, limit int) ([]*notification.Notification, error) {
	limit = normalizeNotificationLimit(limit)
	query := `
		SELECT id, recipient_user_id, recipient_role, type, title, content, severity, action_url, dedupe_key, read_at, created_at, expires_at
		FROM notifications
		WHERE
			(recipient_user_id = $1 OR recipient_role = 'all')
			AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY created_at DESC
		LIMIT $2
	`
	rows, err := r.db.QueryContext(ctx, query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list notifications: %w", err)
	}
	defer rows.Close()
	return scanNotifications(rows)
}

func (r *PostgresNotificationRepository) ListForRole(ctx context.Context, role string, limit int) ([]*notification.Notification, error) {
	limit = normalizeNotificationLimit(limit)
	query := `
		SELECT id, recipient_user_id, recipient_role, type, title, content, severity, action_url, dedupe_key, read_at, created_at, expires_at
		FROM notifications
		WHERE recipient_role = $1 AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY created_at DESC
		LIMIT $2
	`
	rows, err := r.db.QueryContext(ctx, query, role, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list role notifications: %w", err)
	}
	defer rows.Close()
	return scanNotifications(rows)
}

func (r *PostgresNotificationRepository) UnreadCountForUser(ctx context.Context, userID string) (int, error) {
	query := `
		SELECT COUNT(*)::INT
		FROM notifications
		WHERE (recipient_user_id = $1 OR recipient_role = 'all')
			AND read_at IS NULL
			AND (expires_at IS NULL OR expires_at > NOW())
	`
	var count int
	if err := r.db.QueryRowContext(ctx, query, userID).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count unread notifications: %w", err)
	}
	return count, nil
}

func (r *PostgresNotificationRepository) UnreadCountForRole(ctx context.Context, role string) (int, error) {
	query := `
		SELECT COUNT(*)::INT
		FROM notifications
		WHERE recipient_role = $1 AND read_at IS NULL AND (expires_at IS NULL OR expires_at > NOW())
	`
	var count int
	if err := r.db.QueryRowContext(ctx, query, role).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count role unread notifications: %w", err)
	}
	return count, nil
}

func (r *PostgresNotificationRepository) MarkReadForUser(ctx context.Context, userID string, ids []string) error {
	ids = normalizeIDs(ids)
	if len(ids) == 0 {
		return nil
	}
	query := `
		UPDATE notifications
		SET read_at = COALESCE(read_at, NOW())
		WHERE id = ANY($1::text[]) AND (recipient_user_id = $2 OR recipient_role = 'all')
	`
	_, err := r.db.ExecContext(ctx, query, pqArray(ids), userID)
	return err
}

func (r *PostgresNotificationRepository) MarkAllReadForUser(ctx context.Context, userID string) error {
	query := `
		UPDATE notifications
		SET read_at = COALESCE(read_at, NOW())
		WHERE (recipient_user_id = $1 OR recipient_role = 'all') AND read_at IS NULL
	`
	_, err := r.db.ExecContext(ctx, query, userID)
	return err
}

func (r *PostgresNotificationRepository) MarkReadForRole(ctx context.Context, role string, ids []string) error {
	ids = normalizeIDs(ids)
	if len(ids) == 0 {
		return nil
	}
	query := `
		UPDATE notifications
		SET read_at = COALESCE(read_at, NOW())
		WHERE id = ANY($1::text[]) AND recipient_role = $2
	`
	_, err := r.db.ExecContext(ctx, query, pqArray(ids), role)
	return err
}

func (r *PostgresNotificationRepository) MarkAllReadForRole(ctx context.Context, role string) error {
	query := `
		UPDATE notifications
		SET read_at = COALESCE(read_at, NOW())
		WHERE recipient_role = $1 AND read_at IS NULL
	`
	_, err := r.db.ExecContext(ctx, query, role)
	return err
}

func (r *PostgresNotificationRepository) GetPreferences(ctx context.Context, userID string) ([]notification.Preference, error) {
	query := `
		SELECT type, enabled
		FROM notification_preferences
		WHERE user_id = $1
	`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query notification preferences: %w", err)
	}
	defer rows.Close()

	items := make([]notification.Preference, 0)
	for rows.Next() {
		var item notification.Preference
		if err := rows.Scan(&item.Type, &item.Enabled); err != nil {
			return nil, fmt.Errorf("failed to scan notification preference: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate notification preferences: %w", err)
	}
	return items, nil
}

func (r *PostgresNotificationRepository) SetPreference(ctx context.Context, userID, notificationType string, enabled bool) error {
	query := `
		INSERT INTO notification_preferences (user_id, type, enabled, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id, type) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			updated_at = NOW()
	`
	if _, err := r.db.ExecContext(ctx, query, userID, notificationType, enabled); err != nil {
		return fmt.Errorf("failed to update notification preference: %w", err)
	}
	return nil
}

func scanNotifications(rows *sql.Rows) ([]*notification.Notification, error) {
	items := make([]*notification.Notification, 0)
	for rows.Next() {
		item := &notification.Notification{}
		var recipientUserID sql.NullString
		var actionURL sql.NullString
		var dedupeKey sql.NullString
		var readAt sql.NullTime
		var expiresAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&recipientUserID,
			&item.RecipientRole,
			&item.Type,
			&item.Title,
			&item.Content,
			&item.Severity,
			&actionURL,
			&dedupeKey,
			&readAt,
			&item.CreatedAt,
			&expiresAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan notification: %w", err)
		}
		item.RecipientUserID = recipientUserID.String
		item.ActionURL = actionURL.String
		item.DedupeKey = dedupeKey.String
		if readAt.Valid {
			item.ReadAt = &readAt.Time
		}
		if expiresAt.Valid {
			item.ExpiresAt = &expiresAt.Time
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate notifications: %w", err)
	}
	return items, nil
}

func normalizeNotificationLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func normalizeIDs(ids []string) []string {
	result := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func nullString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func pqArray(values []string) any {
	return interface{}(pqStringArray(values))
}

type pqStringArray []string

func (a pqStringArray) Value() (driver.Value, error) {
	if len(a) == 0 {
		return "{}", nil
	}
	escaped := make([]string, 0, len(a))
	for _, value := range a {
		escaped = append(escaped, `"`+strings.ReplaceAll(value, `"`, `\"`)+`"`)
	}
	return "{" + strings.Join(escaped, ",") + "}", nil
}

var _ driver.Valuer = pqStringArray{}
