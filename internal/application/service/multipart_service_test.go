package service

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/s3multipart"
	"github.com/yeying-community/warehouse/internal/domain/user"
)

func TestMultipartETag(t *testing.T) {
	parts := []*s3multipart.Part{
		{ETag: "d41d8cd98f00b204e9800998ecf8427e"},
		{ETag: "0cc175b9c0f1b6a831c399e269772661"},
	}
	got := multipartETag(parts)
	if got != "68083ea43d0307eecfa2b4749f19df15-2" {
		t.Fatalf("multipart ETag = %q, want standard multipart ETag", got)
	}
	if got := multipartETag([]*s3multipart.Part{{ETag: "not-md5"}}); got != "" {
		t.Fatalf("invalid part ETag = %q, want empty", got)
	}
}

func TestMultipartCompletePersistsMetadata(t *testing.T) {
	root := t.TempDir()
	repo := &fakeMultipartRepo{
		uploads: make(map[string]*s3multipart.Upload),
		parts:   make(map[string]map[int]*s3multipart.Part),
	}
	metadataRepo := &testObjectMetadataRepo{items: make(map[string]ObjectMetadata)}
	objects := NewObjectService(root)
	objects.SetMetadataRepository(metadataRepo)
	service := NewMultipartService(root, repo)
	service.SetObjectService(objects)
	owner := &user.User{ID: "user-1", Username: "alice", Directory: "alice"}
	ctx := context.Background()

	upload, err := service.Create(ctx, owner, "personal", "archive.bin", "application/test")
	if err != nil {
		t.Fatalf("create upload: %v", err)
	}
	part1, err := service.UploadPart(ctx, owner, upload.ID, 1, "", bytes.NewReader(bytes.Repeat([]byte("a"), 5*1024*1024)))
	if err != nil {
		t.Fatalf("upload part 1: %v", err)
	}
	part2, err := service.UploadPart(ctx, owner, upload.ID, 2, "", strings.NewReader("tail"))
	if err != nil {
		t.Fatalf("upload part 2: %v", err)
	}

	info, err := service.Complete(ctx, owner, upload.ID, []CompletePart{
		{PartNumber: 1, ETag: part1.ETag},
		{PartNumber: 2, ETag: part2.ETag},
	})
	if err != nil {
		t.Fatalf("complete upload: %v", err)
	}
	if info.ETag == "" || !strings.Contains(info.ETag, "-2") {
		t.Fatalf("unexpected multipart etag: %+v", info)
	}

	stat, err := objects.Stat(ctx, owner.Directory, "personal", "archive.bin")
	if err != nil {
		t.Fatalf("stat object: %v", err)
	}
	if stat.ETag != info.ETag {
		t.Fatalf("stat etag = %q, want %q", stat.ETag, info.ETag)
	}
	if stat.ContentType != "application/test" {
		t.Fatalf("stat content type = %q, want application/test", stat.ContentType)
	}
}

type fakeMultipartRepo struct {
	uploads map[string]*s3multipart.Upload
	parts   map[string]map[int]*s3multipart.Part
}

func (r *fakeMultipartRepo) CreateUpload(_ context.Context, item *s3multipart.Upload) error {
	copy := *item
	r.uploads[item.ID] = &copy
	return nil
}

func (r *fakeMultipartRepo) FindUpload(_ context.Context, id string) (*s3multipart.Upload, error) {
	item, ok := r.uploads[id]
	if !ok {
		return nil, s3multipart.ErrNotFound
	}
	copy := *item
	return &copy, nil
}

func (r *fakeMultipartRepo) UpsertPart(_ context.Context, item *s3multipart.Part) error {
	if r.parts[item.UploadID] == nil {
		r.parts[item.UploadID] = make(map[int]*s3multipart.Part)
	}
	copy := *item
	r.parts[item.UploadID][item.PartNumber] = &copy
	return nil
}

func (r *fakeMultipartRepo) ListParts(_ context.Context, uploadID string) ([]*s3multipart.Part, error) {
	partMap := r.parts[uploadID]
	items := make([]*s3multipart.Part, 0, len(partMap))
	for number := 1; number <= len(partMap); number++ {
		if part, ok := partMap[number]; ok {
			copy := *part
			items = append(items, &copy)
		}
	}
	return items, nil
}

func (r *fakeMultipartRepo) SetUploadStatus(_ context.Context, id, status string, completedAt *time.Time) error {
	item, ok := r.uploads[id]
	if !ok {
		return s3multipart.ErrNotFound
	}
	item.Status = status
	item.CompletedAt = completedAt
	item.UpdatedAt = time.Now()
	return nil
}

func (r *fakeMultipartRepo) DeleteUpload(_ context.Context, id string) error {
	delete(r.uploads, id)
	delete(r.parts, id)
	return nil
}

func (r *fakeMultipartRepo) ListExpiredUploads(_ context.Context, now time.Time) ([]*s3multipart.Upload, error) {
	items := make([]*s3multipart.Upload, 0)
	for _, item := range r.uploads {
		if item.Status == s3multipart.StatusActive && !item.ExpiresAt.After(now) {
			copy := *item
			items = append(items, &copy)
		}
	}
	return items, nil
}
