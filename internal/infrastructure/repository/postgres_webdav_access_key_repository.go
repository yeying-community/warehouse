package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/accesskey"
)

type WebDAVAccessKeyRepository interface {
	Create(ctx context.Context, item *accesskey.WebDAVAccessKey) error
	ListByOwner(ctx context.Context, ownerUserID string) ([]*accesskey.WebDAVAccessKey, error)
	GetByID(ctx context.Context, ownerUserID, id string) (*accesskey.WebDAVAccessKey, error)
	FindByKeyID(ctx context.Context, keyID string) (*accesskey.WebDAVAccessKey, error)
	ListBindingPathsByAccessKeyID(ctx context.Context, accessKeyID string) ([]string, error)
	BindPath(ctx context.Context, ownerUserID, accessKeyID, rootPath string) error
	RevokeByID(ctx context.Context, ownerUserID, id string) error
	TouchByID(ctx context.Context, id string, usedAt time.Time) error
}

type PostgresWebDAVAccessKeyRepository struct {
	db *sql.DB
}

func NewPostgresWebDAVAccessKeyRepository(db *sql.DB) *PostgresWebDAVAccessKeyRepository {
	return &PostgresWebDAVAccessKeyRepository{db: db}
}

func (r *PostgresWebDAVAccessKeyRepository) Create(ctx context.Context, item *accesskey.WebDAVAccessKey) error {
	query := `
		INSERT INTO webdav_access_keys (
			id, owner_user_id, name, key_id, secret_hash, root_path, permissions, status, expires_at, last_used_at, created_at, updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
	`
	_, err := r.db.ExecContext(ctx, query,
		item.ID,
		item.OwnerUserID,
		item.Name,
		item.KeyID,
		item.SecretHash,
		item.RootPath,
		strings.ToUpper(strings.TrimSpace(item.Permissions)),
		item.Status,
		item.ExpiresAt,
		item.LastUsedAt,
		item.CreatedAt,
		item.UpdatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "idx_webdav_access_keys_owner_name") {
			return accesskey.ErrDuplicateName
		}
		return fmt.Errorf("failed to create webdav access key: %w", err)
	}
	return nil
}

