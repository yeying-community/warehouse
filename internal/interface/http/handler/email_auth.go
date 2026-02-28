package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/application/assetspace"
	"github.com/yeying-community/warehouse/internal/domain/user"
	infraAuth "github.com/yeying-community/warehouse/internal/infrastructure/auth"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/email"
	"go.uber.org/zap"
)

// EmailAuthHandler 邮箱验证码登录处理器
type EmailAuthHandler struct {
	web3Auth          *infraAuth.Web3Authenticator
	userRepo          user.Repository
	assetSpaceManager *assetspace.Manager
	store             *infraAuth.EmailCodeStore
	sender            *email.Sender
	config            config.EmailConfig
	logger            *zap.Logger
}

// NewEmailAuthHandler 创建邮箱验证码登录处理器
func NewEmailAuthHandler(
	web3Auth *infraAuth.Web3Authenticator,
	userRepo user.Repository,
	assetSpaceManager *assetspace.Manager,
	store *infraAuth.EmailCodeStore,
	sender *email.Sender,
	cfg config.EmailConfig,
	logger *zap.Logger,
) *EmailAuthHandler {
	return &EmailAuthHandler{
		web3Auth:          web3Auth,
		userRepo:          userRepo,
		assetSpaceManager: assetSpaceManager,
		store:             store,
		sender:            sender,
		config:            cfg,
		logger:            logger,
	}
}

// HandleSendCode 发送邮箱验证码
func (h *EmailAuthHandler) HandleSendCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST method is allowed")
		return
	}

	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid request body", zap.Error(err))
		h.sendError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	emailAddr := strings.ToLower(strings.TrimSpace(req.Email))
	if !user.IsValidEmail(emailAddr) {
		h.sendError(w, http.StatusBadRequest, "INVALID_EMAIL", "Invalid email address")
		return
	}

	if !h.config.Enabled {
		h.sendError(w, http.StatusForbidden, "EMAIL_DISABLED", "Email login is disabled")
		return
	}

	code, expiresAt, retryAfter, err := h.store.Create(emailAddr, h.config.CodeLength, h.config.CodeTTL, h.config.SendInterval)
	if err != nil {
		if errors.Is(err, infraAuth.ErrEmailCodeTooFrequent) {
			h.sendSDKResponse(w, http.StatusTooManyRequests, http.StatusTooManyRequests, "Too many requests", map[string]any{
				"retry_after": int(retryAfter.Seconds()),
			})
			return
		}
		h.logger.Error("failed to create email code", zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to send code")
		return
	}

	if err := h.sender.SendCode(emailAddr, code, h.config.CodeTTL); err != nil {
		h.logger.Error("failed to send email code", zap.Error(err))
		h.store.Delete(emailAddr)
		h.sendError(w, http.StatusInternalServerError, "SEND_FAILED", "Failed to send code")
		return
	}

	data := map[string]any{
		"email":      emailAddr,
		"expiresAt":  expiresAt.UnixMilli(),
		"retryAfter": int(h.config.SendInterval.Seconds()),
	}
	h.sendSDKSuccess(w, data)
}

