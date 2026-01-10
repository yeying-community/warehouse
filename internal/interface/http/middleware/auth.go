package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/yeying-community/webdav/internal/domain/auth"
	"github.com/yeying-community/webdav/internal/domain/user"
	"go.uber.org/zap"
)

// contextKey 上下文键类型
type contextKey string

const (
	// UserContextKey 用户上下文键
	UserContextKey contextKey = "user"
)

// AuthMiddleware 认证中间件
type AuthMiddleware struct {
	authenticators []auth.Authenticator
	required       bool
	logger         *zap.Logger
}

// NewAuthMiddleware 创建认证中间件
func NewAuthMiddleware(authenticators []auth.Authenticator, required bool, logger *zap.Logger) *AuthMiddleware {
	return &AuthMiddleware{
		authenticators: authenticators,
		required:       required,
		logger:         logger,
	}
}

// Handle 处理认证
func (m *AuthMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// OPTIONS 请求不需要认证
		if r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}

		// 尝试从请求中提取凭证
		credentials := m.extractCredentials(r)
		if credentials == nil {
			if m.required {
				m.logger.Debug("no credentials provided")
				m.sendUnauthorized(w, "Authentication required")
				return
			}
			// 不需要认证，继续
			next.ServeHTTP(w, r)
			return
		}

		// 尝试使用所有认证器进行认证
		u, err := m.authenticate(ctx, credentials)
		if err != nil {
			m.logger.Warn("authentication failed", zap.Error(err))
			m.sendUnauthorized(w, "Authentication failed")
			return
		}

		// 将用户信息放入上下文
		ctx = context.WithValue(ctx, UserContextKey, u)
		r = r.WithContext(ctx)

		m.logger.Debug("user authenticated", zap.String("username", u.Username))

		next.ServeHTTP(w, r)
	})
}

// authenticate 认证用户
func (m *AuthMiddleware) authenticate(ctx context.Context, credentials interface{}) (*user.User, error) {
	// 遍历所有认证器
	for _, authenticator := range m.authenticators {
		// 检查是否可以处理该凭证
		if !authenticator.CanHandle(credentials) {
			continue
		}

		m.logger.Debug("trying authenticator",
			zap.String("authenticator", authenticator.Name()))

		// 尝试认证
		u, err := authenticator.Authenticate(ctx, credentials)
		if err != nil {
			m.logger.Debug("authentication failed",
				zap.String("authenticator", authenticator.Name()),
				zap.Error(err))
			return nil, err
		}

		m.logger.Info("authentication successful",
			zap.String("authenticator", authenticator.Name()),
			zap.String("username", u.Username))

		return u, nil
	}

	return nil, auth.ErrInvalidCredentials
}

// extractCredentials 提取凭证
func (m *AuthMiddleware) extractCredentials(r *http.Request) interface{} {
	// 1. 尝试 Bearer Token (Authorization header)
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		return &auth.BearerCredentials{Token: token}
	}

	// 2. 尝试从 Cookie 读取 token
	if cookie, err := r.Cookie("authToken"); err == nil {
		return &auth.BearerCredentials{Token: cookie.Value}
	}

	// 3. 尝试 Basic Auth
	username, password, ok := r.BasicAuth()
	if ok {
		return &auth.BasicCredentials{
			Username: username,
			Password: password,
		}
	}

	return nil
}

// sendUnauthorized 发送未授权响应
func (m *AuthMiddleware) sendUnauthorized(w http.ResponseWriter, message string) {
	// 不设置 WWW-Authenticate，避免浏览器弹出 Basic Auth 对话框
	// 前端会通过 Bearer Token 或其他方式处理认证
	http.Error(w, message, http.StatusUnauthorized)
}

// GetUserFromContext 从上下文获取用户
func GetUserFromContext(ctx context.Context) (*user.User, bool) {
	u, ok := ctx.Value(UserContextKey).(*user.User)
	return u, ok
}