func (r *PostgresWebDAVAccessKeyRepository) ListByOwner(ctx context.Context, ownerUserID string) ([]*accesskey.WebDAVAccessKey, error) {
	query := `
		SELECT id, owner_user_id, name, key_id, secret_hash, root_path, permissions, status, expires_at, last_used_at, created_at, updated_at
		FROM webdav_access_keys
		WHERE owner_user_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, ownerUserID)
	if err != nil {
		return nil, fmt.Errorf("failed to list webdav access keys: %w", err)
	}
	defer rows.Close()
	return scanWebDAVAccessKeys(rows)
}

func (r *PostgresWebDAVAccessKeyRepository) GetByID(ctx context.Context, ownerUserID, id string) (*accesskey.WebDAVAccessKey, error) {
	query := `
		SELECT id, owner_user_id, name, key_id, secret_hash, root_path, permissions, status, expires_at, last_used_at, created_at, updated_at
		FROM webdav_access_keys
		WHERE owner_user_id = $1 AND id = $2
	`
	row := r.db.QueryRowContext(ctx, query, ownerUserID, id)
	item, err := scanOneWebDAVAccessKey(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, accesskey.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get webdav access key: %w", err)
	}
	return item, nil
}

func (r *PostgresWebDAVAccessKeyRepository) FindByKeyID(ctx context.Context, keyID string) (*accesskey.WebDAVAccessKey, error) {
	query := `
		SELECT id, owner_user_id, name, key_id, secret_hash, root_path, permissions, status, expires_at, last_used_at, created_at, updated_at
		FROM webdav_access_keys
		WHERE key_id = $1
	`
	row := r.db.QueryRowContext(ctx, query, keyID)
	item, err := scanOneWebDAVAccessKey(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, accesskey.ErrNotFound
		}
		return nil, fmt.Errorf("failed to find webdav access key: %w", err)
	}
	return item, nil
}

func (r *PostgresWebDAVAccessKeyRepository) RevokeByID(ctx context.Context, ownerUserID, id string) error {
	query := `
		UPDATE webdav_access_keys
		SET status = $1, updated_at = NOW()
		WHERE owner_user_id = $2 AND id = $3
	`
	res, err := r.db.ExecContext(ctx, query, accesskey.StatusRevoked, ownerUserID, id)
	if err != nil {
		return fmt.Errorf("failed to revoke webdav access key: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check revoked rows: %w", err)
	}
	if affected == 0 {
		return accesskey.ErrNotFound
	}
	return nil
}

func (r *PostgresWebDAVAccessKeyRepository) ListBindingPathsByAccessKeyID(ctx context.Context, accessKeyID string) ([]string, error) {
	query := `
		SELECT root_path
		FROM webdav_access_key_bindings
		WHERE access_key_id = $1
		ORDER BY root_path ASC
	`
	rows, err := r.db.QueryContext(ctx, query, accessKeyID)
	if err != nil {
		return nil, fmt.Errorf("failed to list access key bindings: %w", err)
	}
	defer rows.Close()

	items := make([]string, 0)
	for rows.Next() {
		var rootPath string
		if err := rows.Scan(&rootPath); err != nil {
			return nil, fmt.Errorf("failed to scan access key binding: %w", err)
		}
		items = append(items, rootPath)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate access key bindings: %w", err)
	}
	return items, nil
}

func (r *PostgresWebDAVAccessKeyRepository) BindPath(ctx context.Context, ownerUserID, accessKeyID, rootPath string) error {
	query := `
		INSERT INTO webdav_access_key_bindings (access_key_id, owner_user_id, root_path, created_at)
		SELECT k.id, k.owner_user_id, $3, NOW()
		FROM webdav_access_keys k
		WHERE k.id = $1 AND k.owner_user_id = $2
		ON CONFLICT (access_key_id, root_path) DO NOTHING
	`
	if _, err := r.db.ExecContext(ctx, query, accessKeyID, ownerUserID, rootPath); err != nil {
		return fmt.Errorf("failed to bind access key path: %w", err)
	}
	return nil
}

func (r *PostgresWebDAVAccessKeyRepository) TouchByID(ctx context.Context, id string, usedAt time.Time) error {
	query := `
		UPDATE webdav_access_keys
		SET last_used_at = $1, updated_at = NOW()
		WHERE id = $2
	`
	if _, err := r.db.ExecContext(ctx, query, usedAt, id); err != nil {
		return fmt.Errorf("failed to update access key last_used_at: %w", err)
	}
	return nil
}

type webdavAccessKeyScanner interface {
	Scan(dest ...any) error
}

func scanOneWebDAVAccessKey(scanner webdavAccessKeyScanner) (*accesskey.WebDAVAccessKey, error) {
	item := &accesskey.WebDAVAccessKey{}
	var expiresAt sql.NullTime
	var lastUsedAt sql.NullTime
	if err := scanner.Scan(
		&item.ID,
		&item.OwnerUserID,
		&item.Name,
		&item.KeyID,
		&item.SecretHash,
		&item.RootPath,
		&item.Permissions,
		&item.Status,
		&expiresAt,
		&lastUsedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if expiresAt.Valid {
		item.ExpiresAt = &expiresAt.Time
	}
	if lastUsedAt.Valid {
		item.LastUsedAt = &lastUsedAt.Time
	}
	return item, nil
}

func scanWebDAVAccessKeys(rows *sql.Rows) ([]*accesskey.WebDAVAccessKey, error) {
	items := make([]*accesskey.WebDAVAccessKey, 0)
	for rows.Next() {
		item, err := scanOneWebDAVAccessKey(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan webdav access key: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate webdav access keys: %w", err)
	}
	return items, nil
}
