package middleware

import (
	"net/http"
	"strings"
)

// CORSConfig CORS 配置
type CORSConfig struct {
	Enabled        bool
	Credentials    bool
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
	ExposedHeaders []string
}

// CORSMiddleware CORS 中间件
type CORSMiddleware struct {
	config *CORSConfig
}

// NewCORSMiddleware 创建 CORS 中间件
func NewCORSMiddleware(config *CORSConfig) *CORSMiddleware {
	return &CORSMiddleware{
		config: config,
	}
}

// Handle 处理 CORS
func (m *CORSMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// 检查是否允许该来源
		if m.isOriginAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)

			if m.config.Credentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			if len(m.config.AllowedMethods) > 0 {
				w.Header().Set("Access-Control-Allow-Methods", strings.Join(m.config.AllowedMethods, ", "))
			}

			if len(m.config.AllowedHeaders) > 0 {
				w.Header().Set("Access-Control-Allow-Headers", strings.Join(m.config.AllowedHeaders, ", "))
			}

			if len(m.config.ExposedHeaders) > 0 {
				w.Header().Set("Access-Control-Expose-Headers", strings.Join(m.config.ExposedHeaders, ", "))
			}
		}

		// 处理带 Origin 的预检请求
		if origin != "" && r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isOriginAllowed 检查来源是否允许
func (m *CORSMiddleware) isOriginAllowed(origin string) bool {
	if origin == "" {
		return false
	}

	for _, allowed := range m.config.AllowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}

	return false
}
