package auth

import (
	"context"

	"github.com/yeying-community/warehouse/internal/domain/user"
)

// Authenticator 认证器接口
type Authenticator interface {
	// Name 认证器名称
	Name() string

	// Authenticate 认证用户
	Authenticate(ctx context.Context, credentials interface{}) (*user.User, error)

	// CanHandle 是否可以处理该凭证
	CanHandle(credentials interface{}) bool
}

// ContextEnricher enriches request context after successful authentication.
type ContextEnricher interface {
	EnrichContext(ctx context.Context, credentials interface{}) context.Context
}

// BasicCredentials Basic 认证凭证
type BasicCredentials struct {
	Username string
	Password string
}

// BearerCredentials Bearer Token 凭证
type BearerCredentials struct {
	Token string
}
