package handler

import (
	"encoding/json"
	"github.com/yeying-community/webdav/internal/domain/user"
	"github.com/yeying-community/webdav/internal/infrastructure/auth"
	"github.com/yeying-community/webdav/internal/infrastructure/crypto"
	"github.com/yeying-community/webdav/internal/interface/http/dto"
	"go.uber.org/zap"
	"golang.org/x/crypto/sha3"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Web3Handler Web3 认证处理器
type Web3Handler struct {
	web3Auth              *auth.Web3Authenticator
	userRepo              user.Repository
	logger                *zap.Logger
	autoCreateOnChallenge bool
}

const refreshTokenCookieName = "refresh_token"

type sdkResponse struct {
	Code      int         `json:"code"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data"`
	Timestamp int64       `json:"timestamp"`
}

// NewWeb3Handler 创建 Web3 处理器
func NewWeb3Handler(
	web3Auth *auth.Web3Authenticator,
	userRepo user.Repository,
	logger *zap.Logger,
	autoCreateOnChallenge bool,
) *Web3Handler {
	return &Web3Handler{
		web3Auth:              web3Auth,
		userRepo:              userRepo,
		logger:                logger,
		autoCreateOnChallenge: autoCreateOnChallenge,
	}
}

type AddressInfo struct {
	CoinBalance string `json:"coin_balance"`
}

// 获取以太坊钱包的账户余额是否大于0
func HasBalance(address string) bool {
	url := "https://blockscout.yeying.pub/backend/api/v2/addresses/" + address

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	var info AddressInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return false
	}

	balance, err := strconv.Atoi(info.CoinBalance)
	if err != nil {
		return false
	}

	return balance > 0
}

// 验证以太坊地址合法性
func IsValidAddress(address string) bool {
	// 1. 基础格式检查
	re := regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
	if !re.MatchString(address) {
		return false
	}

	// 2. EIP-55 校验和检查
	return verifyChecksum(address)
}

func verifyChecksum(address string) bool {
	address = strings.TrimPrefix(address, "0x")

	// 关键修复：计算哈希时使用全小写地址
	hash := sha3.NewLegacyKeccak256()
	hash.Write([]byte(strings.ToLower(address)))
	digest := hash.Sum(nil)

	for i := 0; i < 40; i++ {
		c := address[i]
		hashByte := digest[i/2]

		if i%2 == 0 {
			hashByte >>= 4
		} else {
			hashByte &= 0x0f
		}

		// 关键修复：正确的校验逻辑
		expectedUpper := (hashByte >= 8)
		isUpper := c >= 'A' && c <= 'F'

		// 如果是字母且大小写不匹配则返回false
		if (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			if isUpper != expectedUpper {
				return false
			}
		}
	}
	return true
}

// HandleChallenge 处理挑战请求
// GET /api/auth/challenge?address=0x123...
func (h *Web3Handler) HandleChallenge(w http.ResponseWriter, r *http.Request) {
	var address string

	// 获取地址参数
	switch r.Method {
	case http.MethodGet:
		address = r.URL.Query().Get("address")

	case http.MethodPost:
		var req dto.ChallengeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.sendError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
			return
		}
		address = req.Address

	default:
		h.sendError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET and POST methods are allowed")
		return
	}

	if address == "" {
		h.sendError(w, http.StatusBadRequest, "MISSING_ADDRESS", "Address parameter is required")
		return
	}

	if !IsValidAddress(address) {
		h.sendError(w, http.StatusBadRequest, "MISSING_ADDRESS", "Address parameter is invalid, address "+address)
		return
	}

	// 规范化地址
	address = strings.ToLower(strings.TrimSpace(address))

	// 短期内不验证余额，先跳过
	skip := false
	// 检查当前钱包账户地址是否有余额
	if skip && !HasBalance(address) {
		h.logger.Error("The balance of the web3 wallet account is 0", zap.String("address", address))
		h.sendError(w, http.StatusInternalServerError, "BALANCE_FETCH_FAIL", "The balance of the web3 wallet account is 0")
		return
	} else {
		if h.autoCreateOnChallenge {
			// 注册钱包账户（不存在则自动创建）
			if _, err := h.web3Auth.EnsureUserByWallet(r.Context(), address, true); err != nil {
				h.logger.Error("failed to ensure wallet user", zap.String("address", address), zap.Error(err))
				h.sendError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to process request")
				return
			}
		}
	}

	// 检查用户是否存在
	ctx := r.Context()
	u, err := h.userRepo.FindByWalletAddress(ctx, address)
	if err != nil {
		if err == user.ErrUserNotFound {
			h.logger.Info("wallet address not registered", zap.String("address", address))
			h.sendError(w, http.StatusNotFound, "USER_NOT_FOUND", "Wallet address not registered")
			return
		}

		h.logger.Error("failed to find user", zap.String("address", address), zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to process request")
		return
	}

	// 创建挑战
	challenge, err := h.web3Auth.CreateChallenge(address)
	if err != nil {
		h.logger.Error("failed to create challenge", zap.String("address", address), zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "CHALLENGE_CREATION_FAILED", "Failed to create challenge")
		return
	}

	h.logger.Info("challenge created",
		zap.String("address", address),
		zap.String("username", u.Username),
		zap.String("nonce", challenge.Nonce))

	// 返回挑战
	data := map[string]interface{}{
		"address":   address,
		"challenge": challenge.Message,
		"nonce":     challenge.Nonce,
		"issuedAt":  time.Now().UnixMilli(),
		"expiresAt": challenge.ExpiresAt.UnixMilli(),
	}

	h.sendSDKSuccess(w, data)
}

