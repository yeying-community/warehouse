package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yeying-community/warehouse/internal/domain/s3credential"
	"github.com/yeying-community/warehouse/internal/infrastructure/crypto"
)

type S3CredentialRepository interface {
	Create(ctx context.Context, credential *s3credential.Credential) error
	ListByOwner(ctx context.Context, ownerUserID string) ([]*s3credential.Credential, error)
	FindByAccessKeyID(ctx context.Context, accessKeyID string) (*s3credential.Credential, error)
	RevokeByID(ctx context.Context, ownerUserID, id string) error
	TouchByID(ctx context.Context, id string, usedAt time.Time) error
}

type PostgresS3CredentialRepository struct {
	db        *sql.DB
	secretBox *crypto.SecretBox
}

func NewPostgresS3CredentialRepository(db *sql.DB, secretBox *crypto.SecretBox) *PostgresS3CredentialRepository {
	return &PostgresS3CredentialRepository{db: db, secretBox: secretBox}
}

func (r *PostgresS3CredentialRepository) Create(ctx context.Context, credential *s3credential.Credential) error {
	if credential == nil || strings.TrimSpace(credential.Secret) == "" {
		return s3credential.ErrInvalidCredential
	}
	if r.secretBox == nil {
		return crypto.ErrInvalidMasterKey
	}
	ciphertext, err := r.secretBox.Seal(credential.Secret)
	if err != nil {
		return fmt.Errorf("encrypt s3 secret: %w", err)
	}
	if credential.ID == "" {
		credential.ID = uuid.NewString()
	}
	if credential.CreatedAt.IsZero() {
		credential.CreatedAt = time.Now()
	}
	if credential.UpdatedAt.IsZero() {
		credential.UpdatedAt = credential.CreatedAt
	}
	if credential.Status == "" {
		credential.Status = s3credential.StatusActive
	}
	if credential.SecretKeyVersion == 0 {
		credential.SecretKeyVersion = 1
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO s3_credentials (
			id, owner_user_id, name, access_key_id, secret_ciphertext,
			secret_key_version, status, expires_at, last_used_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`, credential.ID, credential.OwnerUserID, credential.Name, credential.AccessKeyID,
		ciphertext, credential.SecretKeyVersion, credential.Status, credential.ExpiresAt,
		credential.LastUsedAt, credential.CreatedAt, credential.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create s3 credential: %w", err)
	}
	return nil
}

func (r *PostgresS3CredentialRepository) ListByOwner(ctx context.Context, ownerUserID string) ([]*s3credential.Credential, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, owner_user_id, name, access_key_id, secret_ciphertext,
			secret_key_version, status, expires_at, last_used_at, created_at, updated_at
		FROM s3_credentials WHERE owner_user_id = $1 ORDER BY created_at DESC
	`, ownerUserID)
	if err != nil {
		return nil, fmt.Errorf("list s3 credentials: %w", err)
	}
	defer rows.Close()
	return r.scanRows(rows)
}

func (r *PostgresS3CredentialRepository) FindByAccessKeyID(ctx context.Context, accessKeyID string) (*s3credential.Credential, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, owner_user_id, name, access_key_id, secret_ciphertext,
			secret_key_version, status, expires_at, last_used_at, created_at, updated_at
		FROM s3_credentials WHERE access_key_id = $1
	`, accessKeyID)
	credential, err := r.scanRow(row)
	if err == sql.ErrNoRows {
		return nil, s3credential.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find s3 credential: %w", err)
	}
	return credential, nil
}

func (r *PostgresS3CredentialRepository) RevokeByID(ctx context.Context, ownerUserID, id string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE s3_credentials SET status = $1, updated_at = NOW()
		WHERE owner_user_id = $2 AND id = $3
	`, s3credential.StatusRevoked, ownerUserID, id)
	if err != nil {
		return fmt.Errorf("revoke s3 credential: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check revoked s3 credential: %w", err)
	}
	if count == 0 {
		return s3credential.ErrNotFound
	}
	return nil
}

func (r *PostgresS3CredentialRepository) TouchByID(ctx context.Context, id string, usedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE s3_credentials SET last_used_at = $1, updated_at = NOW() WHERE id = $2`, usedAt, id)
	if err != nil {
		return fmt.Errorf("touch s3 credential: %w", err)
	}
	return nil
}

type s3CredentialScanner interface {
	Scan(dest ...any) error
}

func (r *PostgresS3CredentialRepository) scanRows(rows *sql.Rows) ([]*s3credential.Credential, error) {
	items := make([]*s3credential.Credential, 0)
	for rows.Next() {
		item, err := r.scanRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate s3 credentials: %w", err)
	}
	return items, nil
}

func (r *PostgresS3CredentialRepository) scanRow(scanner s3CredentialScanner) (*s3credential.Credential, error) {
	var ciphertext string
	var expiresAt, lastUsedAt sql.NullTime
	item := &s3credential.Credential{}
	if err := scanner.Scan(&item.ID, &item.OwnerUserID, &item.Name, &item.AccessKeyID,
		&ciphertext, &item.SecretKeyVersion, &item.Status, &expiresAt, &lastUsedAt,
		&item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	if r.secretBox == nil {
		return nil, crypto.ErrInvalidMasterKey
	}
	secret, err := r.secretBox.Open(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt s3 secret: %w", err)
	}
	item.Secret = secret
	if expiresAt.Valid {
		item.ExpiresAt = &expiresAt.Time
	}
	if lastUsedAt.Valid {
		item.LastUsedAt = &lastUsedAt.Time
	}
	return item, nil
}
