package container

import (
	"context"
	"fmt"
	"strings"

	"github.com/yeying-community/webdav/internal/application/service"
	"github.com/yeying-community/webdav/internal/domain/auth"
	"github.com/yeying-community/webdav/internal/domain/quota"
	"github.com/yeying-community/webdav/internal/domain/user"
	infraAuth "github.com/yeying-community/webdav/internal/infrastructure/auth"
	"github.com/yeying-community/webdav/internal/infrastructure/config"
	"github.com/yeying-community/webdav/internal/infrastructure/database"
	"github.com/yeying-community/webdav/internal/infrastructure/logger"
	"github.com/yeying-community/webdav/internal/infrastructure/permission"
	"github.com/yeying-community/webdav/internal/infrastructure/repository"
	"github.com/yeying-community/webdav/internal/interface/http"
	"github.com/yeying-community/webdav/internal/interface/http/handler"
	"go.uber.org/zap"
	"golang.org/x/net/webdav"
)

// Container 依赖注入容器
type Container struct {
	Config *config.Config
	Logger *zap.Logger

	// Database
	DB *database.PostgresDB

	// Repositories
	UserRepository        user.Repository
	RecycleRepository     repository.RecycleRepository
	ShareRepository       repository.ShareRepository
	UserShareRepository   repository.UserShareRepository
	AddressBookRepository repository.AddressBookRepository

	// Services
	QuotaService       quota.Service
	WebDAVService      *service.WebDAVService
	RecycleService     *service.RecycleService
	ShareService       *service.ShareService
	ShareUserService   *service.ShareUserService
	AddressBookService *service.AddressBookService

	// Authenticators
	Authenticators []auth.Authenticator
	BasicAuth      *infraAuth.BasicAuthenticator
	Web3Auth       *infraAuth.Web3Authenticator

	// Handlers
	HealthHandler      *handler.HealthHandler
	Web3Handler        *handler.Web3Handler
	WebDAVHandler      *handler.WebDAVHandler
	QuotaHandler       *handler.QuotaHandler
	UserHandler        *handler.UserHandler
	AdminUserHandler   *handler.AdminUserHandler
	RecycleHandler     *handler.RecycleHandler
	ShareHandler       *handler.ShareHandler
	ShareUserHandler   *handler.ShareUserHandler
	AddressBookHandler *handler.AddressBookHandler

	// HTTP
	Router *http.Router
	Server *http.Server
}

// NewContainer 创建容器
func NewContainer(cfg *config.Config) (*Container, error) {
	c := &Container{
		Config:         cfg,
		Authenticators: make([]auth.Authenticator, 0),
	}

	// 初始化组件
	if err := c.initLogger(); err != nil {
		return nil, fmt.Errorf("failed to init logger: %w", err)
	}

	if err := c.initDatabase(); err != nil {
		return nil, fmt.Errorf("failed to init database: %w", err)
	}

	if err := c.initRepositories(); err != nil {
		return nil, fmt.Errorf("failed to init repositories: %w", err)
	}

	if err := c.initServices(); err != nil {
		return nil, fmt.Errorf("failed to init services: %w", err)
	}

	if err := c.initAuthenticators(); err != nil {
		return nil, fmt.Errorf("failed to init authenticators: %w", err)
	}

	if err := c.initHandlers(); err != nil {
		return nil, fmt.Errorf("failed to init handlers: %w", err)
	}

	if err := c.initHTTP(); err != nil {
		return nil, fmt.Errorf("failed to init http: %w", err)
	}

	return c, nil
}

// initLogger 初始化日志器
func (c *Container) initLogger() error {
	l, err := logger.NewLogger(c.Config.Log)
	if err != nil {
		return err
	}

	c.Logger = l
	c.Logger.Info("logger initialized",
		zap.String("level", c.Config.Log.Level),
		zap.String("format", c.Config.Log.Format))

	return nil
}

// initDatabase 初始化数据库
func (c *Container) initDatabase() error {
	// 仅支持 PostgreSQL
	if !(c.Config.Database.Type == "postgres" || c.Config.Database.Type == "postgresql") {
		return fmt.Errorf("unsupported database type %q: only postgres/postgresql is supported", c.Config.Database.Type)
	}

	db, err := database.NewPostgresDB(c.Config.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}
	c.DB = db

	// 执行数据库迁移
	ctx := context.Background()
	if err := c.DB.Migrate(ctx); err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	c.Logger.Info("database initialized",
		zap.String("type", "postgres"),
		zap.String("host", c.Config.Database.Host),
		zap.Int("port", c.Config.Database.Port))
	return nil
}

// initRepositories 初始化仓储
func (c *Container) initRepositories() error {
	if c.DB == nil {
		return fmt.Errorf("database not initialized")
	}

	// 用户仓储
	repo, err := repository.NewPostgresUserRepository(c.DB)
	if err != nil {
		return fmt.Errorf("failed to create postgres repository: %w", err)
	}
	c.UserRepository = repo

	// 回收站仓储
	c.RecycleRepository = repository.NewPostgresRecycleRepository(c.DB.DB)
	// 分享仓储
	c.ShareRepository = repository.NewPostgresShareRepository(c.DB.DB)
	// 定向分享仓储
	c.UserShareRepository = repository.NewPostgresUserShareRepository(c.DB.DB)
	// 地址簿仓储
	c.AddressBookRepository = repository.NewPostgresAddressBookRepository(c.DB.DB)

	c.Logger.Info("using PostgreSQL user repository")
	c.Logger.Info("repositories initialized")
	return nil
}