// HandleVerify 处理验证请求
// POST /api/auth/verify
func (h *Web3Handler) HandleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST method is allowed")
		return
	}

	// 解析请求
	var req dto.VerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid request body", zap.Error(err))
		h.sendError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	// 验证必填字段
	if req.Address == "" {
		h.sendError(w, http.StatusBadRequest, "MISSING_ADDRESS", "Address is required")
		return
	}

	if req.Signature == "" {
		h.sendError(w, http.StatusBadRequest, "MISSING_SIGNATURE", "Signature is required")
		return
	}

	// 规范化地址
	req.Address = strings.ToLower(strings.TrimSpace(req.Address))

	// 查找用户
	ctx := r.Context()
	u, err := h.userRepo.FindByWalletAddress(ctx, req.Address)
	if err != nil {
		if err == user.ErrUserNotFound {
			h.logger.Info("wallet address not registered", zap.String("address", req.Address))
			h.sendError(w, http.StatusNotFound, "USER_NOT_FOUND", "Wallet address not registered")
			return
		}

		h.logger.Error("failed to find user", zap.String("address", req.Address), zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to process request")
		return
	}

	// 验证签名并生成 token
	token, err := h.web3Auth.VerifySignature(ctx, req.Address, req.Signature)
	if err != nil {
		h.logger.Warn("signature verification failed",
			zap.String("address", req.Address),
			zap.Error(err))
		h.sendError(w, http.StatusUnauthorized, "INVALID_SIGNATURE", "Signature verification failed")
		return
	}

	h.logger.Info("user authenticated via web3",
		zap.String("address", req.Address),
		zap.String("username", u.Username))

	refreshToken, err := h.web3Auth.GenerateRefreshToken(req.Address)
	if err != nil {
		h.logger.Error("failed to generate refresh token", zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "REFRESH_TOKEN_FAILED", "Failed to generate refresh token")
		return
	}

	h.setRefreshCookie(w, r, refreshToken.Value, refreshToken.ExpiresAt)

	data := map[string]interface{}{
		"address":          req.Address,
		"token":            token.Value,
		"expiresAt":        token.ExpiresAt.UnixMilli(),
		"refreshExpiresAt": refreshToken.ExpiresAt.UnixMilli(),
	}

	h.sendSDKSuccess(w, data)
}

// HandlePasswordLogin 用户名密码登录
func (h *Web3Handler) HandlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST method is allowed")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid request body", zap.Error(err))
		h.sendError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	username := strings.TrimSpace(req.Username)
	if username == "" || req.Password == "" {
		h.sendError(w, http.StatusBadRequest, "MISSING_CREDENTIALS", "Username and password are required")
		return
	}

	ctx := r.Context()
	u, err := h.userRepo.FindByUsername(ctx, username)
	if err != nil {
		if err == user.ErrUserNotFound {
			h.sendError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid username or password")
			return
		}
		h.logger.Error("failed to find user", zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to process request")
		return
	}

	if !u.HasPassword() {
		h.sendError(w, http.StatusUnauthorized, "NO_PASSWORD", "Password not set")
		return
	}

	hasher := crypto.NewPasswordHasher()
	if err := hasher.Verify(u.Password, req.Password); err != nil {
		h.sendError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid username or password")
		return
	}

	if strings.TrimSpace(u.WalletAddress) == "" {
		h.sendError(w, http.StatusBadRequest, "NO_WALLET", "Wallet address not bound")
		return
	}

	accessToken, err := h.web3Auth.GenerateAccessToken(u.WalletAddress)
	if err != nil {
		h.logger.Error("failed to generate access token", zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "TOKEN_GENERATION_FAILED", "Failed to generate token")
		return
	}

	refreshToken, err := h.web3Auth.GenerateRefreshToken(u.WalletAddress)
	if err != nil {
		h.logger.Error("failed to generate refresh token", zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "REFRESH_TOKEN_FAILED", "Failed to generate refresh token")
		return
	}

	h.setRefreshCookie(w, r, refreshToken.Value, refreshToken.ExpiresAt)

	data := map[string]interface{}{
		"address":          u.WalletAddress,
		"username":         u.Username,
		"token":            accessToken.Value,
		"expiresAt":        accessToken.ExpiresAt.UnixMilli(),
		"refreshExpiresAt": refreshToken.ExpiresAt.UnixMilli(),
	}

	h.sendSDKSuccess(w, data)
}

