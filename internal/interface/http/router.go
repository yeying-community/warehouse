package http

import (
	"net/http"
	"strings"

	"github.com/yeying-community/webdav/internal/domain/auth"
	"github.com/yeying-community/webdav/internal/infrastructure/config"
	"github.com/yeying-community/webdav/internal/interface/http/handler"
	"github.com/yeying-community/webdav/internal/interface/http/middleware"
	"go.uber.org/zap"
)

// Router HTTP 路由器
type Router struct {
	config         *config.Config
	authenticators []auth.Authenticator
	healthHandler  *handler.HealthHandler
	web3Handler    *handler.Web3Handler
	webdavHandler  *handler.WebDAVHandler
	quotaHandler   *handler.QuotaHandler
	recycleHandler *handler.RecycleHandler
	logger         *zap.Logger
}

// NewRouter 创建路由器
func NewRouter(
	cfg *config.Config,
	authenticators []auth.Authenticator,
	healthHandler *handler.HealthHandler,
	web3Handler *handler.Web3Handler,
	webdavHandler *handler.WebDAVHandler,
	quotaHandler *handler.QuotaHandler,
	recycleHandler *handler.RecycleHandler,
	logger *zap.Logger,
) *Router {
	return &Router{
		config:         cfg,
		authenticators: authenticators,
		healthHandler:  healthHandler,
		web3Handler:    web3Handler,
		webdavHandler:  webdavHandler,
		quotaHandler:   quotaHandler,
		recycleHandler: recycleHandler,
		logger:         logger,
	}
}

// Setup 设置路由
func (r *Router) Setup() http.Handler {
	mux := http.NewServeMux()

	// 健康检查路由（无需认证）
	mux.HandleFunc("/api/v1/public/common/health/heartbeat", r.healthHandler.Handle)

	// Web3 认证路由（无需认证）
	mux.HandleFunc("/api/v1/public/common/auth/challenge", r.web3Handler.HandleChallenge)
	mux.HandleFunc("/api/v1/public/common/auth/verify", r.web3Handler.HandleVerify)
	mux.HandleFunc("/api/v1/public/common/auth/refreshToken", r.web3Handler.HandleRefresh)

	// API 路由（需要认证）
	mux.Handle("/api/v1/public/webdav/quota", r.createAuthenticatedHandler(http.HandlerFunc(r.quotaHandler.GetUserQuota)))

	// 回收站路由
	mux.Handle("/api/v1/public/webdav/recycle/list", r.createAuthenticatedHandler(http.HandlerFunc(r.recycleHandler.HandleList)))
	mux.Handle("/api/v1/public/webdav/recycle/recover", r.createAuthenticatedHandler(http.HandlerFunc(r.recycleHandler.HandleRecover)))
	mux.Handle("/api/v1/public/webdav/recycle/permanent", r.createAuthenticatedHandler(http.HandlerFunc(r.recycleHandler.HandleRemove)))

	// WebDAV 路由（需要认证）
	webdavPrefix := r.normalizePrefix(r.config.WebDAV.Prefix)
	mux.Handle(webdavPrefix, r.createAuthenticatedHandler(http.HandlerFunc(r.webdavHandler.Handle)))

	// 应用全局中间件
	handler := r.applyMiddlewares(mux)

	return handler
}

// createAuthenticatedHandler 创建需要认证的处理器
func (r *Router) createAuthenticatedHandler(handler http.Handler) http.Handler {
	// 应用认证中间件
	authMiddleware := middleware.NewAuthMiddleware(r.authenticators, true, r.logger)
	return authMiddleware.Handle(handler)
}

// applyMiddlewares 应用全局中间件
func (r *Router) applyMiddlewares(handler http.Handler) http.Handler {
	// 1. 恢复中间件（最外层）
	recoveryMiddleware := middleware.NewRecoveryMiddleware(r.logger)
	handler = recoveryMiddleware.Handle(handler)

	// 2. 日志中间件
	loggerMiddleware := middleware.NewLoggerMiddleware(r.logger, r.config.Security.BehindProxy)
	handler = loggerMiddleware.Handle(handler)

	// 3. CORS 中间件
	if r.config.CORS.Enabled {
		corsConfig := &middleware.CORSConfig{
			Enabled:        r.config.CORS.Enabled,
			Credentials:    r.config.CORS.Credentials,
			AllowedOrigins: r.config.CORS.AllowedOrigins,
			AllowedMethods: r.config.CORS.AllowedMethods,
			AllowedHeaders: r.config.CORS.AllowedHeaders,
			ExposedHeaders: r.config.CORS.ExposedHeaders,
		}
		corsMiddleware := middleware.NewCORSMiddleware(corsConfig)
		handler = corsMiddleware.Handle(handler)
	}

	return handler
}

// normalizePrefix 规范化前缀
func (r *Router) normalizePrefix(prefix string) string {
	if prefix == "" {
		return "/"
	}

	// 确保以 / 开头
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}

	// 确保以 / 结尾
	if !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}

	return prefix
}
