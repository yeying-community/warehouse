package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/yeying-community/webdav/internal/domain/auth"
	"github.com/yeying-community/webdav/internal/domain/user"
	"github.com/yeying-community/webdav/internal/infrastructure/crypto"
	"go.uber.org/zap"
)

// Web3Authenticator Web3 认证器
type Web3Authenticator struct {
	userRepo          user.Repository
	jwtManager        *JWTManager
	ucanVerifier      *UcanVerifier
	challengeStore    *ChallengeStore
	ethSigner         *crypto.EthereumSigner
	logger            *zap.Logger
	refreshExpiration time.Duration
}

// NewWeb3Authenticator 创建 Web3 认证器
func NewWeb3Authenticator(
	userRepo user.Repository,
	jwtSecret string,
	tokenExpiration time.Duration,
	refreshTokenExpiration time.Duration,
	ucanVerifier *UcanVerifier,
	logger *zap.Logger,
) *Web3Authenticator {
	return &Web3Authenticator{
		userRepo:          userRepo,
		jwtManager:        NewJWTManager(jwtSecret, tokenExpiration),
		ucanVerifier:      ucanVerifier,
		challengeStore:    NewChallengeStore(),
		ethSigner:         crypto.NewEthereumSigner(),
		logger:            logger,
		refreshExpiration: refreshTokenExpiration,
	}
}

// Name 认证器名称
func (a *Web3Authenticator) Name() string {
	return "web3"
}

// Authenticate 认证用户
func (a *Web3Authenticator) Authenticate(ctx context.Context, credentials interface{}) (*user.User, error) {
	creds, ok := credentials.(*auth.BearerCredentials)
	if !ok {
		return nil, fmt.Errorf("invalid credentials type")
	}

	// 验证 Token (UCAN 或 JWT)
	address, err := a.verifyToken(creds.Token)
	if err != nil {
		return nil, err
	}

	// 查找用户
	u, err := a.userRepo.FindByWalletAddress(ctx, address)
	if err != nil {
		if err == user.ErrUserNotFound {
			a.logger.Debug("wallet address not found",
				zap.String("address", address))
			return nil, user.ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to find user: %w", err)
	}

	a.logger.Debug("user authenticated via web3",
		zap.String("username", u.Username),
		zap.String("address", address))

	return u, nil
}

func (a *Web3Authenticator) verifyToken(token string) (string, error) {
	if token == "" {
		return "", auth.ErrInvalidToken
	}

	if isUcanToken(token) {
		if a.ucanVerifier == nil || !a.ucanVerifier.Enabled() {
			return "", auth.ErrInvalidToken
		}
		address, err := a.ucanVerifier.VerifyInvocation(token)
		if err != nil {
			a.logger.Debug("ucan verification failed", zap.Error(err))
			return "", err
		}
		return address, nil
	}

	address, err := a.jwtManager.Verify(token)
	if err != nil {
		a.logger.Debug("jwt verification failed", zap.Error(err))
		return "", err
	}
	return address, nil
}

// CanHandle 是否可以处理该凭证
func (a *Web3Authenticator) CanHandle(credentials interface{}) bool {
	_, ok := credentials.(*auth.BearerCredentials)
	return ok
}

// CreateChallenge 创建挑战
func (a *Web3Authenticator) CreateChallenge(address string) (*auth.Challenge, error) {
	// 验证地址格式
	if !a.ethSigner.IsValidAddress(address) {
		return nil, fmt.Errorf("invalid ethereum address")
	}

	// 创建挑战（5分钟有效期）
	challenge, err := a.challengeStore.Create(address, 5*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("failed to create challenge: %w", err)
	}

	a.logger.Debug("challenge created",
		zap.String("address", address),
		zap.String("nonce", challenge.Nonce))

	return challenge, nil
}

// VerifySignature 验证签名并生成 token
func (a *Web3Authenticator) VerifySignature(ctx context.Context, address, signature string) (*auth.Token, error) {
	// 验证地址格式
	if !a.ethSigner.IsValidAddress(address) {
		return nil, fmt.Errorf("invalid ethereum address")
	}

	// 获取挑战
	challenge, ok := a.challengeStore.Get(address)
	if !ok {
		a.logger.Warn("challenge not found or expired",
			zap.String("address", address))
		return nil, auth.ErrChallengeExpired
	}

	// 验证签名
	if err := a.ethSigner.VerifySignature(challenge.Message, signature, address); err != nil {
		a.logger.Warn("signature verification failed",
			zap.String("address", address),
			zap.Error(err))
		return nil, auth.ErrInvalidSignature
	}

	// 删除已使用的挑战
	a.challengeStore.Delete(address)

	// 生成 JWT
	token, err := a.jwtManager.Generate(address)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	a.logger.Info("signature verified, token generated",
		zap.String("address", address))

	return token, nil
}

// GenerateAccessToken 生成访问令牌
func (a *Web3Authenticator) GenerateAccessToken(address string) (*auth.Token, error) {
	return a.jwtManager.Generate(address)
}

// GenerateRefreshToken 生成刷新令牌
func (a *Web3Authenticator) GenerateRefreshToken(address string) (*auth.Token, error) {
	return a.jwtManager.GenerateRefresh(address, a.refreshExpiration)
}

// VerifyRefreshToken 验证刷新令牌
func (a *Web3Authenticator) VerifyRefreshToken(token string) (string, error) {
	return a.jwtManager.VerifyRefresh(token)
}

// GetJWTManager 获取 JWT 管理器（用于其他地方验证 token）
func (a *Web3Authenticator) GetJWTManager() *JWTManager {
	return a.jwtManager
}

// GetChallengeStore 获取挑战存储（用于 Web3 Handler）
func (a *Web3Authenticator) GetChallengeStore() *ChallengeStore {
	return a.challengeStore
}

// GetEthereumSigner 获取以太坊签名器
func (a *Web3Authenticator) GetEthereumSigner() *crypto.EthereumSigner {
	return a.ethSigner
}
