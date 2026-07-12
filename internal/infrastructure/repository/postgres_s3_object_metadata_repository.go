package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type S3ObjectMetadata struct {
	UserDirectory string
	Bucket        string
	ObjectKey     string
	ETag          string
	ContentType   string
	UpdatedAt     time.Time
}

type S3ObjectMetadataRepository interface {
	Upsert(context.Context, *S3ObjectMetadata) error
	Find(context.Context, string, string, string) (*S3ObjectMetadata, error)
	Delete(context.Context, string, string, string) error
	ListByPrefix(context.Context, string, string, string) (map[string]S3ObjectMetadata, error)
}

type PostgresS3ObjectMetadataRepository struct {
	db *sql.DB
}

func NewPostgresS3ObjectMetadataRepository(db *sql.DB) *PostgresS3ObjectMetadataRepository {
	return &PostgresS3ObjectMetadataRepository{db: db}
}

func (r *PostgresS3ObjectMetadataRepository) Upsert(ctx context.Context, item *S3ObjectMetadata) error {
	if item == nil {
		return fmt.Errorf("s3 object metadata is nil")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO s3_object_metadata (user_directory, bucket, object_key, etag, content_type, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (user_directory, bucket, object_key)
		DO UPDATE SET etag = EXCLUDED.etag, content_type = EXCLUDED.content_type, updated_at = EXCLUDED.updated_at
	`, item.UserDirectory, item.Bucket, item.ObjectKey, item.ETag, item.ContentType, item.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert s3 object metadata: %w", err)
	}
	return nil
}

func (r *PostgresS3ObjectMetadataRepository) Find(ctx context.Context, userDirectory, bucket, objectKey string) (*S3ObjectMetadata, error) {
	item := &S3ObjectMetadata{}
	err := r.db.QueryRowContext(ctx, `
		SELECT user_directory, bucket, object_key, etag, content_type, updated_at
		FROM s3_object_metadata
		WHERE user_directory = $1 AND bucket = $2 AND object_key = $3
	`, userDirectory, bucket, objectKey).Scan(&item.UserDirectory, &item.Bucket, &item.ObjectKey, &item.ETag, &item.ContentType, &item.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find s3 object metadata: %w", err)
	}
	return item, nil
}

func (r *PostgresS3ObjectMetadataRepository) Delete(ctx context.Context, userDirectory, bucket, objectKey string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM s3_object_metadata
		WHERE user_directory = $1 AND bucket = $2 AND object_key = $3
	`, userDirectory, bucket, objectKey)
	if err != nil {
		return fmt.Errorf("delete s3 object metadata: %w", err)
	}
	return nil
}

func (r *PostgresS3ObjectMetadataRepository) ListByPrefix(ctx context.Context, userDirectory, bucket, prefix string) (map[string]S3ObjectMetadata, error) {
	query := `
		SELECT user_directory, bucket, object_key, etag, content_type, updated_at
		FROM s3_object_metadata
		WHERE user_directory = $1 AND bucket = $2`
	args := []any{userDirectory, bucket}
	if strings.TrimSpace(prefix) != "" {
		query += ` AND object_key LIKE $3`
		args = append(args, strings.TrimSpace(prefix)+"%")
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list s3 object metadata: %w", err)
	}
	defer rows.Close()
	items := make(map[string]S3ObjectMetadata)
	for rows.Next() {
		var item S3ObjectMetadata
		if err := rows.Scan(&item.UserDirectory, &item.Bucket, &item.ObjectKey, &item.ETag, &item.ContentType, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items[item.ObjectKey] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate s3 object metadata: %w", err)
	}
	return items, nil
}
