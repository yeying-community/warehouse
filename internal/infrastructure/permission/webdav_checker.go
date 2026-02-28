package permission

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yeying-community/warehouse/internal/domain/permission"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"go.uber.org/zap"
	"golang.org/x/net/webdav"
)

// WebDAVChecker WebDAV 权限检查器
type WebDAVChecker struct {
	fileSystem webdav.FileSystem
	logger     *zap.Logger
}

// NewWebDAVChecker 创建 WebDAV 权限检查器
func NewWebDAVChecker(fileSystem webdav.FileSystem, logger *zap.Logger) *WebDAVChecker {
	return &WebDAVChecker{
		fileSystem: fileSystem,
		logger:     logger,
	}
}

// Check 检查权限
func (c *WebDAVChecker) Check(ctx context.Context, u *user.User, path string, op permission.Operation) error {
	// 规范化路径
	path = c.normalizePath(path)

	// 映射操作到权限
	perm := permission.MapOperationToPermission(op)

	// 检查用户是否有权限
	if !u.CanAccess(path, perm) {
		c.logger.Warn("permission denied",
			zap.String("username", u.Username),
			zap.String("path", path),
			zap.String("operation", string(op)),
			zap.String("permission", perm))

		return fmt.Errorf("permission denied: %s operation on %s", op, path)
	}

	// 对于创建和写入操作，检查父目录是否存在
	if op == permission.OperationCreate || op == permission.OperationWrite {
		if err := c.checkParentDirectory(path); err != nil {
			return err
		}
	}

	c.logger.Debug("permission granted",
		zap.String("username", u.Username),
		zap.String("path", path),
		zap.String("operation", string(op)))

	return nil
}

// checkParentDirectory 检查父目录是否存在
func (c *WebDAVChecker) checkParentDirectory(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "/" {
		return nil
	}

	// 检查父目录是否存在
	info, err := c.fileSystem.Stat(context.Background(), dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("parent directory does not exist: %s", dir)
		}
		return fmt.Errorf("failed to check parent directory: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("parent path is not a directory: %s", dir)
	}

	return nil
}

// normalizePath 规范化路径
func (c *WebDAVChecker) normalizePath(path string) string {
	// 移除前导和尾随斜杠
	path = strings.Trim(path, "/")

	// 确保以 / 开头
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	return path
}
