package middleware

import (
	"net/http"
	"time"

	"go.uber.org/zap"
)

// LoggerMiddleware 日志中间件
type LoggerMiddleware struct {
	logger      *zap.Logger
	behindProxy bool
}

// NewLoggerMiddleware 创建日志中间件
func NewLoggerMiddleware(logger *zap.Logger, behindProxy bool) *LoggerMiddleware {
	return &LoggerMiddleware{
		logger:      logger,
		behindProxy: behindProxy,
	}
}

// Handle 处理日志
func (m *LoggerMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// 包装 ResponseWriter 以捕获状态码
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// 处理请求
		next.ServeHTTP(wrapped, r)

		// 记录日志
		duration := time.Since(start)

		fields := []zap.Field{
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("if", r.Header.Get("If")),
			zap.String("lock-token", r.Header.Get("Lock-Token")),
			zap.String("timeout", r.Header.Get("Timeout")),
			zap.Int("status", wrapped.statusCode),
			zap.Duration("duration", duration),
			zap.String("remote_addr", m.getRemoteAddr(r)),
			zap.String("user_agent", r.UserAgent()),
		}

		// 如果有用户信息，添加到日志
		if u, ok := GetUserFromContext(r.Context()); ok {
			fields = append(fields, zap.String("username", u.Username))
		}

		m.logger.Debug("http request", fields...)
	})
}

// getRemoteAddr 获取远程地址
func (m *LoggerMiddleware) getRemoteAddr(r *http.Request) string {
	if m.behindProxy {
		// 尝试从 X-Forwarded-For 或 X-Real-IP 获取
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return xff
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return xri
		}
	}
	return r.RemoteAddr
}

// responseWriter 包装 ResponseWriter
type responseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (w *responseWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.statusCode = statusCode
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(w.statusCode)
	}
	return w.ResponseWriter.Write(data)
}

func (w *responseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(w.statusCode)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
