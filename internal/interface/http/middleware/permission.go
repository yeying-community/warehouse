package middleware

import (
	"net/http"

	"github.com/yeying-community/warehouse/internal/domain/permission"
	"go.uber.org/zap"
)

// PermissionMiddleware 权限中间件
type PermissionMiddleware struct {
	checker permission.Checker
	logger  *zap.Logger
}

// NewPermissionMiddleware 创建权限中间件
func NewPermissionMiddleware(checker permission.Checker, logger *zap.Logger) *PermissionMiddleware {
	return &PermissionMiddleware{
		checker: checker,
		logger:  logger,
	}
}

// Handle 处理权限检查
func (m *PermissionMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// 从上下文获取用户
		u, ok := GetUserFromContext(ctx)
		if !ok {
			m.logger.Error("user not found in context")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// 映射 HTTP 方法到操作
		operation := permission.MapHTTPMethodToOperation(r.Method)

		// 检查权限
		if err := m.checker.Check(ctx, u, r.URL.Path, operation); err != nil {
			m.logger.Warn("permission denied",
				zap.String("username", u.Username),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.String("operation", string(operation)),
				zap.Error(err))
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		m.logger.Debug("permission granted",
			zap.String("username", u.Username),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("operation", string(operation)))

		next.ServeHTTP(w, r)
	})
}
