package s3multipart

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("multipart upload not found")
var ErrChecksumMismatch = errors.New("multipart checksum mismatch")

const (
	StatusActive    = "active"
	StatusCompleted = "completed"
	StatusAborted   = "aborted"
)

type Upload struct {
	ID          string
	OwnerUserID string
	Bucket      string
	ObjectKey   string
	StagingPath string
	Status      string
	ContentType string
	InitiatedAt time.Time
	ExpiresAt   time.Time
	CompletedAt *time.Time
	UpdatedAt   time.Time
}

type Part struct {
	UploadID       string
	PartNumber     int
	StagingPath    string
	ETag           string
	Size           int64
	ChecksumSHA256 string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
