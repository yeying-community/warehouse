package handler

import (
	"net/http"
	"strings"

	"github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/quota"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

// WebDAVHandler WebDAV 处理器
// 职责：处理 HTTP 层面的逻辑，如请求验证、响应格式化、错误处理等
type WebDAVHandler struct {
	webdavService *service.WebDAVService
	logger        *zap.Logger
}

// NewWebDAVHandler 创建 WebDAV 处理器
func NewWebDAVHandler(
	webdavService *service.WebDAVService,
	quotaService quota.Service,
	userRepo user.Repository,
	logger *zap.Logger,
) *WebDAVHandler {
	return &WebDAVHandler{
		webdavService: webdavService,
		logger:        logger,
	}
}

// Handle 处理 WebDAV 请求
func (h *WebDAVHandler) Handle(w http.ResponseWriter, r *http.Request) {
	// 处理 OPTIONS 请求
	if r.Method == "OPTIONS" {
		h.handleOptions(w, r)
		return
	}

	// 为所有 WebDAV 请求添加必需的响应头
	w.Header().Set("DAV", "1, 2")
	w.Header().Set("MS-Author-Via", "DAV")

	// 从上下文获取用户信息（用于日志和监控）
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		h.logger.Error("user not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 记录访问日志
	h.logger.Debug("webdav request",
		zap.String("username", u.Username),
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.String("user_agent", r.UserAgent()),
		zap.String("remote_addr", r.RemoteAddr),
	)

	h.webdavService.ServeHTTP(w, r)
}

// handleOptions 处理 OPTIONS 请求
func (h *WebDAVHandler) handleOptions(w http.ResponseWriter, _ *http.Request) {
	// 允许的方法
	methods := []string{
		"OPTIONS",
		"GET", "HEAD", "POST", "PUT", "DELETE",
		"PROPFIND", "PROPPATCH",
		"MKCOL", "COPY", "MOVE",
		"LOCK", "UNLOCK",
	}

	// 设置响应头
	w.Header().Set("Allow", strings.Join(methods, ", "))
	w.Header().Set("DAV", "1, 2")
	w.Header().Set("MS-Author-Via", "DAV")
	w.Header().Set("Accept-Ranges", "bytes")

	// 返回 200 OK（不是 204）
	w.WriteHeader(http.StatusOK)
}
