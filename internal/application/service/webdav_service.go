package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yeying-community/webdav/internal/domain/auth"
	"github.com/yeying-community/webdav/internal/domain/permission"
	"github.com/yeying-community/webdav/internal/domain/quota"
	"github.com/yeying-community/webdav/internal/domain/recycle"
	"github.com/yeying-community/webdav/internal/domain/user"
	"github.com/yeying-community/webdav/internal/infrastructure/config"
	"github.com/yeying-community/webdav/internal/infrastructure/repository"
	webdavfs "github.com/yeying-community/webdav/internal/infrastructure/webdav"
	"github.com/yeying-community/webdav/internal/interface/http/middleware"
	"go.uber.org/zap"
	"golang.org/x/net/webdav"
)

// WebDAVService WebDAV 服务
type WebDAVService struct {
	config          *config.Config
	permissionCheck permission.Checker
	quotaService    quota.Service
	userRepo        user.Repository
	recycleRepo     repository.RecycleRepository
	logger          *zap.Logger
	lockSystem      webdav.LockSystem
	recycleDir      string // 回收站目录
}

// statusRecorder 记录响应状态码
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// NewWebDAVService 创建 WebDAV 服务
func NewWebDAVService(
	cfg *config.Config,
	permissionCheck permission.Checker,
	quotaService quota.Service,
	userRepo user.Repository,
	recycleRepo repository.RecycleRepository,
	logger *zap.Logger,
) *WebDAVService {
	recycleDir := filepath.Join(cfg.WebDAV.Directory, ".recycle")
	return &WebDAVService{
		config:          cfg,
		permissionCheck: permissionCheck,
		quotaService:    quotaService,
		userRepo:        userRepo,
		recycleRepo:     recycleRepo,
		logger:          logger,
		lockSystem:      webdav.NewMemLS(),
		recycleDir:      recycleDir,
	}
}

