package share

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrShareNotFound = errors.New("share item not found")
	ErrShareExpired  = errors.New("share item expired")
	ErrInvalidShare  = errors.New("invalid share")
)

// ShareItem 文件分享实体
type ShareItem struct {
	ID            string
	Token         string
	UserID        string
	Username      string
	Name          string
	Path          string
	ExpiresAt     *time.Time
	ViewCount     int64
	DownloadCount int64
	CreatedAt     time.Time
}

// NewShareItem 创建分享记录
func NewShareItem(userID, username, path, name string, expiresAt *time.Time) *ShareItem {
	now := time.Now()
	return &ShareItem{
		ID:            uuid.NewString(),
		Token:         uuid.NewString(),
		UserID:        userID,
		Username:      username,
		Name:          name,
		Path:          path,
		ExpiresAt:     expiresAt,
		ViewCount:     0,
		DownloadCount: 0,
		CreatedAt:     now,
	}
}

// IsExpired 判断是否过期
func (s *ShareItem) IsExpired() bool {
	if s.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*s.ExpiresAt)
}
