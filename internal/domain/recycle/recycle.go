package recycle

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrRecycleItemNotFound = errors.New("recycle item not found")
	ErrInvalidHash         = errors.New("invalid hash")
)

// RecycleItem 回收站项目实体
type RecycleItem struct {
	ID        string    // 内部 ID
	Hash      string    // 文件内容哈希（唯一标识）
	UserID    string    // 所属用户 ID
	Username  string    // 所属用户名
	Directory string    // 所在目录名
	Name      string    // 文件名
	Path      string    // 相对路径（相对于目录根）
	Size      int64     // 文件大小（字节）
	DeletedAt time.Time // 删除时间
	CreatedAt time.Time // 创建时间
}

// NewRecycleItem 创建新的回收站项目
func NewRecycleItem(userID, username, directory, name, path string, size int64) *RecycleItem {
	now := time.Now()
	return &RecycleItem{
		ID:        generateID(),
		Hash:      generateHash(),
		UserID:    userID,
		Username:  username,
		Directory: directory,
		Name:      name,
		Path:      path,
		Size:      size,
		DeletedAt: now,
		CreatedAt: now,
	}
}

// GetOriginalPath 获取原始完整路径
func (r *RecycleItem) GetOriginalPath() string {
	// Path 已经是相对于目录的路径，直接返回
	return r.Path
}

// generateID 生成内部 ID
func generateID() string {
	return uuid.NewString()
}

// generateHash 生成文件哈希（简化版本，实际应计算文件内容哈希）
func generateHash() string {
	return uuid.NewString()
}