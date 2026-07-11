package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/s3multipart"
)

type S3MultipartRepository interface {
	CreateUpload(context.Context, *s3multipart.Upload) error
	FindUpload(context.Context, string) (*s3multipart.Upload, error)
	UpsertPart(context.Context, *s3multipart.Part) error
	ListParts(context.Context, string) ([]*s3multipart.Part, error)
	SetUploadStatus(context.Context, string, string, *time.Time) error
	DeleteUpload(context.Context, string) error
	ListExpiredUploads(context.Context, time.Time) ([]*s3multipart.Upload, error)
}

type PostgresS3MultipartRepository struct{ db *sql.DB }

func NewPostgresS3MultipartRepository(db *sql.DB) *PostgresS3MultipartRepository {
	return &PostgresS3MultipartRepository{db: db}
}

func (r *PostgresS3MultipartRepository) CreateUpload(ctx context.Context, item *s3multipart.Upload) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO s3_multipart_uploads (id, owner_user_id, bucket, object_key, staging_path, status, content_type, initiated_at, expires_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, item.ID, item.OwnerUserID, item.Bucket, item.ObjectKey, item.StagingPath, item.Status, item.ContentType, item.InitiatedAt, item.ExpiresAt, item.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create multipart upload: %w", err)
	}
	return nil
}

func (r *PostgresS3MultipartRepository) FindUpload(ctx context.Context, id string) (*s3multipart.Upload, error) {
	item := &s3multipart.Upload{}
	var completed sql.NullTime
	err := r.db.QueryRowContext(ctx, `SELECT id, owner_user_id, bucket, object_key, staging_path, status, COALESCE(content_type,''), initiated_at, expires_at, completed_at, updated_at FROM s3_multipart_uploads WHERE id = $1`, id).Scan(&item.ID, &item.OwnerUserID, &item.Bucket, &item.ObjectKey, &item.StagingPath, &item.Status, &item.ContentType, &item.InitiatedAt, &item.ExpiresAt, &completed, &item.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, s3multipart.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find multipart upload: %w", err)
	}
	if completed.Valid {
		item.CompletedAt = &completed.Time
	}
	return item, nil
}

func (r *PostgresS3MultipartRepository) UpsertPart(ctx context.Context, item *s3multipart.Part) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO s3_multipart_parts (upload_id, part_number, staging_path, etag, size, checksum_sha256, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (upload_id, part_number) DO UPDATE SET staging_path=EXCLUDED.staging_path, etag=EXCLUDED.etag, size=EXCLUDED.size, checksum_sha256=EXCLUDED.checksum_sha256, updated_at=EXCLUDED.updated_at`, item.UploadID, item.PartNumber, item.StagingPath, item.ETag, item.Size, item.ChecksumSHA256, item.CreatedAt, item.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert multipart part: %w", err)
	}
	return nil
}

func (r *PostgresS3MultipartRepository) ListParts(ctx context.Context, uploadID string) ([]*s3multipart.Part, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT upload_id, part_number, staging_path, etag, size, COALESCE(checksum_sha256,''), created_at, updated_at FROM s3_multipart_parts WHERE upload_id = $1 ORDER BY part_number`, uploadID)
	if err != nil {
		return nil, fmt.Errorf("list multipart parts: %w", err)
	}
	defer rows.Close()
	items := make([]*s3multipart.Part, 0)
	for rows.Next() {
		item := &s3multipart.Part{}
		if err := rows.Scan(&item.UploadID, &item.PartNumber, &item.StagingPath, &item.ETag, &item.Size, &item.ChecksumSHA256, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresS3MultipartRepository) SetUploadStatus(ctx context.Context, id, status string, completedAt *time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE s3_multipart_uploads SET status=$1, completed_at=$2, updated_at=NOW() WHERE id=$3`, status, completedAt, id)
	if err != nil {
		return fmt.Errorf("set multipart upload status: %w", err)
	}
	return nil
}

func (r *PostgresS3MultipartRepository) DeleteUpload(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM s3_multipart_uploads WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete multipart upload: %w", err)
	}
	return nil
}

func (r *PostgresS3MultipartRepository) ListExpiredUploads(ctx context.Context, now time.Time) ([]*s3multipart.Upload, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, owner_user_id, bucket, object_key, staging_path, status, COALESCE(content_type,''), initiated_at, expires_at, completed_at, updated_at FROM s3_multipart_uploads WHERE status = $1 AND expires_at <= $2 ORDER BY expires_at LIMIT 100`, s3multipart.StatusActive, now)
	if err != nil {
		return nil, fmt.Errorf("list expired multipart uploads: %w", err)
	}
	defer rows.Close()
	items := make([]*s3multipart.Upload, 0)
	for rows.Next() {
		item := &s3multipart.Upload{}
		var completed sql.NullTime
		if err := rows.Scan(&item.ID, &item.OwnerUserID, &item.Bucket, &item.ObjectKey, &item.StagingPath, &item.Status, &item.ContentType, &item.InitiatedAt, &item.ExpiresAt, &completed, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if completed.Valid {
			item.CompletedAt = &completed.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
