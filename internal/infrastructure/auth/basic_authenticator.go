package auth

import (
	"context"
	"fmt"

	"github.com/yeying-community/warehouse/internal/domain/auth"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/crypto"
	"go.uber.org/zap"
)

// BasicAuthenticator Basic 认证器
type BasicAuthenticator struct {
	userRepo       user.Repository
	passwordHasher *crypto.PasswordHasher
	noPassword     bool
	logger         *zap.Logger
}

// NewBasicAuthenticator 创建 Basic 认证器
func NewBasicAuthenticator(
	userRepo user.Repository,
	noPassword bool,
	logger *zap.Logger,
) *BasicAuthenticator {
	return &BasicAuthenticator{
		userRepo:       userRepo,
		passwordHasher: crypto.NewPasswordHasher(),
		noPassword:     noPassword,
		logger:         logger,
	}
}

// Name 认证器名称
func (a *BasicAuthenticator) Name() string {
	return "basic"
}

// Authenticate 认证用户
func (a *BasicAuthenticator) Authenticate(ctx context.Context, credentials interface{}) (*user.User, error) {
	creds, ok := credentials.(*auth.BasicCredentials)
	if !ok {
		return nil, fmt.Errorf("invalid credentials type")
	}

	// 查找用户
	u, err := a.userRepo.FindByUsername(ctx, creds.Username)
	if err != nil {
		if err == user.ErrUserNotFound {
			a.logger.Debug("user not found",
				zap.String("username", creds.Username))
			return nil, user.ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to find user: %w", err)
	}

	// 如果启用了无密码模式，直接返回
	if a.noPassword {
		a.logger.Debug("user authenticated (no password mode)",
			zap.String("username", u.Username))
		return u, nil
	}

	// 验证密码
	if !u.HasPassword() {
		a.logger.Warn("user has no password",
			zap.String("username", u.Username))
		return nil, user.ErrInvalidPassword
	}

	if err := a.passwordHasher.Verify(u.Password, creds.Password); err != nil {
		a.logger.Warn("password verification failed",
			zap.String("username", u.Username),
			zap.Error(err))
		return nil, user.ErrInvalidPassword
	}

	a.logger.Debug("user authenticated via basic auth",
		zap.String("username", u.Username))

	return u, nil
}

// CanHandle 是否可以处理该凭证
func (a *BasicAuthenticator) CanHandle(credentials interface{}) bool {
	_, ok := credentials.(*auth.BasicCredentials)
	return ok
}
