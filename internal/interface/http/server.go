package http

import (
	"context"
	"fmt"
	"net/http"

	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"go.uber.org/zap"
)

// Server HTTP 服务器
type Server struct {
	config     *config.Config
	router     *Router
	httpServer *http.Server
	logger     *zap.Logger
}

// NewServer 创建 HTTP 服务器
func NewServer(cfg *config.Config, router *Router, logger *zap.Logger) *Server {
	return &Server{
		config: cfg,
		router: router,
		logger: logger,
	}
}

// Start 启动服务器
func (s *Server) Start() error {
	// 设置路由
	handler := s.router.Setup()

	// 创建 HTTP 服务器
	addr := fmt.Sprintf("%s:%d", s.config.Server.Address, s.config.Server.Port)

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  s.config.Server.ReadTimeout,
		WriteTimeout: s.config.Server.WriteTimeout,
		IdleTimeout:  s.config.Server.IdleTimeout,
	}

	// 启动服务器
	s.logger.Info("starting http server",
		zap.String("address", addr),
		zap.Bool("tls", s.config.Server.TLS))

	if s.config.Server.TLS {
		return s.startTLS()
	}

	return s.start()
}

// start 启动 HTTP 服务器
func (s *Server) start() error {
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("failed to start server: %w", err)
	}
	return nil
}

// startTLS 启动 HTTPS 服务器
func (s *Server) startTLS() error {
	if err := s.httpServer.ListenAndServeTLS(
		s.config.Server.CertFile,
		s.config.Server.KeyFile,
	); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("failed to start tls server: %w", err)
	}
	return nil
}

// Shutdown 优雅关闭服务器
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down http server")

	if s.httpServer == nil {
		return nil
	}

	// 创建关闭上下文
	shutdownCtx, cancel := context.WithTimeout(ctx, s.config.Server.ShutdownTimeout)
	defer cancel()

	// 关闭服务器
	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}

	s.logger.Info("http server stopped")
	return nil
}
