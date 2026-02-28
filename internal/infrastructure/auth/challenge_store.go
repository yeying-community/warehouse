package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/auth"
)

// ChallengeStore 挑战存储
type ChallengeStore struct {
	challenges map[string]*auth.Challenge
	mu         sync.RWMutex
}

// NewChallengeStore 创建挑战存储
func NewChallengeStore() *ChallengeStore {
	store := &ChallengeStore{
		challenges: make(map[string]*auth.Challenge),
	}

	// 启动清理协程
	go store.cleanupExpired()

	return store
}

// Create 创建挑战
func (s *ChallengeStore) Create(address string, expiresIn time.Duration) (*auth.Challenge, error) {
	nonce, err := generateNonce()
	if err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	now := time.Now()
	message := buildChallengeMessage(address, nonce, now)

	challenge := &auth.Challenge{
		Nonce:     nonce,
		Message:   message,
		Address:   strings.ToLower(address),
		ExpiresAt: now.Add(expiresIn),
	}

	s.Store(challenge)

	return challenge, nil
}

// Store 存储挑战
func (s *ChallengeStore) Store(challenge *auth.Challenge) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := strings.ToLower(challenge.Address)
	s.challenges[key] = challenge
}

// Get 获取挑战
func (s *ChallengeStore) Get(address string) (*auth.Challenge, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := strings.ToLower(address)
	challenge, ok := s.challenges[key]

	if !ok {
		return nil, false
	}

	// 检查是否过期
	if challenge.IsExpired() {
		return nil, false
	}

	return challenge, true
}

// Delete 删除挑战
func (s *ChallengeStore) Delete(address string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := strings.ToLower(address)
	delete(s.challenges, key)
}

// cleanupExpired 清理过期挑战
func (s *ChallengeStore) cleanupExpired() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for key, challenge := range s.challenges {
			if now.After(challenge.ExpiresAt) {
				delete(s.challenges, key)
			}
		}
		s.mu.Unlock()
	}
}

// generateNonce 生成随机 nonce
func generateNonce() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// buildChallengeMessage 构建挑战消息
func buildChallengeMessage(address, nonce string, timestamp time.Time) string {
	return fmt.Sprintf(
		"Welcome to Warehouse!\n\n"+
			"Sign this message to authenticate.\n\n"+
			"Address: %s\n"+
			"Nonce: %s\n"+
			"Timestamp: %s\n\n"+
			"This request will not trigger a blockchain transaction or cost any gas fees.",
		address,
		nonce,
		timestamp.Format(time.RFC3339),
	)
}
