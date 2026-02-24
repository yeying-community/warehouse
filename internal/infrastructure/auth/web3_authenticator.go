package auth

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/yeying-community/webdav/internal/domain/auth"
	"github.com/yeying-community/webdav/internal/domain/user"
	"github.com/yeying-community/webdav/internal/infrastructure/crypto"
	"github.com/yeying-community/webdav/internal/interface/http/middleware"
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
	autoCreateOnUCAN  bool
}

// NewWeb3Authenticator 创建 Web3 认证器
func NewWeb3Authenticator(
	userRepo user.Repository,
	jwtSecret string,
	tokenExpiration time.Duration,
	refreshTokenExpiration time.Duration,
	ucanVerifier *UcanVerifier,
	logger *zap.Logger,
	autoCreateOnUCAN bool,
) *Web3Authenticator {
	return &Web3Authenticator{
		userRepo:          userRepo,
		jwtManager:        NewJWTManager(jwtSecret, tokenExpiration),
		ucanVerifier:      ucanVerifier,
		challengeStore:    NewChallengeStore(),
		ethSigner:         crypto.NewEthereumSigner(),
		logger:            logger,
		refreshExpiration: refreshTokenExpiration,
		autoCreateOnUCAN:  autoCreateOnUCAN,
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
	subject, subjectType, err := a.verifyTokenSubject(creds.Token)
	if err != nil {
		return nil, err
	}

	if subjectType == "email" {
		u, err := a.userRepo.FindByEmail(ctx, subject)
		if err != nil {
			if err == user.ErrUserNotFound {
				a.logger.Debug("email not found", zap.String("email", subject))
				return nil, err
			}
			return nil, err
		}
		a.logger.Debug("user authenticated via email token",
			zap.String("username", u.Username),
			zap.String("email", subject))
		return u, nil
	}

	isUcan := isUcanToken(creds.Token)
	u, err := a.EnsureUserByWallet(ctx, subject, isUcan && a.autoCreateOnUCAN)
	if err != nil {
		if err == user.ErrUserNotFound {
			a.logger.Debug("wallet address not found",
				zap.String("address", subject))
			return nil, err
		}
		return nil, err
	}

	a.logger.Debug("user authenticated via web3",
		zap.String("username", u.Username),
		zap.String("address", subject))

	return u, nil
}

func (a *Web3Authenticator) EnsureUserByWallet(ctx context.Context, address string, createIfMissing bool) (*user.User, error) {
	u, err := a.userRepo.FindByWalletAddress(ctx, address)
	if err == nil {
		return u, nil
	}
	if err == user.ErrUserNotFound {
		if createIfMissing {
			return a.createUserFromWallet(ctx, address)
		}
		return nil, err
	}
	return nil, fmt.Errorf("failed to find user: %w", err)
}

func (a *Web3Authenticator) createUserFromWallet(ctx context.Context, address string) (*user.User, error) {
	normalizedAddress := strings.ToLower(strings.TrimSpace(address))
	if normalizedAddress == "" {
		return nil, fmt.Errorf("invalid wallet address")
	}

	for attempt := 0; attempt < 5; attempt++ {
		username := generateHumanReadableName()
		u := user.NewUser(username, username)
		if err := u.SetWalletAddress(normalizedAddress); err != nil {
			return nil, err
		}
		u.Permissions = user.ParsePermissions("CRUD")
		_ = u.SetQuota(1073741824)

		if err := a.userRepo.Save(ctx, u); err != nil {
			if errors.Is(err, user.ErrDuplicateUsername) {
				continue
			}
			return nil, fmt.Errorf("failed to create user: %w", err)
		}

		a.logger.Info("user created via ucan",
			zap.String("username", u.Username),
			zap.String("address", normalizedAddress))

		return u, nil
	}

	return nil, fmt.Errorf("failed to create user: duplicate username")
}

var (
	ucanAdjectives = []string{"Quick", "Lazy", "Funny", "Serious", "Brave"}
	ucanNouns      = []string{"Fox", "Dog", "Cat", "Mouse", "Wolf"}
)

func generateHumanReadableName() string {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	adj := ucanAdjectives[rng.Intn(len(ucanAdjectives))]
	noun := ucanNouns[rng.Intn(len(ucanNouns))]
	num := rng.Intn(1000)
	return fmt.Sprintf("%s%s%d", adj, noun, num)
}

// EnrichContext attaches UCAN scope info to the request context.
func (a *Web3Authenticator) EnrichContext(ctx context.Context, credentials interface{}) context.Context {
	creds, ok := credentials.(*auth.BearerCredentials)
	if !ok || ctx == nil {
		return ctx
	}
	token := strings.TrimSpace(creds.Token)
	if token == "" || !isUcanToken(token) {
		return ctx
	}

	caps, err := parseUcanCaps(token)
	if err != nil {
		a.logger.Debug("failed to parse ucan caps", zap.Error(err))
		return middleware.WithUcanContext(ctx, &middleware.UcanContext{AppCaps: map[string][]string{}})
	}

	appCaps := extractAppCapsFromCaps(caps, "app:")
	return middleware.WithUcanContext(ctx, &middleware.UcanContext{AppCaps: appCaps})
}

func (a *Web3Authenticator) verifyTokenSubject(token string) (string, string, error) {
	if token == "" {
		return "", "", auth.ErrInvalidToken
	}

	if isUcanToken(token) {
		if a.ucanVerifier == nil || !a.ucanVerifier.Enabled() {
			return "", "", auth.ErrInvalidToken
		}
		address, err := a.ucanVerifier.VerifyInvocation(token)
		if err != nil {
			a.logger.Debug("ucan verification failed", zap.Error(err))
			return "", "", err
		}
		return address, "wallet", nil
	}

	claims, err := a.jwtManager.VerifyClaims(token)
	if err != nil {
		a.logger.Debug("jwt verification failed", zap.Error(err))
		return "", "", err
	}

	email := strings.TrimSpace(claims.Email)
	if claims.SubjectType == "email" && email != "" {
		return strings.ToLower(email), "email", nil
	}

	if claims.Address != "" {
		return strings.ToLower(claims.Address), "wallet", nil
	}

	if email != "" {
		return strings.ToLower(email), "email", nil
	}

	if claims.Subject != "" {
		return strings.ToLower(claims.Subject), "wallet", nil
	}

	return "", "", auth.ErrInvalidToken
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

// GenerateAccessTokenForEmail 生成邮箱登录访问令牌
func (a *Web3Authenticator) GenerateAccessTokenForEmail(email string) (*auth.Token, error) {
	return a.jwtManager.GenerateForEmail(email)
}

// GenerateRefreshToken 生成刷新令牌
func (a *Web3Authenticator) GenerateRefreshToken(address string) (*auth.Token, error) {
	return a.jwtManager.GenerateRefresh(address, a.refreshExpiration)
}

// GenerateRefreshTokenForEmail 生成邮箱登录刷新令牌
func (a *Web3Authenticator) GenerateRefreshTokenForEmail(email string) (*auth.Token, error) {
	return a.jwtManager.GenerateRefreshForEmail(email, a.refreshExpiration)
}

// VerifyRefreshToken 验证刷新令牌
func (a *Web3Authenticator) VerifyRefreshToken(token string) (string, error) {
	subject, _, err := a.VerifyRefreshTokenWithSubject(token)
	return subject, err
}

// VerifyRefreshTokenWithSubject 验证刷新令牌并返回类型
func (a *Web3Authenticator) VerifyRefreshTokenWithSubject(token string) (string, string, error) {
	claims, err := a.jwtManager.VerifyRefreshClaims(token)
	if err != nil {
		return "", "", err
	}

	email := strings.TrimSpace(claims.Email)
	if claims.SubjectType == "email" && email != "" {
		return strings.ToLower(email), "email", nil
	}
	if claims.Address != "" {
		return strings.ToLower(claims.Address), "wallet", nil
	}
	if email != "" {
		return strings.ToLower(email), "email", nil
	}
	if claims.Subject != "" {
		return strings.ToLower(claims.Subject), "wallet", nil
	}
	return "", "", auth.ErrInvalidToken
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
