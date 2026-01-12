package dto

import "time"

// ChallengeRequest 挑战请求
type ChallengeRequest struct {
	Address string `json:"address"`
}

// ChallengeResponse 挑战响应
type ChallengeResponse struct {
	Nonce     string    `json:"nonce"`
	Message   string    `json:"message"`
	ExpiresAt time.Time `json:"expires_at"`
}

// VerifyRequest 验证请求
type VerifyRequest struct {
	Address   string `json:"address"`
	Signature string `json:"signature"`
}

// VerifyResponse 验证响应
type VerifyResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      *UserInfo `json:"user"`
}

// UserInfo 用户信息
type UserInfo struct {
	Username      string   `json:"username"`
	WalletAddress string   `json:"wallet_address,omitempty"`
	Permissions   []string `json:"permissions"`
	CreatedAt     string   `json:"created_at,omitempty"`
}
