package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/yeying-community/webdav/internal/domain/auth"
)

// JWTManager JWT 管理器
type JWTManager struct {
	secret     []byte
	expiration time.Duration
	issuer     string
}

const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

// Claims JWT 声明
type Claims struct {
	Address     string `json:"address,omitempty"`
	Email       string `json:"email,omitempty"`
	SubjectType string `json:"subject_type,omitempty"`
	TokenType   string `json:"token_type,omitempty"`
	jwt.RegisteredClaims
}

// NewJWTManager 创建 JWT 管理器
func NewJWTManager(secret string, expiration time.Duration) *JWTManager {
	return &JWTManager{
		secret:     []byte(secret),
		expiration: expiration,
		issuer:     "webdav-server",
	}
}

// Generate 生成 JWT
func (m *JWTManager) Generate(address string) (*auth.Token, error) {
	return m.generate(address, "", "", TokenTypeAccess, m.expiration)
}

func (m *JWTManager) GenerateRefresh(address string, expiration time.Duration) (*auth.Token, error) {
	return m.generate(address, "", "", TokenTypeRefresh, expiration)
}

func (m *JWTManager) generate(address, email, subjectType, tokenType string, expiration time.Duration) (*auth.Token, error) {
	now := time.Now()
	expiresAt := now.Add(expiration)

	subject := address
	if subject == "" {
		subject = email
	}

	claims := Claims{
		Address:     strings.ToLower(address),
		Email:       strings.ToLower(strings.TrimSpace(email)),
		SubjectType: subjectType,
		TokenType:   tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    m.issuer,
			Subject:   subject,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(m.secret)
	if err != nil {
		return nil, fmt.Errorf("failed to sign token: %w", err)
	}

	return &auth.Token{
		Value:     tokenString,
		Address:   subject,
		ExpiresAt: expiresAt,
		IssuedAt:  now,
	}, nil
}

// GenerateForEmail 生成邮箱登录 JWT
func (m *JWTManager) GenerateForEmail(email string) (*auth.Token, error) {
	return m.generate("", email, "email", TokenTypeAccess, m.expiration)
}

// GenerateRefreshForEmail 生成邮箱登录刷新 token
func (m *JWTManager) GenerateRefreshForEmail(email string, expiration time.Duration) (*auth.Token, error) {
	return m.generate("", email, "email", TokenTypeRefresh, expiration)
}

// Verify 验证 JWT
func (m *JWTManager) Verify(tokenString string) (string, error) {
	claims, err := m.verifyClaims(tokenString, TokenTypeAccess, true)
	if err != nil {
		return "", err
	}
	return claims.Address, nil
}

func (m *JWTManager) VerifyRefresh(tokenString string) (string, error) {
	claims, err := m.verifyClaims(tokenString, TokenTypeRefresh, false)
	if err != nil {
		return "", err
	}
	return claims.Address, nil
}

// VerifyClaims 验证 access token 并返回 Claims
func (m *JWTManager) VerifyClaims(tokenString string) (*Claims, error) {
	return m.verifyClaims(tokenString, TokenTypeAccess, true)
}

// VerifyRefreshClaims 验证 refresh token 并返回 Claims
func (m *JWTManager) VerifyRefreshClaims(tokenString string) (*Claims, error) {
	return m.verifyClaims(tokenString, TokenTypeRefresh, false)
}

func (m *JWTManager) verifyClaims(tokenString, expectedType string, allowEmptyType bool) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// 验证签名方法
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return m.secret, nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, auth.ErrTokenExpired
		}
		return nil, fmt.Errorf("%w: %v", auth.ErrInvalidToken, err)
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		if claims.TokenType == "" && allowEmptyType {
			return claims, nil
		}
		if claims.TokenType != expectedType {
			return nil, auth.ErrInvalidToken
		}
		return claims, nil
	}

	return nil, auth.ErrInvalidToken
}
