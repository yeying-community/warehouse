package assetspace

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"go.uber.org/zap"
)

const (
	// PersonalSpaceKey 个人资产空间 key
	PersonalSpaceKey = "personal"
	// AppsSpaceKey 应用资产空间 key
	AppsSpaceKey = "apps"
)

// Space 资产空间元信息
type Space struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// Manager 管理用户资产空间目录（personal/apps）
type Manager struct {
	webdavRoot   string
	appScopePath string
	logger       *zap.Logger
}

// NewManager 创建资产空间管理器
func NewManager(cfg *config.Config, logger *zap.Logger) *Manager {
	webdavRoot := ""
	appScopePath := "/apps"
	if cfg != nil {
		webdavRoot = strings.TrimSpace(cfg.WebDAV.Directory)
		appScopePath = normalizeAppScopePath(cfg.Web3.UCAN.AppScope.PathPrefix)
	}

	return &Manager{
		webdavRoot:   webdavRoot,
		appScopePath: appScopePath,
		logger:       logger,
	}
}

// DefaultSpace 返回默认空间 key
func (m *Manager) DefaultSpace() string {
	return PersonalSpaceKey
}

// Spaces 返回可展示的空间列表
func (m *Manager) Spaces() []Space {
	appPath := "/apps"
	if m != nil && m.appScopePath != "" {
		appPath = m.appScopePath
	}

	return []Space{
		{Key: PersonalSpaceKey, Name: "个人资产", Path: "/" + PersonalSpaceKey},
		{Key: AppsSpaceKey, Name: "应用资产", Path: appPath},
	}
}

// EnsureForUser 确保用户空间目录存在（幂等）
func (m *Manager) EnsureForUser(u *user.User) error {
	if u == nil {
		return fmt.Errorf("user is nil")
	}
	userRoot := m.resolveUserRoot(u)
	if userRoot == "" {
		return fmt.Errorf("user root directory is empty")
	}
	return m.EnsureForUserDirectory(userRoot)
}

// EnsureForUserDirectory 确保指定用户根目录下的空间目录存在（幂等）
func (m *Manager) EnsureForUserDirectory(userRoot string) error {
	userRoot = strings.TrimSpace(userRoot)
	if userRoot == "" {
		return fmt.Errorf("user root directory is empty")
	}

	appRelative := strings.TrimPrefix(normalizeAppScopePath(m.appScopePath), "/")
	dirs := []string{
		filepath.Clean(userRoot),
		filepath.Join(userRoot, PersonalSpaceKey),
		filepath.Join(userRoot, filepath.FromSlash(appRelative)),
	}

	seen := make(map[string]struct{}, len(dirs))
	for _, dir := range dirs {
		dir = filepath.Clean(dir)
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}

		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to ensure directory %s: %w", dir, err)
		}
	}

	if m != nil && m.logger != nil {
		m.logger.Debug("asset spaces ensured", zap.String("user_root", filepath.Clean(userRoot)))
	}
	return nil
}

func (m *Manager) resolveUserRoot(u *user.User) string {
	userDir := strings.TrimSpace(u.Directory)
	if userDir == "" {
		userDir = strings.TrimSpace(u.Username)
	}
	if userDir == "" {
		return ""
	}

	if filepath.IsAbs(userDir) {
		return filepath.Clean(userDir)
	}

	base := ""
	if m != nil {
		base = strings.TrimSpace(m.webdavRoot)
	}
	if base == "" {
		return filepath.Clean(userDir)
	}
	return filepath.Join(base, userDir)
}

func normalizeAppScopePath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/apps"
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	clean := path.Clean(raw)
	if clean == "." || clean == "/" {
		return "/apps"
	}
	return clean
}