// HandleLogin 邮箱验证码登录
func (h *EmailAuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST method is allowed")
		return
	}

	var req struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid request body", zap.Error(err))
		h.sendError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	emailAddr := strings.ToLower(strings.TrimSpace(req.Email))
	if !user.IsValidEmail(emailAddr) || strings.TrimSpace(req.Code) == "" {
		h.sendError(w, http.StatusBadRequest, "INVALID_REQUEST", "Email and code are required")
		return
	}

	if !h.config.Enabled {
		h.sendError(w, http.StatusForbidden, "EMAIL_DISABLED", "Email login is disabled")
		return
	}

	if ok := h.store.Verify(emailAddr, req.Code); !ok {
		h.sendError(w, http.StatusUnauthorized, "INVALID_CODE", "Invalid or expired code")
		return
	}

	ctx := r.Context()
	u, err := h.userRepo.FindByEmail(ctx, emailAddr)
	if err != nil {
		if err != user.ErrUserNotFound {
			h.logger.Error("failed to find user", zap.Error(err))
			h.sendError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to process request")
			return
		}
		if !h.config.AutoCreateOnLogin {
			h.sendError(w, http.StatusNotFound, "USER_NOT_FOUND", "User not found")
			return
		}
		u, err = h.createUserFromEmail(ctx, emailAddr)
		if err != nil {
			h.logger.Error("failed to create user from email", zap.Error(err))
			h.sendError(w, http.StatusInternalServerError, "USER_CREATE_FAILED", "Failed to create user")
			return
		}
	}

	if err := h.ensureAssetSpaces(u); err != nil {
		h.sendError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to initialize user spaces")
		return
	}

	accessToken, err := h.web3Auth.GenerateAccessTokenForEmail(emailAddr)
	if err != nil {
		h.logger.Error("failed to generate access token", zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "TOKEN_GENERATION_FAILED", "Failed to generate token")
		return
	}

	refreshToken, err := h.web3Auth.GenerateRefreshTokenForEmail(emailAddr)
	if err != nil {
		h.logger.Error("failed to generate refresh token", zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "REFRESH_TOKEN_FAILED", "Failed to generate refresh token")
		return
	}

	h.setRefreshCookie(w, r, refreshToken.Value, refreshToken.ExpiresAt)

	data := map[string]any{
		"email":            emailAddr,
		"username":         u.Username,
		"token":            accessToken.Value,
		"expiresAt":        accessToken.ExpiresAt.UnixMilli(),
		"refreshExpiresAt": refreshToken.ExpiresAt.UnixMilli(),
	}

	h.sendSDKSuccess(w, data)
}

func (h *EmailAuthHandler) createUserFromEmail(ctx context.Context, emailAddr string) (*user.User, error) {
	base := sanitizeEmailUsername(emailAddr)
	if base == "" {
		base = "user"
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	for attempt := 0; attempt < 5; attempt++ {
		username := base
		if attempt > 0 {
			username = fmt.Sprintf("%s%d", base, rng.Intn(1000))
		}

		u := user.NewUser(username, username)
		if err := u.SetEmail(emailAddr); err != nil {
			return nil, err
		}
		u.Permissions = user.ParsePermissions("CRUD")
		_ = u.SetQuota(1073741824)

		if err := h.userRepo.Save(ctx, u); err != nil {
			if err == user.ErrDuplicateUsername {
				continue
			}
			if err == user.ErrDuplicateEmail {
				return h.userRepo.FindByEmail(ctx, emailAddr)
			}
			return nil, err
		}

		h.logger.Info("user created via email",
			zap.String("username", u.Username),
			zap.String("email", emailAddr))

		return u, nil
	}

	return nil, fmt.Errorf("failed to create user: duplicate username")
}

func (h *EmailAuthHandler) ensureAssetSpaces(u *user.User) error {
	if h == nil || h.assetSpaceManager == nil || u == nil {
		return nil
	}
	if err := h.assetSpaceManager.EnsureForUser(u); err != nil {
		h.logger.Error("failed to ensure user asset spaces",
			zap.String("username", u.Username),
			zap.String("directory", u.Directory),
			zap.Error(err))
		return err
	}
	return nil
}

func sanitizeEmailUsername(emailAddr string) string {
	localPart := strings.Split(strings.TrimSpace(emailAddr), "@")[0]
	localPart = strings.ToLower(localPart)
	var builder strings.Builder
	for _, r := range localPart {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			builder.WriteRune(r)
			continue
		}
		if r == '.' || r == '-' || r == '_' {
			builder.WriteRune(r)
		}
	}
	return strings.Trim(builder.String(), "._-")
}

func (h *EmailAuthHandler) setRefreshCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
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

func (h *EmailAuthHandler) sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
	}
}

func (h *EmailAuthHandler) sendSDKSuccess(w http.ResponseWriter, data interface{}) {
	h.sendSDKResponse(w, http.StatusOK, 0, "ok", data)
}

func (h *EmailAuthHandler) sendSDKResponse(w http.ResponseWriter, status int, code int, message string, data interface{}) {
	response := sdkResponse{
		Code:      code,
		Message:   message,
		Data:      data,
		Timestamp: time.Now().UnixMilli(),
	}
	h.sendJSON(w, status, response)
}

func (h *EmailAuthHandler) sendError(w http.ResponseWriter, status int, code, message string) {
	h.sendSDKResponse(w, status, status, message, nil)
}
