package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

// Loader 配置加载器
type Loader struct {
	defaultConfig *Config
}

// NewLoader 创建配置加载器
func NewLoader() *Loader {
	return &Loader{
		defaultConfig: DefaultConfig(),
	}
}

// Load 加载配置
func (l *Loader) Load(configFile string, flags *pflag.FlagSet) (*Config, error) {
	config := l.defaultConfig

	// 1. 从文件加载
	if configFile != "" {
		if err := l.LoadFromFile(configFile, config); err != nil {
			return nil, fmt.Errorf("failed to load config file: %w", err)
		}
	}

	// 2. 从命令行参数覆盖
	if flags != nil {
		l.overrideFromFlags(config, flags)
	}

	// 3. 从环境变量覆盖
	l.overrideFromEnv(config)

	// 4. 验证配置
	if err := l.validate(config); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return config, nil
}

// loadFromFile 从文件加载配置
func (l *Loader) LoadFromFile(filename string, config *Config) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	return yaml.Unmarshal(data, config)
}

// overrideFromFlags 从命令行参数覆盖配置
func (l *Loader) overrideFromFlags(config *Config, flags *pflag.FlagSet) {
	if flags.Changed("address") {
		config.Server.Address, _ = flags.GetString("address")
	}
	if flags.Changed("port") {
		config.Server.Port, _ = flags.GetInt("port")
	}
	if flags.Changed("tls") {
		config.Server.TLS, _ = flags.GetBool("tls")
	}
	if flags.Changed("cert") {
		config.Server.CertFile, _ = flags.GetString("cert")
	}
	if flags.Changed("key") {
		config.Server.KeyFile, _ = flags.GetString("key")
	}
	if flags.Changed("prefix") {
		config.WebDAV.Prefix, _ = flags.GetString("prefix")
	}
	if flags.Changed("directory") {
		config.WebDAV.Directory, _ = flags.GetString("directory")
	}
}

// overrideFromEnv 从环境变量覆盖配置
func (l *Loader) overrideFromEnv(config *Config) {
	if v := os.Getenv("WEBDAV_ADDRESS"); v != "" {
		config.Server.Address = v
	}
	if v := os.Getenv("WEBDAV_PORT"); v != "" {
		// 解析端口...
	}
	if v := os.Getenv("WEBDAV_JWT_SECRET"); v != "" {
		config.Web3.JWTSecret = v
	}
	if v := os.Getenv("WEBDAV_UCAN_ENABLED"); v != "" {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			config.Web3.UCAN.Enabled = true
		default:
			config.Web3.UCAN.Enabled = false
		}
	}
	if v := os.Getenv("WEBDAV_UCAN_AUDIENCE"); v != "" {
		config.Web3.UCAN.Audience = v
	}
	if v := os.Getenv("WEBDAV_UCAN_RESOURCE"); v != "" {
		config.Web3.UCAN.RequiredResource = v
	}
	if v := os.Getenv("WEBDAV_UCAN_ACTION"); v != "" {
		config.Web3.UCAN.RequiredAction = v
	}
	if v := os.Getenv("WEBDAV_UCAN_APP_SCOPE_PATH_PREFIX"); v != "" {
		config.Web3.UCAN.AppScope.PathPrefix = v
	}
	if v := os.Getenv("WEBDAV_ADMIN_ADDRESSES"); v != "" {
		config.Security.AdminAddresses = strings.Split(v, ",")
	}
}

// validate 验证配置
func (l *Loader) validate(config *Config) error {
	l.normalizeAdminAddresses(config)

	if err := l.validateServer(config); err != nil {
		return fmt.Errorf("server config: %w", err)
	}
	if err := l.validateWebDAV(config); err != nil {
		return fmt.Errorf("webdav config: %w", err)
	}
	if err := l.validateWeb3(config); err != nil {
		return fmt.Errorf("web3 config: %w", err)
	}
	if err := l.validateDatabase(config); err != nil {
		return fmt.Errorf("database config: %w", err)
	}
	return nil
}

// validateServer 验证服务器配置
func (l *Loader) validateServer(config *Config) error {
	if config.Server.Port < 1 || config.Server.Port > 65535 {
		return errors.New("invalid port number")
	}

	if config.Server.TLS {
		if config.Server.CertFile == "" {
			return errors.New("cert_file is required when TLS is enabled")
		}
		if config.Server.KeyFile == "" {
			return errors.New("key_file is required when TLS is enabled")
		}
		// 检查证书文件是否存在
		if _, err := os.Stat(config.Server.CertFile); err != nil {
			return fmt.Errorf("cert file not found: %w", err)
		}
		if _, err := os.Stat(config.Server.KeyFile); err != nil {
			return fmt.Errorf("key file not found: %w", err)
		}
	}

	return nil
}

// validateWebDAV 验证 WebDAV 配置
func (l *Loader) validateWebDAV(config *Config) error {
	if config.WebDAV.Directory == "" {
		return errors.New("directory is required")
	}

	// 检查目录是否存在
	info, err := os.Stat(config.WebDAV.Directory)
	if err != nil {
		// 创建目录
		if err := os.MkdirAll(config.WebDAV.Directory, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
		info, _ = os.Stat(config.WebDAV.Directory)
	}

	if !info.IsDir() {
		return errors.New("directory is not a directory")
	}

	return nil
}

// validateWeb3 验证 Web3 配置
func (l *Loader) validateWeb3(config *Config) error {
	if config.Web3.JWTSecret == "" {
		return errors.New("jwt_secret is required when web3 is enabled")
	}
	if len(config.Web3.JWTSecret) < 32 {
		return errors.New("jwt_secret must be at least 32 characters")
	}

	return nil
}

func (l *Loader) normalizeAdminAddresses(config *Config) {
	if len(config.Security.AdminAddresses) == 0 {
		return
	}

	normalized := make([]string, 0, len(config.Security.AdminAddresses))
	seen := make(map[string]struct{}, len(config.Security.AdminAddresses))

	for _, raw := range config.Security.AdminAddresses {
		addr := strings.ToLower(strings.TrimSpace(raw))
		if addr == "" {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		normalized = append(normalized, addr)
	}

	config.Security.AdminAddresses = normalized
}

// validateDatabase 验证数据库配置（仅支持 PostgreSQL）
func (l *Loader) validateDatabase(config *Config) error {
	t := strings.ToLower(strings.TrimSpace(config.Database.Type))
	if t != "postgres" && t != "postgresql" {
		return fmt.Errorf("database.type must be 'postgres' or 'postgresql'")
	}
	if config.Database.Host == "" {
		return errors.New("host is required")
	}
	if config.Database.Port <= 0 || config.Database.Port > 65535 {
		return errors.New("invalid port")
	}
	if config.Database.Database == "" {
		return errors.New("database name is required")
	}
	if config.Database.Username == "" {
		return errors.New("username is required")
	}
	// password 可为空，取决于连接策略/环境
	return nil
}
