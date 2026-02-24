package auth

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

// ErrEmailCodeTooFrequent 表示发送过于频繁
var ErrEmailCodeTooFrequent = errors.New("email code sent too frequently")

type EmailCode struct {
	Code       string
	ExpiresAt  time.Time
	LastSentAt time.Time
}

// EmailCodeStore 邮箱验证码存储
type EmailCodeStore struct {
	codes map[string]*EmailCode
	mu    sync.RWMutex
}

// NewEmailCodeStore 创建邮箱验证码存储
func NewEmailCodeStore() *EmailCodeStore {
	store := &EmailCodeStore{
		codes: make(map[string]*EmailCode),
	}

	go store.cleanupExpired()

	return store
}

// Create 生成验证码并存储
func (s *EmailCodeStore) Create(email string, codeLength int, ttl time.Duration, interval time.Duration) (string, time.Time, time.Duration, error) {
	if codeLength <= 0 {
		return "", time.Time{}, 0, fmt.Errorf("invalid code length")
	}
	key := normalizeEmail(email)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	if entry, ok := s.codes[key]; ok {
		if interval > 0 && now.Sub(entry.LastSentAt) < interval {
			retryAfter := interval - now.Sub(entry.LastSentAt)
			return "", time.Time{}, retryAfter, ErrEmailCodeTooFrequent
		}
	}

	code, err := generateNumericCode(codeLength)
	if err != nil {
		return "", time.Time{}, 0, err
	}

	expiresAt := now.Add(ttl)
	s.codes[key] = &EmailCode{
		Code:       code,
		ExpiresAt:  expiresAt,
		LastSentAt: now,
	}

	return code, expiresAt, 0, nil
}

// Verify 校验验证码（成功后即删除）
func (s *EmailCodeStore) Verify(email, code string) bool {
	key := normalizeEmail(email)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.codes[key]
	if !ok {
		return false
	}
	if now.After(entry.ExpiresAt) {
		delete(s.codes, key)
		return false
	}
	if strings.TrimSpace(code) == "" || entry.Code != strings.TrimSpace(code) {
		return false
	}

	delete(s.codes, key)
	return true
}

// Delete 删除指定邮箱的验证码
func (s *EmailCodeStore) Delete(email string) {
	key := normalizeEmail(email)
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.codes, key)
}

func (s *EmailCodeStore) cleanupExpired() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for key, entry := range s.codes {
			if now.After(entry.ExpiresAt) {
				delete(s.codes, key)
			}
		}
		s.mu.Unlock()
	}
}

func generateNumericCode(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("invalid code length")
	}
	max := big.NewInt(10)
	var builder strings.Builder
	builder.Grow(length)
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		builder.WriteByte(byte('0' + n.Int64()))
	}
	return builder.String(), nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
