package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/yeying-community/webdav/internal/domain/share"
)

// ShareRepository 文件分享仓储接口
type ShareRepository interface {
	Create(ctx context.Context, item *share.ShareItem) error
	GetByToken(ctx context.Context, token string) (*share.ShareItem, error)
	GetByUserID(ctx context.Context, userID string) ([]*share.ShareItem, error)
	DeleteByToken(ctx context.Context, token string) error
	IncrementView(ctx context.Context, token string) error
	IncrementDownload(ctx context.Context, token string) error
}

// PostgresShareRepository PostgreSQL 实现
type PostgresShareRepository struct {
	db *sql.DB
}

// NewPostgresShareRepository 创建 PostgreSQL 分享仓储
func NewPostgresShareRepository(db *sql.DB) *PostgresShareRepository {
	return &PostgresShareRepository{db: db}
}

// Create 创建分享记录
func (r *PostgresShareRepository) Create(ctx context.Context, item *share.ShareItem) error {
	query := `
		INSERT INTO share_items (id, token, user_id, username, name, path, expires_at, view_count, download_count, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := r.db.ExecContext(ctx, query,
		item.ID,
		item.Token,
		item.UserID,
		item.Username,
		item.Name,
		item.Path,
		item.ExpiresAt,
		item.ViewCount,
		item.DownloadCount,
		item.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create share item: %w", err)
	}
	return nil
}

// GetByToken 根据 token 获取分享记录
func (r *PostgresShareRepository) GetByToken(ctx context.Context, token string) (*share.ShareItem, error) {
	query := `
		SELECT id, token, user_id, username, name, path, expires_at, view_count, download_count, created_at
		FROM share_items
		WHERE token = $1
	`
	item := &share.ShareItem{}
	var expiresAt sql.NullTime
	err := r.db.QueryRowContext(ctx, query, token).Scan(
		&item.ID,
		&item.Token,
		&item.UserID,
		&item.Username,
		&item.Name,
		&item.Path,
		&expiresAt,
		&item.ViewCount,
		&item.DownloadCount,
		&item.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, share.ErrShareNotFound
		}
		return nil, fmt.Errorf("failed to get share item: %w", err)
	}
	if expiresAt.Valid {
		item.ExpiresAt = &expiresAt.Time
	}
	return item, nil
}

// GetByUserID 获取用户的分享列表
func (r *PostgresShareRepository) GetByUserID(ctx context.Context, userID string) ([]*share.ShareItem, error) {
	query := `
		SELECT id, token, user_id, username, name, path, expires_at, view_count, download_count, created_at
		FROM share_items
		WHERE user_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query share items: %w", err)
	}
	defer rows.Close()

	var items []*share.ShareItem
	for rows.Next() {
		item := &share.ShareItem{}
		var expiresAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.Token,
			&item.UserID,
			&item.Username,
			&item.Name,
			&item.Path,
			&expiresAt,
			&item.ViewCount,
			&item.DownloadCount,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan share item: %w", err)
		}
		if expiresAt.Valid {
			item.ExpiresAt = &expiresAt.Time
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate share items: %w", err)
	}
	return items, nil
}

// DeleteByToken 删除分享记录
func (r *PostgresShareRepository) DeleteByToken(ctx context.Context, token string) error {
	query := `DELETE FROM share_items WHERE token = $1`
	result, err := r.db.ExecContext(ctx, query, token)
	if err != nil {
		return fmt.Errorf("failed to delete share item: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return share.ErrShareNotFound
	}
	return nil
}

// IncrementView 增加访问次数
func (r *PostgresShareRepository) IncrementView(ctx context.Context, token string) error {
	query := `UPDATE share_items SET view_count = view_count + 1 WHERE token = $1`
	result, err := r.db.ExecContext(ctx, query, token)
	if err != nil {
		return fmt.Errorf("failed to increment view count: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return share.ErrShareNotFound
	}
	return nil
}

// IncrementDownload 增加下载次数
func (r *PostgresShareRepository) IncrementDownload(ctx context.Context, token string) error {
	query := `UPDATE share_items SET download_count = download_count + 1 WHERE token = $1`
	result, err := r.db.ExecContext(ctx, query, token)
	if err != nil {
		return fmt.Errorf("failed to increment download count: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return share.ErrShareNotFound
	}
	return nil
}
