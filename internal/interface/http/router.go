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
	config             *config.Config
	authenticators     []auth.Authenticator
	healthHandler      *handler.HealthHandler
	web3Handler        *handler.Web3Handler
	webdavHandler      *handler.WebDAVHandler
	quotaHandler       *handler.QuotaHandler
	userHandler        *handler.UserHandler
	adminUserHandler   *handler.AdminUserHandler
	recycleHandler     *handler.RecycleHandler
	shareHandler       *handler.ShareHandler
	shareUserHandler   *handler.ShareUserHandler
	addressBookHandler *handler.AddressBookHandler
	logger             *zap.Logger
}

// NewRouter 创建路由器
func NewRouter(
	cfg *config.Config,
	authenticators []auth.Authenticator,
	healthHandler *handler.HealthHandler,
	web3Handler *handler.Web3Handler,
	webdavHandler *handler.WebDAVHandler,
	quotaHandler *handler.QuotaHandler,
	userHandler *handler.UserHandler,
	adminUserHandler *handler.AdminUserHandler,
	recycleHandler *handler.RecycleHandler,
	shareHandler *handler.ShareHandler,
	shareUserHandler *handler.ShareUserHandler,
	addressBookHandler *handler.AddressBookHandler,
	logger *zap.Logger,
) *Router {
	return &Router{
		config:             cfg,
		authenticators:     authenticators,
		healthHandler:      healthHandler,
		web3Handler:        web3Handler,
		webdavHandler:      webdavHandler,
		quotaHandler:       quotaHandler,
		userHandler:        userHandler,
		adminUserHandler:   adminUserHandler,
		recycleHandler:     recycleHandler,
		shareHandler:       shareHandler,
		shareUserHandler:   shareUserHandler,
		addressBookHandler: addressBookHandler,
		logger:             logger,
	}
}

