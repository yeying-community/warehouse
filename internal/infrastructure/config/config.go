package config

import (
	"time"
)

// Config 应用配置
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"` // 新增
	WebDAV   WebDAVConfig   `yaml:"webdav"`
	Web3     Web3Config     `yaml:"web3"`
	Security SecurityConfig `yaml:"security"`
	CORS     CORSConfig     `yaml:"cors"`
	Log      LogConfig      `yaml:"log"`
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Type         string        `yaml:"type"` // 仅支持 "postgres"/"postgresql"
	Host         string        `yaml:"host"`
	Port         int           `yaml:"port"`
	Database     string        `yaml:"database"`
	Username     string        `yaml:"username"`
	Password     string        `yaml:"password"`
	SSLMode      string        `yaml:"ssl_mode"`
	MaxOpenConns int           `yaml:"max_open_conns"`
	MaxIdleConns int           `yaml:"max_idle_conns"`
	MaxLifetime  time.Duration `yaml:"max_lifetime"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Address         string        `yaml:"address"`
	Port            int           `yaml:"port"`
	TLS             bool          `yaml:"tls"`
	CertFile        string        `yaml:"cert_file"`
	KeyFile         string        `yaml:"key_file"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	IdleTimeout     time.Duration `yaml:"idle_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

// WebDAVConfig WebDAV 配置
type WebDAVConfig struct {
	Prefix      string `yaml:"prefix"`
	Directory   string `yaml:"directory"`
	NoSniff     bool   `yaml:"no_sniff"`
	Permissions string `yaml:"permissions"`
}

// Web3Config Web3 配置
type Web3Config struct {
	JWTSecret              string        `yaml:"jwt_secret"`
	TokenExpiration        time.Duration `yaml:"token_expiration"`
	RefreshTokenExpiration time.Duration `yaml:"refresh_token_expiration"`
	UCAN                   UCANConfig    `yaml:"ucan"`
}

// UCANConfig UCAN authentication configuration
type UCANConfig struct {
	Enabled          bool           `yaml:"enabled"`
	Audience         string         `yaml:"audience"`
	RequiredResource string         `yaml:"required_resource"`
	RequiredAction   string         `yaml:"required_action"`
	AppScope         AppScopeConfig `yaml:"app_scope"`
}

// AppScopeConfig config for UCAN app scope enforcement.
type AppScopeConfig struct {
	PathPrefix string `yaml:"path_prefix"`
}

// SecurityConfig 安全配置
type SecurityConfig struct {
	NoPassword     bool     `yaml:"no_password"`
	BehindProxy    bool     `yaml:"behind_proxy"`
	AdminAddresses []string `yaml:"admin_addresses"`
}

// CORSConfig CORS 配置
type CORSConfig struct {
	Enabled        bool     `yaml:"enabled"`
	Credentials    bool     `yaml:"credentials"`
	AllowedOrigins []string `yaml:"allowed_origins"`
	AllowedMethods []string `yaml:"allowed_methods"`
	AllowedHeaders []string `yaml:"allowed_headers"`
	ExposedHeaders []string `yaml:"exposed_headers"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level   string   `yaml:"level"`
	Format  string   `yaml:"format"`
	Colors  bool     `yaml:"colors"`
	Outputs []string `yaml:"outputs"`
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Address:         "0.0.0.0",
			Port:            6065,
			TLS:             false,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     60 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
		Database: DatabaseConfig{
			Type:         "postgres", // 默认使用 PostgreSQL
			Host:         "localhost",
			Port:         5432,
			Database:     "webdav",
			Username:     "webdav",
			Password:     "",
			SSLMode:      "disable",
			MaxOpenConns: 25,
			MaxIdleConns: 5,
			MaxLifetime:  5 * time.Minute,
		},
		WebDAV: WebDAVConfig{
			Prefix:      "/",
			Directory:   "/data",
			NoSniff:     true,
			Permissions: "R",
		},
		Web3: Web3Config{
			TokenExpiration:        24 * time.Hour,
			RefreshTokenExpiration: 30 * 24 * time.Hour,
			UCAN: UCANConfig{
				AppScope: AppScopeConfig{
					PathPrefix: "/apps",
				},
			},
		},
		Security: SecurityConfig{
			NoPassword:     false,
			BehindProxy:    false,
			AdminAddresses: []string{},
		},
		CORS: CORSConfig{
			Enabled:     false,
			Credentials: false,
		},
		Log: LogConfig{
			Level:   "info",
			Format:  "console",
			Colors:  true,
			Outputs: []string{"stderr"},
		},
	}
}
