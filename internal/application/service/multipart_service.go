package service

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yeying-community/warehouse/internal/domain/quota"
	"github.com/yeying-community/warehouse/internal/domain/s3multipart"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
)

type MultipartService struct {
	root         string
	repo         repository.S3MultipartRepository
	objects      *ObjectService
	quotaService quota.Service
	uploadLocks  sync.Map
}

type stagingQuotaRepository interface {
	ReserveStaging(context.Context, string, int64, int64, int64) error
}

const (
	minMultipartPartSize   int64 = 5 * 1024 * 1024
	maxMultipartPartSize   int64 = 5 * 1024 * 1024 * 1024
	maxMultipartObjectSize int64 = 100 * 1024 * 1024 * 1024
)

func (s *MultipartService) SetObjectService(objects *ObjectService)    { s.objects = objects }
func (s *MultipartService) SetQuotaService(quotaService quota.Service) { s.quotaService = quotaService }
func (s *MultipartService) lockUpload(id string) func() {
	value, _ := s.uploadLocks.LoadOrStore(id, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

func NewMultipartService(root string, repo repository.S3MultipartRepository) *MultipartService {
	return &MultipartService{root: filepath.Clean(root), repo: repo}
}

func (s *MultipartService) Create(ctx context.Context, owner *user.User, bucket, key, contentType string) (*s3multipart.Upload, error) {
	if owner == nil || s.repo == nil {
		return nil, fmt.Errorf("multipart service is not configured")
	}
	id := uuid.NewString()
	staging := filepath.Join(s.root, ".s3-multipart", id)
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return nil, err
	}
	now := time.Now()
	item := &s3multipart.Upload{ID: id, OwnerUserID: owner.ID, Bucket: bucket, ObjectKey: key, StagingPath: staging, Status: s3multipart.StatusActive, ContentType: contentType, InitiatedAt: now, ExpiresAt: now.Add(24 * time.Hour), UpdatedAt: now}
	if err := s.repo.CreateUpload(ctx, item); err != nil {
		_ = os.RemoveAll(staging)
		return nil, err
	}
	return item, nil
}

func (s *MultipartService) UploadPart(ctx context.Context, owner *user.User, uploadID string, partNumber int, expectedChecksum string, src io.Reader) (*s3multipart.Part, error) {
	if owner == nil || s.repo == nil {
		return nil, fmt.Errorf("multipart service is not configured")
	}
	if partNumber < 1 || partNumber > 10000 {
		return nil, fmt.Errorf("invalid part number")
	}
	upload, err := s.repo.FindUpload(ctx, uploadID)
	if err != nil {
		return nil, err
	}
	if upload.OwnerUserID != owner.ID || upload.Status != s3multipart.StatusActive || time.Now().After(upload.ExpiresAt) {
		return nil, s3multipart.ErrNotFound
	}
	unlock := s.lockUpload(uploadID)
	defer unlock()
	partPath := filepath.Join(upload.StagingPath, fmt.Sprintf("part-%05d", partNumber))
	tmpPath := partPath + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	md5Hash := md5.New()
	shaHash := sha256.New()
	limited := io.LimitReader(src, maxMultipartPartSize+1)
	size, copyErr := io.Copy(io.MultiWriter(file, md5Hash, shaHash), limited)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return nil, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return nil, closeErr
	}
	if size > maxMultipartPartSize {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("multipart part exceeds 5 GiB limit")
	}
	if expectedChecksum != "" {
		decoded, err := base64.StdEncoding.DecodeString(expectedChecksum)
		if err != nil || hex.EncodeToString(decoded) != hex.EncodeToString(shaHash.Sum(nil)) {
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("checksum mismatch")
		}
	}
	existing, err := s.repo.ListParts(ctx, uploadID)
	if err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}
	var staged, oldSize int64
	for _, item := range existing {
		staged += item.Size
		if item.PartNumber == partNumber {
			oldSize = item.Size
		}
	}
	if s.quotaService != nil {
		quotaInfo, err := s.quotaService.GetQuota(ctx, owner.ID)
		if err != nil {
			_ = os.Remove(tmpPath)
			return nil, err
		}
		delta := size - oldSize
		if quotaRepo, ok := s.repo.(stagingQuotaRepository); ok {
			if err := quotaRepo.ReserveStaging(ctx, owner.ID, quotaInfo.Used, quotaInfo.Quota, delta); err != nil {
				_ = os.Remove(tmpPath)
				return nil, err
			}
		} else if quotaInfo.Available >= 0 && staged-oldSize+size > quotaInfo.Available {
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("multipart staging quota exceeded")
		}
	}
	if err := os.Rename(tmpPath, partPath); err != nil {
		if quotaRepo, ok := s.repo.(stagingQuotaRepository); ok && s.quotaService != nil {
			if info, quotaErr := s.quotaService.GetQuota(ctx, owner.ID); quotaErr == nil {
				_ = quotaRepo.ReserveStaging(ctx, owner.ID, info.Used, info.Quota, -(size - oldSize))
			}
		}
		_ = os.Remove(tmpPath)
		return nil, err
	}
	now := time.Now()
	part := &s3multipart.Part{UploadID: uploadID, PartNumber: partNumber, StagingPath: partPath, ETag: hex.EncodeToString(md5Hash.Sum(nil)), Size: size, ChecksumSHA256: hex.EncodeToString(shaHash.Sum(nil)), CreatedAt: now, UpdatedAt: now}
	if err := s.repo.UpsertPart(ctx, part); err != nil {
		if quotaRepo, ok := s.repo.(stagingQuotaRepository); ok && s.quotaService != nil {
			if info, quotaErr := s.quotaService.GetQuota(ctx, owner.ID); quotaErr == nil {
				_ = quotaRepo.ReserveStaging(ctx, owner.ID, info.Used, info.Quota, -(size - oldSize))
			}
		}
		_ = os.Remove(partPath)
		return nil, err
	}
	return part, nil
}

