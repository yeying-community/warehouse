package shareuser

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrShareNotFound = errors.New("share not found")
	ErrShareExpired  = errors.New("share expired")
	ErrInvalidShare  = errors.New("invalid share")
)

const (
	AudienceTypeUser     = "user"
	AudienceTypeAllUsers = "all_users"
)

// ShareUserItem 定向分享实体
type ShareUserItem struct {
	ID                  string
	OwnerUserID         string
	OwnerUsername       string
	TargetUserID        string
	TargetWalletAddress string
	Name                string
	Path                string
	IsDir               bool
	Permissions         string
	AudienceType        string
	AudienceCount       int
	AllUsers            bool
	TargetCount         int
	ExpiresAt           *time.Time
	CreatedAt           time.Time
}

// NewShareUserItem 创建定向分享记录
func NewShareUserItem(ownerID, ownerUsername, targetID, targetWallet, path, name string, isDir bool, permissions string, expiresAt *time.Time) *ShareUserItem {
	now := time.Now()
	return &ShareUserItem{
		ID:                  uuid.NewString(),
		OwnerUserID:         ownerID,
		OwnerUsername:       ownerUsername,
		TargetUserID:        targetID,
		TargetWalletAddress: targetWallet,
		Name:                name,
		Path:                path,
		IsDir:               isDir,
		Permissions:         permissions,
		ExpiresAt:           expiresAt,
		CreatedAt:           now,
	}
}

// NewInternalShareItem 创建内部共享记录（支持多受众）
func NewInternalShareItem(ownerID, ownerUsername, path, name string, isDir bool, permissions string, expiresAt *time.Time) *ShareUserItem {
	now := time.Now()
	return &ShareUserItem{
		ID:            uuid.NewString(),
		OwnerUserID:   ownerID,
		OwnerUsername: ownerUsername,
		Name:          name,
		Path:          path,
		IsDir:         isDir,
		Permissions:   permissions,
		AudienceType:  AudienceTypeUser,
		AudienceCount: 0,
		AllUsers:      false,
		TargetCount:   0,
		ExpiresAt:     expiresAt,
		CreatedAt:     now,
	}
}

// IsExpired 判断分享是否过期
func (s *ShareUserItem) IsExpired() bool {
	if s.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*s.ExpiresAt)
}