func (h *Web3Handler) HandleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST method is allowed")
		return
	}

	cookie, err := r.Cookie(refreshTokenCookieName)
	if err != nil || cookie.Value == "" {
		h.sendError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Refresh token is required")
		return
	}

	address, err := h.web3Auth.VerifyRefreshToken(cookie.Value)
	if err != nil {
		h.logger.Warn("invalid refresh token", zap.Error(err))
		h.sendError(w, http.StatusUnauthorized, "INVALID_REFRESH_TOKEN", "Invalid refresh token")
		return
	}

	// 确认用户存在
	ctx := r.Context()
	if _, err := h.userRepo.FindByWalletAddress(ctx, address); err != nil {
		if err == user.ErrUserNotFound {
			h.sendError(w, http.StatusNotFound, "USER_NOT_FOUND", "Wallet address not registered")
			return
		}
		h.logger.Error("failed to find user", zap.String("address", address), zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to process request")
		return
	}

	accessToken, err := h.web3Auth.GenerateAccessToken(address)
	if err != nil {
		h.logger.Error("failed to generate access token", zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "TOKEN_GENERATION_FAILED", "Failed to generate token")
		return
	}

	refreshToken, err := h.web3Auth.GenerateRefreshToken(address)
	if err != nil {
		h.logger.Error("failed to generate refresh token", zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "REFRESH_TOKEN_FAILED", "Failed to generate refresh token")
		return
	}

	h.setRefreshCookie(w, r, refreshToken.Value, refreshToken.ExpiresAt)

	data := map[string]interface{}{
		"address":          address,
		"token":            accessToken.Value,
		"expiresAt":        accessToken.ExpiresAt.UnixMilli(),
		"refreshExpiresAt": refreshToken.ExpiresAt.UnixMilli(),
	}

	h.sendSDKSuccess(w, data)
}

func (h *Web3Handler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST method is allowed")
		return
	}

	h.clearRefreshCookie(w, r)
	h.sendSDKSuccess(w, map[string]bool{"logout": true})
}

// getPermissionStrings 获取权限字符串列表
func (h *Web3Handler) getPermissionStrings(perms *user.Permissions) []string {
	var permissions []string

	if perms.Create {
		permissions = append(permissions, "create")
	}
	if perms.Read {
		permissions = append(permissions, "read")
	}
	if perms.Update {
		permissions = append(permissions, "update")
	}
	if perms.Delete {
		permissions = append(permissions, "delete")
	}

	return permissions
}

func (h *Web3Handler) setRefreshCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	secure := isSecureRequest(r)
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	http.SetCookie(w, &http.Cookie{
		Name:     refreshTokenCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

func (h *Web3Handler) clearRefreshCookie(w http.ResponseWriter, r *http.Request) {
	secure := isSecureRequest(r)
	http.SetCookie(w, &http.Cookie{
		Name:     refreshTokenCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		proto := strings.TrimSpace(parts[0])
		return strings.EqualFold(proto, "https")
	}
	return false
}

// sendJSON 发送 JSON 响应
func (h *Web3Handler) sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
	}
}

func (h *Web3Handler) sendSDKSuccess(w http.ResponseWriter, data interface{}) {
	h.sendSDKResponse(w, http.StatusOK, 0, "ok", data)
}

func (h *Web3Handler) sendSDKResponse(w http.ResponseWriter, status int, code int, message string, data interface{}) {
	response := sdkResponse{
		Code:      code,
		Message:   message,
		Data:      data,
		Timestamp: time.Now().UnixMilli(),
	}
	h.sendJSON(w, status, response)
}

// sendError 发送错误响应
func (h *Web3Handler) sendError(w http.ResponseWriter, status int, code, message string) {
	h.sendSDKResponse(w, status, status, message, nil)
}
