package lib

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"
)

// ChallengeRequest 挑战请求
type ChallengeRequest struct {
	Address string `json:"address"`
}

// ChallengeResponse 挑战响应
type ChallengeResponse struct {
	Challenge string `json:"challenge"`
}

// VerifyRequest 验证请求
type VerifyRequest struct {
	Address   string `json:"address"`
	Signature string `json:"signature"`
}

// VerifyResponse 验证响应
type VerifyResponse struct {
	Token string `json:"token"`
}

// ErrorResponse 错误响应
type ErrorResponse struct {
	Error string `json:"error"`
}

// handleChallenge 处理获取挑战请求
func (h *Handler) handleChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChallengeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Invalid request body"})
		return
	}

	if req.Address == "" {
		respondJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Address is required"})
		return
	}

	challenge, err := h.web3Auth.GenerateChallenge(req.Address)
	if err != nil {
		zap.L().Error("failed to generate challenge", zap.Error(err))
		respondJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, ChallengeResponse{Challenge: challenge})
}

// handleVerify 处理验证签名请求
func (h *Handler) handleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req VerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Invalid request body"})
		return
	}

	if req.Address == "" || req.Signature == "" {
		respondJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Address and signature are required"})
		return
	}

	token, err := h.web3Auth.VerifySignature(req.Address, req.Signature)
	if err != nil {
		zap.L().Warn("signature verification failed",
			zap.String("address", req.Address),
			zap.Error(err))
		respondJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "Signature verification failed"})
		return
	}

	respondJSON(w, http.StatusOK, VerifyResponse{Token: token})
}

// respondJSON 发送 JSON 响应
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