// initServices 初始化服务
func (c *Container) initServices() error {
	// 配额服务
	c.QuotaService = quota.NewService(c.UserRepository)

	// WebDAV 服务
	fileSystem := webdav.Dir(c.Config.WebDAV.Directory)
	permissionChecker := permission.NewWebDAVChecker(fileSystem, c.Logger)

	c.WebDAVService = service.NewWebDAVService(
		c.Config,
		permissionChecker,
		c.QuotaService,
		c.UserRepository,
		c.RecycleRepository,
		c.Logger,
	)

	// 回收站服务
	c.RecycleService = service.NewRecycleService(
		c.RecycleRepository,
		c.UserRepository,
		c.Config,
		c.Logger,
	)

	// 分享服务
	c.ShareService = service.NewShareService(
		c.ShareRepository,
		c.UserRepository,
		c.Config,
		c.Logger,
	)
	// 地址簿服务
	c.AddressBookService = service.NewAddressBookService(c.AddressBookRepository)
	// 定向分享服务
	c.ShareUserService = service.NewShareUserService(
		c.UserShareRepository,
		c.UserRepository,
		c.AddressBookService,
		c.Config,
		c.Logger,
	)

	c.Logger.Info("services initialized", zap.Bool("quota_enabled", true))

	return nil
}

// initAuthenticators 初始化认证器
func (c *Container) initAuthenticators() error {
	// Basic 认证器
	c.BasicAuth = infraAuth.NewBasicAuthenticator(
		c.UserRepository,
		c.Config.Security.NoPassword,
		c.Logger,
	)
	c.Authenticators = append(c.Authenticators, c.BasicAuth)

	// Web3 认证器
	ucanAudience := strings.TrimSpace(c.Config.Web3.UCAN.Audience)
	if ucanAudience == "" {
		ucanAudience = fmt.Sprintf("did:web:localhost:%d", c.Config.Server.Port)
	}
	ucanCaps := infraAuth.BuildRequiredUcanCaps(
		c.Config.Web3.UCAN.RequiredResource,
		c.Config.Web3.UCAN.RequiredAction,
	)
	ucanVerifier := infraAuth.NewUcanVerifier(
		c.Config.Web3.UCAN.Enabled,
		ucanAudience,
		ucanCaps,
		c.Logger,
	)
	c.Web3Auth = infraAuth.NewWeb3Authenticator(
		c.UserRepository,
		c.Config.Web3.JWTSecret,
		c.Config.Web3.TokenExpiration,
		c.Config.Web3.RefreshTokenExpiration,
		ucanVerifier,
		c.Logger,
		c.Config.Web3.AutoCreateOnUCAN,
	)
	c.Authenticators = append(c.Authenticators, c.Web3Auth)

	c.Logger.Info("authenticators initialized", zap.Int("count", len(c.Authenticators)))

	return nil
}

// initHandlers 初始化处理器
func (c *Container) initHandlers() error {
	// 健康检查处理器
	c.HealthHandler = handler.NewHealthHandler(c.Logger)

	// 创建配额处理器
	c.QuotaHandler = handler.NewQuotaHandler(c.QuotaService, c.Logger)
	// 用户信息处理器
	c.UserHandler = handler.NewUserHandler(c.Logger, c.UserRepository)
	// 管理员用户处理器
	c.AdminUserHandler = handler.NewAdminUserHandler(c.Logger, c.UserRepository)

	// Web3 处理器
	if c.Web3Auth != nil {
		c.Web3Handler = handler.NewWeb3Handler(
			c.Web3Auth,
			c.UserRepository,
			c.Logger,
			c.Config.Web3.AutoCreateOnChallenge,
		)
	}

	// WebDAV 处理器
	c.WebDAVHandler = handler.NewWebDAVHandler(
		c.WebDAVService,
		c.QuotaService,
		c.UserRepository,
		c.Logger,
	)

	// 回收站处理器
	c.RecycleHandler = handler.NewRecycleHandler(
		c.RecycleService,
		c.UserRepository,
		c.Logger,
	)

	// 分享处理器
	c.ShareHandler = handler.NewShareHandler(
		c.ShareService,
		c.Logger,
	)
	// 定向分享处理器
	c.ShareUserHandler = handler.NewShareUserHandler(
		c.ShareUserService,
		c.UserRepository,
		c.Logger,
	)
	// 地址簿处理器
	c.AddressBookHandler = handler.NewAddressBookHandler(
		c.AddressBookService,
		c.Logger,
	)

	c.Logger.Info("handlers initialized")

	return nil
}

// initHTTP 初始化 HTTP
func (c *Container) initHTTP() error {
	// 路由器
	c.Router = http.NewRouter(
		c.Config,
		c.Authenticators,
		c.HealthHandler,
		c.Web3Handler,
		c.WebDAVHandler,
		c.QuotaHandler,
		c.UserHandler,
		c.AdminUserHandler,
		c.RecycleHandler,
		c.ShareHandler,
		c.ShareUserHandler,
		c.AddressBookHandler,
		c.Logger,
	)

	// 服务器
	c.Server = http.NewServer(c.Config, c.Router, c.Logger)
	c.Logger.Info("http components initialized")

	return nil
}

// Close 关闭容器
func (c *Container) Close() error {
	if c.Logger != nil {
		c.Logger.Info("closing container")
	}

	// 关闭数据库连接
	if c.DB != nil {
		if err := c.DB.Close(); err != nil {
			c.Logger.Error("failed to close database", zap.Error(err))
		} else {
			c.Logger.Info("database connection closed")
		}
	}

	if c.Logger != nil {
		_ = c.Logger.Sync()
	}

	return nil
}