// Setup 设置路由
func (r *Router) Setup() http.Handler {
	mux := http.NewServeMux()

	// 健康检查路由（无需认证）
	mux.HandleFunc("/api/v1/public/health/heartbeat", r.healthHandler.Handle)

	// Web3 认证路由（无需认证）
	mux.HandleFunc("/api/v1/public/auth/challenge", r.web3Handler.HandleChallenge)
	mux.HandleFunc("/api/v1/public/auth/verify", r.web3Handler.HandleVerify)
	mux.HandleFunc("/api/v1/public/auth/refresh", r.web3Handler.HandleRefresh)
	mux.HandleFunc("/api/v1/public/auth/logout", r.web3Handler.HandleLogout)
	mux.HandleFunc("/api/v1/public/auth/password/login", r.web3Handler.HandlePasswordLogin)

	// API 路由（需要认证）
	mux.Handle("/api/v1/public/webdav/quota", r.createAuthenticatedHandler(http.HandlerFunc(r.quotaHandler.GetUserQuota)))
	mux.Handle("/api/v1/public/webdav/user/info", r.createAuthenticatedHandler(http.HandlerFunc(r.userHandler.GetUserInfo)))
	mux.Handle("/api/v1/public/webdav/user/update", r.createAuthenticatedHandler(http.HandlerFunc(r.userHandler.UpdateUsername)))
	mux.Handle("/api/v1/public/webdav/user/password", r.createAuthenticatedHandler(http.HandlerFunc(r.userHandler.UpdatePassword)))

	// 管理员用户管理（需要认证 + 管理员权限）
	mux.Handle("/api/v1/public/admin/users/list", r.createAdminHandler(http.HandlerFunc(r.adminUserHandler.HandleList)))
	mux.Handle("/api/v1/public/admin/users/create", r.createAdminHandler(http.HandlerFunc(r.adminUserHandler.HandleCreate)))
	mux.Handle("/api/v1/public/admin/users/update", r.createAdminHandler(http.HandlerFunc(r.adminUserHandler.HandleUpdate)))
	mux.Handle("/api/v1/public/admin/users/delete", r.createAdminHandler(http.HandlerFunc(r.adminUserHandler.HandleDelete)))
	mux.Handle("/api/v1/public/admin/users/reset-password", r.createAdminHandler(http.HandlerFunc(r.adminUserHandler.HandleResetPassword)))

	// 回收站路由
	mux.Handle("/api/v1/public/webdav/recycle/list", r.createAuthenticatedHandler(http.HandlerFunc(r.recycleHandler.HandleList)))
	mux.Handle("/api/v1/public/webdav/recycle/recover", r.createAuthenticatedHandler(http.HandlerFunc(r.recycleHandler.HandleRecover)))
	mux.Handle("/api/v1/public/webdav/recycle/permanent", r.createAuthenticatedHandler(http.HandlerFunc(r.recycleHandler.HandleRemove)))
	mux.Handle("/api/v1/public/webdav/recycle/clear", r.createAuthenticatedHandler(http.HandlerFunc(r.recycleHandler.HandleClear)))

	// 好友地址簿
	mux.Handle("/api/v1/public/webdav/address/groups", r.createAuthenticatedHandler(http.HandlerFunc(r.addressBookHandler.HandleGroupList)))
	mux.Handle("/api/v1/public/webdav/address/groups/create", r.createAuthenticatedHandler(http.HandlerFunc(r.addressBookHandler.HandleGroupCreate)))
	mux.Handle("/api/v1/public/webdav/address/groups/update", r.createAuthenticatedHandler(http.HandlerFunc(r.addressBookHandler.HandleGroupUpdate)))
	mux.Handle("/api/v1/public/webdav/address/groups/delete", r.createAuthenticatedHandler(http.HandlerFunc(r.addressBookHandler.HandleGroupDelete)))
	mux.Handle("/api/v1/public/webdav/address/contacts", r.createAuthenticatedHandler(http.HandlerFunc(r.addressBookHandler.HandleContactList)))
	mux.Handle("/api/v1/public/webdav/address/contacts/create", r.createAuthenticatedHandler(http.HandlerFunc(r.addressBookHandler.HandleContactCreate)))
	mux.Handle("/api/v1/public/webdav/address/contacts/update", r.createAuthenticatedHandler(http.HandlerFunc(r.addressBookHandler.HandleContactUpdate)))
	mux.Handle("/api/v1/public/webdav/address/contacts/delete", r.createAuthenticatedHandler(http.HandlerFunc(r.addressBookHandler.HandleContactDelete)))

	// 分享路由
	mux.Handle("/api/v1/public/share/create", r.createAuthenticatedHandler(http.HandlerFunc(r.shareHandler.HandleCreate)))
	mux.Handle("/api/v1/public/share/list", r.createAuthenticatedHandler(http.HandlerFunc(r.shareHandler.HandleList)))
	mux.Handle("/api/v1/public/share/revoke", r.createAuthenticatedHandler(http.HandlerFunc(r.shareHandler.HandleRevoke)))
	mux.HandleFunc("/api/v1/public/share/", r.shareHandler.HandleAccess)

	// 定向分享路由（需要认证）
	mux.Handle("/api/v1/public/share/user/create", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleCreate)))
	mux.Handle("/api/v1/public/share/user/list", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleListMine)))
	mux.Handle("/api/v1/public/share/user/received", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleListReceived)))
	mux.Handle("/api/v1/public/share/user/revoke", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleRevoke)))
	mux.Handle("/api/v1/public/share/user/entries", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleEntries)))
	mux.Handle("/api/v1/public/share/user/download", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleDownload)))
	mux.Handle("/api/v1/public/share/user/upload", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleUpload)))
	mux.Handle("/api/v1/public/share/user/folder", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleCreateFolder)))
	mux.Handle("/api/v1/public/share/user/rename", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleRename)))
	mux.Handle("/api/v1/public/share/user/item", r.createAuthenticatedHandler(http.HandlerFunc(r.shareUserHandler.HandleDelete)))

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

// createAdminHandler 创建管理员处理器
func (r *Router) createAdminHandler(handler http.Handler) http.Handler {
	adminMiddleware := middleware.NewAdminMiddleware(r.config.Security.AdminAddresses, r.logger)
	authMiddleware := middleware.NewAuthMiddleware(r.authenticators, true, r.logger)
	return authMiddleware.Handle(adminMiddleware.Handle(handler))
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
