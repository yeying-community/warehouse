package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/yeying-community/webdav/internal/domain/recycle"
)

// RecycleRepository 回收站仓储接口
type RecycleRepository interface {
	// Create 创建回收站项目
	Create(ctx context.Context, item *recycle.RecycleItem) error

	// GetByHash 根据哈希获取项目
	GetByHash(ctx context.Context, hash string) (*recycle.RecycleItem, error)

	// GetByUserID 获取用户的所有回收站项目
	GetByUserID(ctx context.Context, userID string) ([]*recycle.RecycleItem, error)

	// DeleteByHash 根据哈希删除项目
	DeleteByHash(ctx context.Context, hash string) error

	// DeleteByUserID 删除用户的所有回收站项目
	DeleteByUserID(ctx context.Context, userID string) error

	// DeleteExpiredItems 删除过期项目
	DeleteExpiredItems(ctx context.Context, retentionPeriod time.Duration) (int64, error)
}

// PostgresRecycleRepository PostgreSQL 实现
type PostgresRecycleRepository struct {
	db *sql.DB
}

// NewPostgresRecycleRepository 创建 PostgreSQL 回收站仓储
func NewPostgresRecycleRepository(db *sql.DB) *PostgresRecycleRepository {
	return &PostgresRecycleRepository{db: db}
}

// Create 创建回收站项目
func (r *PostgresRecycleRepository) Create(ctx context.Context, item *recycle.RecycleItem) error {
	query := `
		INSERT INTO recycle_items (id, hash, user_id, username, directory, name, path, size, deleted_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := r.db.ExecContext(ctx, query,
		item.ID,
		item.Hash,
		item.UserID,
		item.Username,
		item.Directory,
		item.Name,
		item.Path,
		item.Size,
		item.DeletedAt,
		item.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create recycle item: %w", err)
	}
	return nil
}

// GetByHash 根据哈希获取项目
func (r *PostgresRecycleRepository) GetByHash(ctx context.Context, hash string) (*recycle.RecycleItem, error) {
	query := `
		SELECT id, hash, user_id, username, directory, name, path, size, deleted_at, created_at
		FROM recycle_items
		WHERE hash = $1
	`
	item := &recycle.RecycleItem{}
	err := r.db.QueryRowContext(ctx, query, hash).Scan(
		&item.ID,
		&item.Hash,
		&item.UserID,
		&item.Username,
		&item.Directory,
		&item.Name,
		&item.Path,
		&item.Size,
		&item.DeletedAt,
		&item.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, recycle.ErrRecycleItemNotFound
		}
		return nil, fmt.Errorf("failed to get recycle item: %w", err)
	}
	return item, nil
}

// GetByUserID 获取用户的所有回收站项目
func (r *PostgresRecycleRepository) GetByUserID(ctx context.Context, userID string) ([]*recycle.RecycleItem, error) {
	query := `
		SELECT id, hash, user_id, username, directory, name, path, size, deleted_at, created_at
		FROM recycle_items
		WHERE user_id = $1
		ORDER BY deleted_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query recycle items: %w", err)
	}
	defer rows.Close()

	var items []*recycle.RecycleItem
	for rows.Next() {
		item := &recycle.RecycleItem{}
		if err := rows.Scan(
			&item.ID,
			&item.Hash,
			&item.UserID,
			&item.Username,
			&item.Directory,
			&item.Name,
			&item.Path,
			&item.Size,
			&item.DeletedAt,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan recycle item: %w", err)
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate recycle items: %w", err)
	}

	return items, nil
}

// DeleteByHash 根据哈希删除项目
func (r *PostgresRecycleRepository) DeleteByHash(ctx context.Context, hash string) error {
	query := `DELETE FROM recycle_items WHERE hash = $1`
	result, err := r.db.ExecContext(ctx, query, hash)
	if err != nil {
		return fmt.Errorf("failed to delete recycle item: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return recycle.ErrRecycleItemNotFound
	}
	return nil
}

// DeleteByUserID 删除用户的所有回收站项目
func (r *PostgresRecycleRepository) DeleteByUserID(ctx context.Context, userID string) error {
	query := `DELETE FROM recycle_items WHERE user_id = $1`
	_, err := r.db.ExecContext(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("failed to delete recycle items: %w", err)
	}
	return nil
}

// GetDeletedItemsOlderThan 获取指定时间之前删除的项目
func (r *PostgresRecycleRepository) GetDeletedItemsOlderThan(ctx context.Context, before time.Time) ([]*recycle.RecycleItem, error) {
	query := `
		SELECT id, hash, user_id, username, directory, name, path, size, deleted_at, created_at
		FROM recycle_items
		WHERE deleted_at < $1
		ORDER BY deleted_at ASC
	`
	rows, err := r.db.QueryContext(ctx, query, before)
	if err != nil {
		return nil, fmt.Errorf("failed to query recycle items: %w", err)
	}
	defer rows.Close()

	var items []*recycle.RecycleItem
	for rows.Next() {
		item := &recycle.RecycleItem{}
		if err := rows.Scan(
			&item.ID,
			&item.Hash,
			&item.UserID,
			&item.Username,
			&item.Directory,
			&item.Name,
			&item.Path,
			&item.Size,
			&item.DeletedAt,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan recycle item: %w", err)
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate recycle items: %w", err)
	}

	return items, nil
}

// DeleteExpiredItems 删除过期项目（可配置保留期限）
func (r *PostgresRecycleRepository) DeleteExpiredItems(ctx context.Context, retentionPeriod time.Duration) (int64, error) {
	cutoff := time.Now().Add(-retentionPeriod)
	query := `DELETE FROM recycle_items WHERE deleted_at < $1`
	result, err := r.db.ExecContext(ctx, query, cutoff)
	if err != nil {
		return 0, fmt.Errorf("failed to delete expired items: %w", err)
	}
	return result.RowsAffected()
}