func (s *MultipartService) Abort(ctx context.Context, owner *user.User, uploadID string) error {
	if owner == nil || s.repo == nil {
		return fmt.Errorf("multipart service is not configured")
	}
	upload, err := s.repo.FindUpload(ctx, uploadID)
	if err != nil {
		return err
	}
	if upload.OwnerUserID != owner.ID {
		return s3multipart.ErrNotFound
	}
	if err := s.repo.SetUploadStatus(ctx, uploadID, s3multipart.StatusAborted, nil); err != nil {
		return err
	}
	s.releaseStaging(ctx, owner.ID, uploadID)
	return os.RemoveAll(upload.StagingPath)
}

func (s *MultipartService) releaseStaging(ctx context.Context, userID, uploadID string) {
	quotaRepo, ok := s.repo.(stagingQuotaRepository)
	if !ok || s.quotaService == nil {
		return
	}
	parts, err := s.repo.ListParts(ctx, uploadID)
	if err != nil {
		return
	}
	var total int64
	for _, part := range parts {
		total += part.Size
	}
	if info, err := s.quotaService.GetQuota(ctx, userID); err == nil {
		_ = quotaRepo.ReserveStaging(ctx, userID, info.Used, info.Quota, -total)
	}
}

func (s *MultipartService) CleanupExpired(ctx context.Context, now time.Time) (int, error) {
	if s.repo == nil {
		return 0, nil
	}
	items, err := s.repo.ListExpiredUploads(ctx, now)
	if err != nil {
		return 0, err
	}
	cleaned := 0
	for _, item := range items {
		if err := s.repo.SetUploadStatus(ctx, item.ID, s3multipart.StatusAborted, nil); err != nil {
			return cleaned, err
		}
		s.releaseStaging(ctx, item.OwnerUserID, item.ID)
		if err := os.RemoveAll(item.StagingPath); err != nil {
			return cleaned, err
		}
		cleaned++
	}
	return cleaned, nil
}

func (s *MultipartService) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		_, _ = s.CleanupExpired(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type CompletePart struct {
	PartNumber int
	ETag       string
}

func (s *MultipartService) Complete(ctx context.Context, owner *user.User, uploadID string, requested []CompletePart) (*ObjectInfo, error) {
	if owner == nil || s.repo == nil || s.objects == nil {
		return nil, fmt.Errorf("multipart service is not configured")
	}
	upload, err := s.repo.FindUpload(ctx, uploadID)
	if err != nil {
		return nil, err
	}
	if upload.OwnerUserID != owner.ID || upload.Status != s3multipart.StatusActive || time.Now().After(upload.ExpiresAt) {
		return nil, s3multipart.ErrNotFound
	}
	parts, err := s.repo.ListParts(ctx, uploadID)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 || len(parts) != len(requested) {
		return nil, fmt.Errorf("invalid multipart part list")
	}
	var totalSize int64
	for _, part := range parts {
		totalSize += part.Size
		if totalSize > maxMultipartObjectSize {
			return nil, fmt.Errorf("multipart object exceeds 100 GiB limit")
		}
	}
	files := make([]*os.File, 0, len(parts))
	readers := make([]io.Reader, 0, len(parts))
	defer func() {
		for _, file := range files {
			_ = file.Close()
		}
	}()
	for index, requestedPart := range requested {
		part := parts[index]
		if requestedPart.PartNumber != part.PartNumber || requestedPart.ETag != part.ETag {
			return nil, fmt.Errorf("multipart part etag or order mismatch")
		}
		if index < len(parts)-1 && part.Size < minMultipartPartSize {
			return nil, fmt.Errorf("non-final multipart part must be at least 5 MiB")
		}
		file, err := os.Open(part.StagingPath)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
		readers = append(readers, file)
	}
	info, err := s.objects.PutForUser(ctx, owner, upload.Bucket, upload.ObjectKey, io.MultiReader(readers...))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if err := s.repo.SetUploadStatus(ctx, uploadID, s3multipart.StatusCompleted, &now); err != nil {
		return nil, err
	}
	info.ETag = multipartETag(parts)
	s.releaseStaging(ctx, owner.ID, uploadID)
	if err := os.RemoveAll(upload.StagingPath); err != nil {
		return nil, err
	}
	return &info, nil
}

func multipartETag(parts []*s3multipart.Part) string {
	hash := md5.New()
	for _, part := range parts {
		digest, err := hex.DecodeString(part.ETag)
		if err != nil || len(digest) != md5.Size {
			return ""
		}
		_, _ = hash.Write(digest)
	}
	return hex.EncodeToString(hash.Sum(nil)) + "-" + strconv.Itoa(len(parts))
}
