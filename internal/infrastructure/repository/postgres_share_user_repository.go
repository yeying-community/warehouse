package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/yeying-community/warehouse/internal/domain/shareuser"
)

// UserShareRepository 定向分享仓储接口
type UserShareRepository interface {
	Create(ctx context.Context, item *shareuser.ShareUserItem) error
	GetByID(ctx context.Context, id string) (*shareuser.ShareUserItem, error)
	GetByOwnerID(ctx context.Context, ownerID string) ([]*shareuser.ShareUserItem, error)
	GetByTargetID(ctx context.Context, targetID string) ([]*shareuser.ShareUserItem, error)
	DeleteByID(ctx context.Context, id string) error
}

// PostgresUserShareRepository PostgreSQL 实现
type PostgresUserShareRepository struct {
	db *sql.DB
}

// NewPostgresUserShareRepository 创建 PostgreSQL 定向分享仓储
func NewPostgresUserShareRepository(db *sql.DB) *PostgresUserShareRepository {
	return &PostgresUserShareRepository{db: db}
}

func (r *PostgresUserShareRepository) Create(ctx context.Context, item *shareuser.ShareUserItem) error {
	query := `
		INSERT INTO share_user_items (id, owner_user_id, owner_username, target_user_id, target_wallet_address,
			name, path, is_dir, permissions, expires_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`
	_, err := r.db.ExecContext(ctx, query,
		item.ID,
		item.OwnerUserID,
		item.OwnerUsername,
		item.TargetUserID,
		item.TargetWalletAddress,
		item.Name,
		item.Path,
		item.IsDir,
		item.Permissions,
		item.ExpiresAt,
		item.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create share user item: %w", err)
	}
	return nil
}

func (r *PostgresUserShareRepository) GetByID(ctx context.Context, id string) (*shareuser.ShareUserItem, error) {
	query := `
		SELECT id, owner_user_id, owner_username, target_user_id, target_wallet_address,
		       name, path, is_dir, permissions, expires_at, created_at
		FROM share_user_items
		WHERE id = $1
	`

	item := &shareuser.ShareUserItem{}
	var expiresAt sql.NullTime
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&item.ID,
		&item.OwnerUserID,
		&item.OwnerUsername,
		&item.TargetUserID,
		&item.TargetWalletAddress,
		&item.Name,
		&item.Path,
		&item.IsDir,
		&item.Permissions,
		&expiresAt,
		&item.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, shareuser.ErrShareNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get share user item: %w", err)
	}
	if expiresAt.Valid {
		item.ExpiresAt = &expiresAt.Time
	}
	return item, nil
}

func (r *PostgresUserShareRepository) GetByOwnerID(ctx context.Context, ownerID string) ([]*shareuser.ShareUserItem, error) {
	query := `
		SELECT id, owner_user_id, owner_username, target_user_id, target_wallet_address,
		       name, path, is_dir, permissions, expires_at, created_at
		FROM share_user_items
		WHERE owner_user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, fmt.Errorf("failed to query share user items: %w", err)
	}
	defer rows.Close()

	var items []*shareuser.ShareUserItem
	for rows.Next() {
		item := &shareuser.ShareUserItem{}
		var expiresAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.OwnerUserID,
			&item.OwnerUsername,
			&item.TargetUserID,
			&item.TargetWalletAddress,
			&item.Name,
			&item.Path,
			&item.IsDir,
			&item.Permissions,
			&expiresAt,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan share user item: %w", err)
		}
		if expiresAt.Valid {
			item.ExpiresAt = &expiresAt.Time
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate share user items: %w", err)
	}
	return items, nil
}

func (r *PostgresUserShareRepository) GetByTargetID(ctx context.Context, targetID string) ([]*shareuser.ShareUserItem, error) {
	query := `
		SELECT id, owner_user_id, owner_username, target_user_id, target_wallet_address,
		       name, path, is_dir, permissions, expires_at, created_at
		FROM share_user_items
		WHERE target_user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, targetID)
	if err != nil {
		return nil, fmt.Errorf("failed to query share user items: %w", err)
	}
	defer rows.Close()

	var items []*shareuser.ShareUserItem
	for rows.Next() {
		item := &shareuser.ShareUserItem{}
		var expiresAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.OwnerUserID,
			&item.OwnerUsername,
			&item.TargetUserID,
			&item.TargetWalletAddress,
			&item.Name,
			&item.Path,
			&item.IsDir,
			&item.Permissions,
			&expiresAt,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan share user item: %w", err)
		}
		if expiresAt.Valid {
			item.ExpiresAt = &expiresAt.Time
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate share user items: %w", err)
	}
	return items, nil
}

func (r *PostgresUserShareRepository) DeleteByID(ctx context.Context, id string) error {
	query := `DELETE FROM share_user_items WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete share user item: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if rows == 0 {
		return shareuser.ErrShareNotFound
	}
	return nil
}