// ServeHTTP 处理 WebDAV 请求
func (s *WebDAVService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 从上下文获取用户
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		s.logger.Error("user not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if isIgnoredWebDAVPath(r.URL.Path) {
		if r.Body != nil {
			_, _ = io.Copy(io.Discard, r.Body)
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, "PROPFIND":
			http.Error(w, "Not Found", http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
		return
	}

	// 获取用户目录
	userDir := s.getUserDirectory(u)
	s.logger.Debug("user directory", zap.String("username", u.Username), zap.String("directory", userDir))

	// 确保目录存在
	if err := s.ensureDirectory(userDir); err != nil {
		s.logger.Error("failed to ensure directory",
			zap.String("directory", userDir),
			zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 确保 app scope 根目录存在（例如 /apps）
	if err := s.ensureAppScopeRoot(userDir); err != nil {
		s.logger.Error("failed to ensure app scope root",
			zap.String("directory", userDir),
			zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 规范化 MOVE/COPY 的 Destination 头，避免编码或代理导致的路径异常
	if r.Method == "MOVE" || r.Method == "COPY" {
		normalizeDestinationHeader(r)
	}

	// UCAN app scope 校验
	if err := s.checkAppScope(r.Context(), r); err != nil {
		s.logger.Warn("ucan app scope denied",
			zap.String("username", u.Username),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("destination", r.Header.Get("Destination")),
			zap.Error(err),
		)
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// 检查权限
	if err := s.checkPermission(r.Context(), u, r); err != nil {
		s.logger.Warn("permission denied",
			zap.String("username", u.Username),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Error(err))
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// 对于上传操作，检查配额
	if isUploadMethod(r.Method) {
		if err := s.checkQuota(r.Context(), u, r); err != nil {
			s.logger.Warn("quota exceeded",
				zap.String("username", u.Username),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Error(err))
			http.Error(w, "Insufficient Storage", http.StatusInsufficientStorage)
			return
		}
	}

	// 创建 WebDAV 处理器（使用自定义的 Unicode FileSystem）
	unicodeFS := webdavfs.NewUnicodeFileSystem(userDir)
	handler := &webdav.Handler{
		Prefix:     s.config.WebDAV.Prefix,
		FileSystem: unicodeFS,
		LockSystem: s.lockSystem,
		Logger:     s.createLogger(u.Username),
	}

	// 设置响应头
	if s.config.WebDAV.NoSniff {
		w.Header().Set("X-Content-Type-Options", "nosniff")
	}

	// 处理请求
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

	// 处理 DELETE 请求：将文件移动到回收站
	if r.Method == http.MethodDelete {
		s.handleDeleteWithRecycle(w, r, u, userDir, handler, rec)
		return
	}

	handler.ServeHTTP(rec, r)

	// 写操作成功后刷新 used_space
	if isMutatingMethod(r.Method) && rec.status >= 200 && rec.status < 300 {
		used, err := s.quotaService.CalculateUsedSpace(r.Context(), userDir)
		if err != nil {
			s.logger.Error("failed to calculate used space",
				zap.String("username", u.Username),
				zap.String("directory", userDir),
				zap.Error(err))
			return
		}
		if err := s.userRepo.UpdateUsedSpace(r.Context(), u.Username, used); err != nil {
			s.logger.Error("failed to update used space in repo",
				zap.String("username", u.Username),
				zap.Int64("used_space", used),
				zap.Error(err))
			return
		}
		u.UpdateUsedSpace(used)
		s.logger.Debug("used space updated",
			zap.String("username", u.Username),
			zap.Int64("used_space", used))
	}
}

func isIgnoredWebDAVPath(rawPath string) bool {
	if rawPath == "" || rawPath == "/" {
		return false
	}
	cleaned := strings.TrimSuffix(rawPath, "/")
	base := path.Base(cleaned)
	return webdavfs.IsIgnoredName(base)
}

// handleDeleteWithRecycle 处理删除请求（带回收站功能）
func (s *WebDAVService) handleDeleteWithRecycle(w http.ResponseWriter, r *http.Request, u *user.User, userDir string, handler *webdav.Handler, rec *statusRecorder) {
	// 获取文件相对路径（剥离 WebDAV 前缀）
	normalizedPath := s.normalizeWebdavRequestPath(r.URL.Path)
	filePath := strings.TrimPrefix(normalizedPath, "/")

	// 获取文件的完整路径
	fullPath := filepath.Join(userDir, filePath)

	// 检查是否存在
	if _, err := os.Stat(fullPath); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		s.logger.Error("failed to stat file", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 文件/目录移动到回收站目录
	if err := s.moveToRecycle(r.Context(), u, filePath, fullPath); err != nil {
		s.logger.Error("failed to move file to recycle", zap.Error(err))
		// 如果移动失败，直接删除
		handler.ServeHTTP(rec, r)
		return
	}

	// 更新配额
	used, err := s.quotaService.CalculateUsedSpace(r.Context(), userDir)
	if err != nil {
		s.logger.Error("failed to calculate used space", zap.Error(err))
		return
	}
	if err := s.userRepo.UpdateUsedSpace(r.Context(), u.Username, used); err != nil {
		s.logger.Error("failed to update used space", zap.Error(err))
		return
	}
	u.UpdateUsedSpace(used)

	// 返回成功
	w.WriteHeader(http.StatusOK)
}

// moveToRecycle 将文件移动到回收站并保存记录
func (s *WebDAVService) moveToRecycle(ctx context.Context, u *user.User, relativePath, fullPath string) error {
	// 获取文件信息
	info, err := os.Stat(fullPath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}
	fileSize := info.Size()
	if info.IsDir() {
		fileSize = 0
	}

	// 确保回收站目录存在
	if err := os.MkdirAll(s.recycleDir, 0755); err != nil {
		return fmt.Errorf("failed to create recycle dir: %w", err)
	}

	// 获取文件名和目录
	cleanRelative := strings.TrimSuffix(relativePath, "/")
	cleanRelative = filepath.Clean(filepath.FromSlash(cleanRelative))
	if cleanRelative == "." {
		cleanRelative = ""
	}
	fileName := filepath.Base(cleanRelative)
	dirName := filepath.Dir(cleanRelative)
	if dirName == "." {
		dirName = u.Directory
		if dirName == "" {
			dirName = u.Username
		}
	}

	// 创建回收站记录（先生成 hash，便于文件命名）
	item := recycle.NewRecycleItem(u.ID, u.Username, dirName, fileName, cleanRelative, fileSize)

	// 生成唯一的回收站文件名：{hash}_{原文件名}
	recycleFileName := fmt.Sprintf("%s_%s", item.Hash, fileName)
	recyclePath := filepath.Join(s.recycleDir, recycleFileName)

	// 移动文件
	if err := os.Rename(fullPath, recyclePath); err != nil {
		return fmt.Errorf("failed to move file to recycle: %w", err)
	}

	// 创建回收站记录并保存到数据库
	if err := s.recycleRepo.Create(ctx, item); err != nil {
		s.logger.Error("failed to save recycle item", zap.Error(err))
		// 不返回错误，因为文件已经移动了
	}

	s.logger.Info("file moved to recycle",
		zap.String("username", u.Username),
		zap.String("original_path", relativePath),
		zap.String("recycle_path", recyclePath),
		zap.String("hash", item.Hash),
	)

	return nil
}

// isUploadMethod 判断是否为上传方法
func isUploadMethod(method string) bool {
	return method == "PUT" || method == "POST" || method == "MKCOL"
}

// isMutatingMethod 判断是否为可能改变存储的 WebDAV 方法
func isMutatingMethod(method string) bool {
	switch method {
	case "PUT", "POST", "MKCOL", "DELETE", "MOVE", "COPY":
		return true
	default:
		return false
	}
}

// checkQuota 检查配额
func (s *WebDAVService) checkQuota(ctx context.Context, u *user.User, r *http.Request) error {
	// 如果用户没有配额限制，跳过检查
	if u.Quota <= 0 {
		return nil
	}

	// 获取文件大小
	var fileSize int64
	if r.Method == "PUT" || r.Method == "POST" {
		// 从 Content-Length 头获取大小
		if contentLength := r.Header.Get("Content-Length"); contentLength != "" {
			size, err := strconv.ParseInt(contentLength, 10, 64)
			if err == nil {
				fileSize = size
			}
		}

		// 如果没有 Content-Length，尝试读取 body
		if fileSize == 0 && r.Body != nil {
			// 注意：这会消耗 body，需要重新设置
			body, err := io.ReadAll(r.Body)
			if err != nil {
				return fmt.Errorf("failed to read body: %w", err)
			}
			fileSize = int64(len(body))
			// 重新设置 body
			r.Body = io.NopCloser(io.NewSectionReader(
				io.NewSectionReader(
					&bodyReader{data: body},
					0,
					int64(len(body)),
				),
				0,
				int64(len(body)),
			))
		}
	}

	// 检查是否超过配额
	if err := s.quotaService.CheckQuota(ctx, u, fileSize); err != nil {
		return err
	}

	return nil
}

// bodyReader 用于重新读取 body
type bodyReader struct {
	data []byte
	pos  int
}

func (b *bodyReader) Read(p []byte) (n int, err error) {
	if b.pos >= len(b.data) {
		return 0, io.EOF
	}
	n = copy(p, b.data[b.pos:])
	b.pos += n
	return n, nil
}

func (b *bodyReader) ReadAt(p []byte, off int64) (n int, err error) {
	if off >= int64(len(b.data)) {
		return 0, io.EOF
	}
	n = copy(p, b.data[off:])
	if n < len(p) {
		err = io.EOF
	}
	return n, err
}

// normalizeDestinationHeader 规范化 Destination 头，处理编码和代理前缀差异
func normalizeDestinationHeader(r *http.Request) {
	dest := r.Header.Get("Destination")
	if dest == "" {
		return
	}

	u, err := url.Parse(dest)
	if err != nil {
		return
	}

	if u.Path == "" {
		return
	}

	if decoded, err := url.PathUnescape(u.Path); err == nil {
		u.Path = decoded
	}

	// 强制使用路径形式，避免代理导致的 host 不匹配
	path := "/" + strings.TrimLeft(u.Path, "/")
	r.Header.Set("Destination", path)
}

// createLogger 创建 WebDAV 日志记录器
func (s *WebDAVService) createLogger(username string) func(*http.Request, error) {
	return func(r *http.Request, err error) {
		if err == nil {
			return
		}

		// 分类错误
		fields := []zap.Field{
			zap.String("username", username),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
		}

		// 判断错误类型
		if isNoSuchLockError(err) {
			// Finder/客户端常见的无锁解锁请求，降级为 DEBUG
			s.logger.Debug("webdav lock not found",
				append(fields, zap.String("error", err.Error()))...)
			return
		}
		if isNotFoundError(err) {
			// 文件不存在 - WARN 级别，不打印堆栈
			s.logger.Warn("resource not found",
				append(fields, zap.String("error", err.Error()))...)
		} else if isPermissionError(err) {
			// 权限错误 - WARN 级别
			s.logger.Warn("permission denied",
				append(fields, zap.String("error", err.Error()))...)
		} else if isExistsError(err) {
			// 文件已存在 - WARN 级别
			s.logger.Warn("resource already exists",
				append(fields, zap.String("error", err.Error()))...)
		} else if isClientError(err) {
			// 客户端错误 - INFO 级别
			s.logger.Info("client error",
				append(fields, zap.String("error", err.Error()))...)
		} else {
			// 系统错误 - ERROR 级别，打印堆栈
			s.logger.Error("webdav error", append(fields, zap.Error(err))...)
		}
	}
}

// isNotFoundError 判断是否为文件不存在错误
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	// 检查是否为 os.ErrNotExist
	if errors.Is(err, os.ErrNotExist) {
		return true
	}

	// 检查错误消息
	errMsg := err.Error()
	return contains(errMsg, "no such file") ||
		contains(errMsg, "not found") ||
		contains(errMsg, "does not exist")
}

// isNoSuchLockError 判断是否为不存在的锁错误
func isNoSuchLockError(err error) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), "no such lock")
}

// isPermissionError 判断是否为权限错误
func isPermissionError(err error) bool {
	if err == nil {
		return false
	}

	// 检查是否为 os.ErrPermission
	if errors.Is(err, os.ErrPermission) {
		return true
	}

	// 检查错误消息
	errMsg := err.Error()
	return contains(errMsg, "permission denied") ||
		contains(errMsg, "access denied") ||
		contains(errMsg, "forbidden")
}

// isClientError 判断是否为客户端错误
func isClientError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()
	return contains(errMsg, "invalid") ||
		contains(errMsg, "bad request") ||
		contains(errMsg, "malformed")
}

// isExistsError 判断是否为文件/目录已存在错误
func isExistsError(err error) bool {
	if err == nil {
		return false
	}

	// 检查是否为 os.ErrExist
	if errors.Is(err, os.ErrExist) {
		return true
	}

	// 检查错误消息
	errMsg := err.Error()
	return contains(errMsg, "file exists") ||
		contains(errMsg, "already exists") ||
		contains(errMsg, "cannot create")
}

// contains 检查字符串是否包含子串（不区分大小写）
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || containsCaseInsensitive(s, substr))
}

// containsCaseInsensitive 不区分大小写的字符串包含检查
func containsCaseInsensitive(s, substr string) bool {
	s = toLower(s)
	substr = toLower(substr)
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// toLower 转换为小写
func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// getUserDirectory 获取用户目录
func (s *WebDAVService) getUserDirectory(u *user.User) string {
	// 如果用户有自定义目录，使用用户目录
	if u.Directory != "" {
		// 如果是绝对路径，直接使用
		if filepath.IsAbs(u.Directory) {
			return u.Directory
		}
		// 否则拼接到基础目录
		return filepath.Join(s.config.WebDAV.Directory, u.Directory)
	}

	// 使用基础目录
	return s.config.WebDAV.Directory
}

// ensureDirectory 确保目录存在
func (s *WebDAVService) ensureDirectory(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// 创建目录
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
			s.logger.Info("directory created", zap.String("directory", dir))
			return nil
		}
		return fmt.Errorf("failed to stat directory: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", dir)
	}

	return nil
}

func (s *WebDAVService) ensureAppScopeRoot(userDir string) error {
	if s == nil || s.config == nil {
		return nil
	}
	prefix := strings.TrimSpace(s.config.Web3.UCAN.AppScope.PathPrefix)
	if prefix == "" {
		return nil
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	clean := path.Clean(prefix)
	if clean == "." || clean == "/" {
		return nil
	}
	relative := strings.TrimPrefix(clean, "/")
	if relative == "" {
		return nil
	}
	target := filepath.Join(userDir, relative)
	if err := os.MkdirAll(target, 0755); err != nil {
		return fmt.Errorf("failed to create app scope root: %w", err)
	}
	return nil
}

// checkPermission 检查权限
func (s *WebDAVService) checkPermission(ctx context.Context, u *user.User, r *http.Request) error {
	// 映射 HTTP 方法到操作
	operation := permission.MapHTTPMethodToOperation(r.Method)

	// 拼接用户目录和请求路径，得到相对于 webdav 根目录的完整路径
	// 例如：用户目录是 "BraveWolf44"，请求路径是 "/test/icon16.png"
	// 需要检查的是 "BraveWolf44/test/icon16.png"
	userDir := u.Directory
	if userDir == "" {
		userDir = u.Username
	}
	normalizedPath := s.normalizeWebdavRequestPath(r.URL.Path)
	fullPath := filepath.Join(userDir, strings.TrimPrefix(normalizedPath, "/"))

	// 检查权限
	return s.permissionCheck.Check(ctx, u, fullPath, operation)
}

func (s *WebDAVService) checkAppScope(ctx context.Context, r *http.Request) error {
	scope, err := resolveAppScope(ctx, s.config)
	if err != nil {
		return err
	}
	if !scope.active {
		return nil
	}

	sourcePath := s.normalizeWebdavRequestPath(r.URL.Path)
	if isAppScopeRootPath(sourcePath, scope.prefix) {
		if isAppScopeRootMethod(r.Method) {
			return nil
		}
		return auth.ErrAppScopeDenied
	}
	actions := requiredActionsForWebdavMethod(r.Method)
	if !scope.allowsAny(sourcePath, actions...) {
		return auth.ErrAppScopeDenied
	}

	if r.Method == "MOVE" || r.Method == "COPY" {
		dest := strings.TrimSpace(r.Header.Get("Destination"))
		if dest != "" {
			destPath := s.normalizeWebdavRequestPath(dest)
			if isAppScopeRootPath(destPath, scope.prefix) {
				return auth.ErrAppScopeDenied
			}
			if !scope.allowsAny(destPath, actions...) {
				return auth.ErrAppScopeDenied
			}
		}
	}

	return nil
}

func isAppScopeRootPath(rawPath, prefix string) bool {
	normalizedPath := normalizeScopePath(rawPath)
	normalizedPrefix := strings.TrimSuffix(normalizeScopePrefix(prefix), "/")
	return normalizedPath == normalizedPrefix
}

func isAppScopeRootMethod(method string) bool {
	switch strings.ToUpper(method) {
	case "GET", "HEAD", "OPTIONS", "PROPFIND", "REPORT", "SEARCH", "MKCOL":
		return true
	default:
		return false
	}
}

func requiredActionsForWebdavMethod(method string) []string {
	switch strings.ToUpper(method) {
	case "GET", "HEAD", "OPTIONS", "PROPFIND", "REPORT", "SEARCH":
		return []string{"read"}
	case "MKCOL", "POST":
		return []string{"create"}
	case "PUT":
		return []string{"update", "create"}
	case "PATCH", "PROPPATCH", "LOCK", "UNLOCK":
		return []string{"update"}
	case "DELETE":
		return []string{"delete"}
	case "MOVE":
		return []string{"move"}
	case "COPY":
		return []string{"copy"}
	default:
		op := permission.MapHTTPMethodToOperation(method)
		if op == permission.OperationRead {
			return []string{"read"}
		}
		return []string{"write"}
	}
}

func (s *WebDAVService) normalizeWebdavRequestPath(rawPath string) string {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "/"
	}
	if strings.HasPrefix(rawPath, "http://") || strings.HasPrefix(rawPath, "https://") {
		if u, err := url.Parse(rawPath); err == nil && u.Path != "" {
			rawPath = u.Path
		}
	}

	prefix := strings.TrimSpace(s.config.WebDAV.Prefix)
	if prefix == "" || prefix == "/" {
		return rawPath
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix != "" && prefix != "/" && strings.HasPrefix(rawPath, prefix) {
		rawPath = strings.TrimPrefix(rawPath, prefix)
		if rawPath == "" {
			rawPath = "/"
		}
	}
	return rawPath
}
