package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

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
		if port, err := strconv.Atoi(v); err == nil {
			config.Server.Port = port
		}
	}
	if v := os.Getenv("WEBDAV_NODE_ID"); v != "" {
		config.Node.ID = v
	}
	if v := os.Getenv("WEBDAV_NODE_ROLE"); v != "" {
		config.Node.Role = v
	}
	if v := os.Getenv("WEBDAV_NODE_ADVERTISE_URL"); v != "" {
		config.Node.AdvertiseURL = v
	}
	if v := os.Getenv("WEBDAV_INTERNAL_REPLICATION_ENABLED"); v != "" {
		config.Internal.Replication.Enabled = parseEnvBool(v)
	}
	if v := os.Getenv("WEBDAV_INTERNAL_REPLICATION_PEER_NODE_ID"); v != "" {
		config.Internal.Replication.PeerNodeID = v
	}
	if v := os.Getenv("WEBDAV_INTERNAL_REPLICATION_PEER_BASE_URL"); v != "" {
		config.Internal.Replication.PeerBaseURL = v
	}
	if v := os.Getenv("WEBDAV_INTERNAL_REPLICATION_SHARED_SECRET"); v != "" {
		config.Internal.Replication.SharedSecret = v
	}
	if v := os.Getenv("WEBDAV_INTERNAL_REPLICATION_ALLOWED_CLOCK_SKEW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			config.Internal.Replication.AllowedClockSkew = d
		}
	}
	if v := os.Getenv("WEBDAV_INTERNAL_REPLICATION_DISPATCH_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			config.Internal.Replication.DispatchInterval = d
		}
	}
	if v := os.Getenv("WEBDAV_INTERNAL_REPLICATION_REQUEST_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			config.Internal.Replication.RequestTimeout = d
		}
	}
	if v := os.Getenv("WEBDAV_INTERNAL_REPLICATION_BATCH_SIZE"); v != "" {
		if size, err := strconv.Atoi(v); err == nil {
			config.Internal.Replication.BatchSize = size
		}
	}
	if v := os.Getenv("WEBDAV_INTERNAL_REPLICATION_RETRY_BACKOFF_BASE"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			config.Internal.Replication.RetryBackoffBase = d
		}
	}
	if v := os.Getenv("WEBDAV_INTERNAL_REPLICATION_MAX_RETRY_BACKOFF"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			config.Internal.Replication.MaxRetryBackoff = d
		}
	}
	if v := os.Getenv("WEBDAV_PREFIX"); v != "" {
		config.WebDAV.Prefix = v
	}
	if v := os.Getenv("WEBDAV_DIRECTORY"); v != "" {
		config.WebDAV.Directory = v
	}
	if v := os.Getenv("WEBDAV_AUTO_CREATE_DIRECTORY"); v != "" {
		config.WebDAV.AutoCreateDirectory = parseEnvBool(v)
	}
	if v := os.Getenv("WEBDAV_BEHIND_PROXY"); v != "" {
		config.Security.BehindProxy = parseEnvBool(v)
	}
	if v := os.Getenv("WEBDAV_DB_HOST"); v != "" {
		config.Database.Host = v
	}
	if v := os.Getenv("WEBDAV_DB_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			config.Database.Port = port
		}
	}
	if v := os.Getenv("WEBDAV_DB_NAME"); v != "" {
		config.Database.Database = v
	}
	if v := os.Getenv("WEBDAV_DB_USER"); v != "" {
		config.Database.Username = v
	}
	if v := os.Getenv("WEBDAV_DB_PASSWORD"); v != "" {
		config.Database.Password = v
	}
	if v := os.Getenv("WEBDAV_DB_SSL_MODE"); v != "" {
		config.Database.SSLMode = v
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

	if v := os.Getenv("WEBDAV_EMAIL_ENABLED"); v != "" {
		config.Email.Enabled = parseEnvBool(v)
	}
	if v := os.Getenv("WEBDAV_EMAIL_SMTP_HOST"); v != "" {
		config.Email.SMTPHost = v
	}
	if v := os.Getenv("WEBDAV_EMAIL_SMTP_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			config.Email.SMTPPort = port
		}
	}
	if v := os.Getenv("WEBDAV_EMAIL_SMTP_USERNAME"); v != "" {
		config.Email.SMTPUsername = v
	}
	if v := os.Getenv("WEBDAV_EMAIL_SMTP_PASSWORD"); v != "" {
		config.Email.SMTPPassword = v
	}
	if v := os.Getenv("WEBDAV_EMAIL_FROM"); v != "" {
		config.Email.From = v
	}
	if v := os.Getenv("WEBDAV_EMAIL_FROM_NAME"); v != "" {
		config.Email.FromName = v
	}
	if v := os.Getenv("WEBDAV_EMAIL_TEMPLATE_PATH"); v != "" {
		config.Email.TemplatePath = v
	}
	if v := os.Getenv("WEBDAV_EMAIL_CODE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			config.Email.CodeTTL = d
		}
	}
	if v := os.Getenv("WEBDAV_EMAIL_SEND_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			config.Email.SendInterval = d
		}
	}
	if v := os.Getenv("WEBDAV_EMAIL_CODE_LENGTH"); v != "" {
		if length, err := strconv.Atoi(v); err == nil {
			config.Email.CodeLength = length
		}
	}
	if v := os.Getenv("WEBDAV_EMAIL_AUTO_CREATE_ON_LOGIN"); v != "" {
		config.Email.AutoCreateOnLogin = parseEnvBool(v)
	}
	if v := os.Getenv("WEBDAV_EMAIL_USE_TLS"); v != "" {
		config.Email.UseTLS = parseEnvBool(v)
	}
	if v := os.Getenv("WEBDAV_EMAIL_INSECURE_SKIP_VERIFY"); v != "" {
		config.Email.InsecureSkipVerify = parseEnvBool(v)
	}
}

func parseEnvBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// validate 验证配置
func (l *Loader) validate(config *Config) error {
	l.normalizeAdminAddresses(config)

	if err := l.validateServer(config); err != nil {
		return fmt.Errorf("server config: %w", err)
	}
	if err := l.validateNode(config); err != nil {
		return fmt.Errorf("node config: %w", err)
	}
	if err := l.validateInternal(config); err != nil {
		return fmt.Errorf("internal config: %w", err)
	}
	if err := l.validateWebDAV(config); err != nil {
		return fmt.Errorf("webdav config: %w", err)
	}
	if err := l.validateWeb3(config); err != nil {
		return fmt.Errorf("web3 config: %w", err)
	}
	if err := l.validateEmail(config); err != nil {
		return fmt.Errorf("email config: %w", err)
	}
	if err := l.validateDatabase(config); err != nil {
		return fmt.Errorf("database config: %w", err)
	}
	return nil
}

func (l *Loader) validateNode(config *Config) error {
	role := strings.ToLower(strings.TrimSpace(config.Node.Role))
	switch role {
	case "", "active":
		config.Node.Role = "active"
	case "standby":
		config.Node.Role = "standby"
	default:
		return fmt.Errorf("unsupported role %q: only active/standby supported", config.Node.Role)
	}

	config.Node.ID = strings.TrimSpace(config.Node.ID)
	config.Node.AdvertiseURL = strings.TrimSpace(config.Node.AdvertiseURL)
	if config.Node.AdvertiseURL != "" {
		parsed, err := url.Parse(config.Node.AdvertiseURL)
		if err != nil {
			return fmt.Errorf("invalid node.advertise_url: %w", err)
		}
		if parsed.Scheme == "" || parsed.Host == "" {
			return errors.New("node.advertise_url must include scheme and host")
		}
	}
	return nil
}

func (l *Loader) validateInternal(config *Config) error {
	replication := &config.Internal.Replication
	replication.PeerNodeID = strings.TrimSpace(replication.PeerNodeID)
	replication.PeerBaseURL = strings.TrimSpace(replication.PeerBaseURL)
	replication.SharedSecret = strings.TrimSpace(replication.SharedSecret)

	if !replication.Enabled {
		return nil
	}
	if config.Node.ID == "" {
		return errors.New("node.id is required when internal replication is enabled")
	}
	if replication.SharedSecret == "" {
		return errors.New("internal.replication.shared_secret is required when internal replication is enabled")
	}
	if replication.AllowedClockSkew <= 0 {
		return errors.New("internal.replication.allowed_clock_skew must be greater than zero")
	}
	if replication.DispatchInterval <= 0 {
		return errors.New("internal.replication.dispatch_interval must be greater than zero")
	}
	if replication.RequestTimeout <= 0 {
		return errors.New("internal.replication.request_timeout must be greater than zero")
	}
	if replication.BatchSize <= 0 {
		return errors.New("internal.replication.batch_size must be greater than zero")
	}
	if replication.RetryBackoffBase <= 0 {
		return errors.New("internal.replication.retry_backoff_base must be greater than zero")
	}
	if replication.MaxRetryBackoff < replication.RetryBackoffBase {
		return errors.New("internal.replication.max_retry_backoff must be greater than or equal to retry_backoff_base")
	}
	if replication.PeerBaseURL != "" {
		parsed, err := url.Parse(replication.PeerBaseURL)
		if err != nil {
			return fmt.Errorf("invalid internal.replication.peer_base_url: %w", err)
		}
		if parsed.Scheme == "" || parsed.Host == "" {
			return errors.New("internal.replication.peer_base_url must include scheme and host")
		}
	}
	if replication.PeerBaseURL != "" && replication.PeerNodeID == "" {
		return errors.New("internal.replication.peer_node_id is required when peer_base_url is configured")
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
		if !config.WebDAV.AutoCreateDirectory {
			return fmt.Errorf("directory does not exist: %w", err)
		}
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

// validateEmail 验证邮箱登录配置
func (l *Loader) validateEmail(config *Config) error {
	if !config.Email.Enabled {
		return nil
	}
	if config.Email.SMTPHost == "" {
		return errors.New("smtp_host is required when email login is enabled")
	}
	if config.Email.SMTPPort <= 0 || config.Email.SMTPPort > 65535 {
		return errors.New("smtp_port is invalid")
	}
	if config.Email.From == "" {
		return errors.New("from is required when email login is enabled")
	}
	if config.Email.CodeTTL <= 0 {
		return errors.New("code_ttl must be positive")
	}
	if config.Email.SendInterval < 0 {
		return errors.New("send_interval must be non-negative")
	}
	if config.Email.CodeLength < 4 || config.Email.CodeLength > 10 {
		return errors.New("code_length must be between 4 and 10")
	}
	if config.Email.TemplatePath == "" {
		return errors.New("template_path is required when email login is enabled")
	}
	if _, err := os.Stat(config.Email.TemplatePath); err != nil {
		return fmt.Errorf("template_path not found: %w", err)
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
