package accesskey

import (
	"errors"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound         = errors.New("access key not found")
	ErrInvalidName      = errors.New("access key name is required")
	ErrDuplicateName    = errors.New("access key name already exists")
	ErrInvalidRootPath  = errors.New("invalid access key root path")
	ErrInvalidSecret    = errors.New("invalid access key secret")
	ErrInvalidStatus    = errors.New("invalid access key status")
	ErrInvalidOwner     = errors.New("invalid access key owner")
	ErrInvalidPerms     = errors.New("invalid access key permissions")
	ErrAlreadyRevoked   = errors.New("access key already revoked")
	ErrAccessKeyExpired = errors.New("access key expired")
)

const (
	StatusActive  = "active"
	StatusRevoked = "revoked"
)

type WebDAVAccessKey struct {
	ID          string
	OwnerUserID string
	Name        string
	KeyID       string
	SecretHash  string
	RootPath    string
	Permissions string
	Status      string
	ExpiresAt   *time.Time
	LastUsedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func New(ownerUserID, name, keyID, secretHash, rootPath, permissions string, expiresAt *time.Time) (*WebDAVAccessKey, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	name = strings.TrimSpace(name)
	keyID = strings.TrimSpace(keyID)
	secretHash = strings.TrimSpace(secretHash)
	permissions = strings.ToUpper(strings.TrimSpace(permissions))
	rootPath, err := NormalizeRootPath(rootPath)
	if err != nil {
		return nil, err
	}
	if ownerUserID == "" {
		return nil, ErrInvalidOwner
	}
	if name == "" {
		return nil, ErrInvalidName
	}
	if keyID == "" {
		return nil, ErrInvalidSecret
	}
	if secretHash == "" {
		return nil, ErrInvalidSecret
	}
	if permissions == "" {
		return nil, ErrInvalidPerms
	}

	now := time.Now()
	return &WebDAVAccessKey{
		ID:          uuid.NewString(),
		OwnerUserID: ownerUserID,
		Name:        name,
		KeyID:       keyID,
		SecretHash:  secretHash,
		RootPath:    rootPath,
		Permissions: permissions,
		Status:      StatusActive,
		ExpiresAt:   expiresAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

func NormalizeRootPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/", nil
	}
	clean := path.Clean("/" + strings.TrimLeft(raw, "/"))
	if clean == "." {
		return "/", nil
	}
	if !strings.HasPrefix(clean, "/") {
		return "", ErrInvalidRootPath
	}
	return clean, nil
}

func (k *WebDAVAccessKey) IsExpired(now time.Time) bool {
	if k == nil || k.ExpiresAt == nil {
		return false
	}
	return now.After(*k.ExpiresAt)
}
