package permission

import (
	"context"
	"github.com/yeying-community/warehouse/internal/domain/user"
)

// Checker 权限检查器接口
type Checker interface {
	// Check 检查用户是否有权限执行操作
	Check(ctx context.Context, user *user.User, path string, operation Operation) error
}

