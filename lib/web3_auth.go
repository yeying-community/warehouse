package lib

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
)

// Challenge 存储结构
type Challenge struct {
	Message   string
	Timestamp time.Time
}

// Web3Auth Web3 认证管理器
type Web3Auth struct {
	jwtSecret  []byte
	challenges sync.Map // map[string]*Challenge
	mu         sync.RWMutex
}

// NewWeb3Auth 创建 Web3 认证管理器
func NewWeb3Auth(jwtSecret string) *Web3Auth {
	if jwtSecret == "" {
		jwtSecret = generateRandomSecret()
		zap.L().Warn("using random JWT secret, tokens will be invalid after restart")
	}

	auth := &Web3Auth{
		jwtSecret: []byte(jwtSecret),
	}

	// 启动清理过期 Challenge 的协程
	go auth.cleanupExpiredChallenges()

	return auth
}

// generateRandomSecret 生成随机密钥
func generateRandomSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// GenerateChallenge 生成登录挑战
func (w *Web3Auth) GenerateChallenge(address string) (string, error) {
	if !common.IsHexAddress(address) {
		return "", fmt.Errorf("invalid ethereum address")
	}

	// 规范化地址
	addr := common.HexToAddress(address).Hex()

	// 生成随机 nonce
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	// 构造挑战消息
	message := fmt.Sprintf(
		"请签名以登录 YeYing WebDAV\n\n地址: %s\n随机数: %s\n时间戳: %d",
		addr,
		hex.EncodeToString(nonce),
		time.Now().Unix(),
	)

	// 存储挑战（5分钟有效期）
	w.challenges.Store(addr, &Challenge{
		Message:   message,
		Timestamp: time.Now(),
	})

	zap.L().Debug("generated challenge", zap.String("address", addr))

	return message, nil
}

// VerifySignature 验证签名并生成 JWT
func (w *Web3Auth) VerifySignature(address, signature string) (string, error) {
	if !common.IsHexAddress(address) {
		return "", fmt.Errorf("invalid ethereum address")
	}

	addr := common.HexToAddress(address).Hex()

	// 获取挑战
	value, ok := w.challenges.Load(addr)
	if !ok {
		return "", fmt.Errorf("challenge not found or expired")
	}

	challenge := value.(*Challenge)

	// 检查过期（5分钟）
	if time.Since(challenge.Timestamp) > 5*time.Minute {
		w.challenges.Delete(addr)
		return "", fmt.Errorf("challenge expired")
	}

	// 验证签名
	if err := w.verifyEthereumSignature(challenge.Message, signature, addr); err != nil {
		zap.L().Warn("signature verification failed",
			zap.String("address", addr),
			zap.Error(err))
		return "", fmt.Errorf("signature verification failed: %w", err)
	}

	// 删除已使用的挑战
	w.challenges.Delete(addr)

	// 生成 JWT
	token, err := w.generateJWT(addr)
	if err != nil {
		return "", err
	}

	zap.L().Info("user authenticated via web3",
		zap.String("address", addr))

	return token, nil
}

// verifyEthereumSignature 验证以太坊签名
func (w *Web3Auth) verifyEthereumSignature(message, signatureHex, expectedAddress string) error {
	// 解码签名
	signature, err := hexutil.Decode(signatureHex)
	if err != nil {
		return fmt.Errorf("invalid signature format: %w", err)
	}

	// 调整 v 值（MetaMask 等钱包使用 27/28，需要转换为 0/1）
	if signature[64] >= 27 {
		signature[64] -= 27
	}

	// 计算消息哈希（以太坊签名前缀）
	messageHash := crypto.Keccak256Hash(
		[]byte(fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)),
	)

	// 恢复公钥
	pubKey, err := crypto.SigToPub(messageHash.Bytes(), signature)
	if err != nil {
		return fmt.Errorf("failed to recover public key: %w", err)
	}

	// 从公钥恢复地址
	recoveredAddr := crypto.PubkeyToAddress(*pubKey)

	// 比较地址
	if recoveredAddr.Hex() != expectedAddress {
		return fmt.Errorf("address mismatch: expected %s, got %s",
			expectedAddress, recoveredAddr.Hex())
	}

	return nil
}

// generateJWT 生成 JWT Token
func (w *Web3Auth) generateJWT(address string) (string, error) {
	claims := jwt.MapClaims{
		"address": address,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
		"iat":     time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(w.jwtSecret)
}

// ValidateJWT 验证 JWT Token
func (w *Web3Auth) ValidateJWT(tokenString string) (string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return w.jwtSecret, nil
	})

	if err != nil {
		return "", err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		if address, ok := claims["address"].(string); ok {
			return address, nil
		}
		return "", fmt.Errorf("invalid token claims")
	}

	return "", fmt.Errorf("invalid token")
}

// cleanupExpiredChallenges 定期清理过期的挑战
func (w *Web3Auth) cleanupExpiredChallenges() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		w.challenges.Range(func(key, value interface{}) bool {
			challenge := value.(*Challenge)
			if now.Sub(challenge.Timestamp) > 5*time.Minute {
				w.challenges.Delete(key)
				zap.L().Debug("cleaned up expired challenge",
					zap.String("address", key.(string)))
			}
			return true
		})
	}
}

