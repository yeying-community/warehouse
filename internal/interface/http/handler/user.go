package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/crypto"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

// UserHandler 用户信息处理器
type UserHandler struct {
	logger         *zap.Logger
	userRepository user.Repository
	passwordHasher *crypto.PasswordHasher
}

// NewUserHandler 创建用户信息处理器
func NewUserHandler(logger *zap.Logger, userRepo user.Repository) *UserHandler {
	return &UserHandler{
		logger:         logger,
		userRepository: userRepo,
		passwordHasher: crypto.NewPasswordHasher(),
	}
}

// UserInfoResponse 用户信息响应
type UserInfoResponse struct {
	Username      string   `json:"username"`
	WalletAddress string   `json:"wallet_address,omitempty"`
	Email         string   `json:"email,omitempty"`
	Permissions   []string `json:"permissions"`
	CreatedAt     string   `json:"created_at,omitempty"`
	UpdatedAt     string   `json:"updated_at,omitempty"`
	HasPassword   bool     `json:"has_password"`
}

// GetUserInfo 获取用户信息
func (h *UserHandler) GetUserInfo(w http.ResponseWriter, r *http.Request) {
	// 只允许 GET 请求
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// 从上下文获取用户
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		h.logger.Error("user not found in context")
		h.writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	response := UserInfoResponse{
		Username:      u.Username,
		WalletAddress: u.WalletAddress,
		Email:         u.Email,
		Permissions:   permissionsToStrings(u.Permissions),
		HasPassword:   u.HasPassword(),
	}

	if !u.CreatedAt.IsZero() {
		response.CreatedAt = u.CreatedAt.Format(timeLayout)
	}
	if !u.UpdatedAt.IsZero() {
		response.UpdatedAt = u.UpdatedAt.Format(timeLayout)
	}

	h.writeJSON(w, http.StatusOK, response)
}

// UpdateUsername 更新用户名
func (h *UserHandler) UpdateUsername(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		h.logger.Error("user not found in context")
		h.writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid request body", zap.Error(err))
		h.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	newName := strings.TrimSpace(req.Username)
	if newName == "" {
		h.writeError(w, http.StatusBadRequest, "Username is required")
		return
	}

	current, err := h.userRepository.FindByID(r.Context(), u.ID)
	if err != nil {
		h.logger.Error("failed to find user", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to find user")
		return
	}

	if current.Username == newName {
		h.writeJSON(w, http.StatusOK, map[string]string{"username": current.Username})
		return
	}

	// 保持目录不变，避免影响存储路径
	originalDirectory := current.Directory
	if originalDirectory == "" {
		originalDirectory = current.Username
	}

	current.Username = newName
	current.Directory = originalDirectory
	current.UpdatedAt = time.Now()

	if err := h.userRepository.Save(r.Context(), current); err != nil {
		if err == user.ErrDuplicateUsername {
			h.writeError(w, http.StatusConflict, "Username already exists")
			return
		}
		h.logger.Error("failed to update username", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to update username")
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]string{"username": current.Username})
}

// UpdatePassword 设置/修改密码
func (h *UserHandler) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		h.logger.Error("user not found in context")
		h.writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid request body", zap.Error(err))
		h.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	newPassword := strings.TrimSpace(req.NewPassword)
	if newPassword == "" || len(newPassword) < 6 {
		h.writeError(w, http.StatusBadRequest, "New password is invalid")
		return
	}

	current, err := h.userRepository.FindByID(r.Context(), u.ID)
	if err != nil {
		h.logger.Error("failed to find user", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to find user")
		return
	}

	if current.HasPassword() {
		if strings.TrimSpace(req.OldPassword) == "" {
			h.writeError(w, http.StatusBadRequest, "Old password is required")
			return
		}
		if err := h.passwordHasher.Verify(current.Password, req.OldPassword); err != nil {
			h.writeError(w, http.StatusUnauthorized, "Old password is incorrect")
			return
		}
	}

	hashed, err := h.passwordHasher.Hash(newPassword)
	if err != nil {
		h.logger.Error("failed to hash password", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to update password")
		return
	}

	current.SetPassword(hashed)
	if err := h.userRepository.Save(r.Context(), current); err != nil {
		h.logger.Error("failed to update password", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to update password")
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func permissionsToStrings(perms *user.Permissions) []string {
	if perms == nil {
		return []string{}
	}
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

// writeJSON 写入 JSON 响应
func (h *UserHandler) writeJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
	}
}

// writeError 写入错误响应
func (h *UserHandler) writeError(w http.ResponseWriter, code int, message string) {
	h.writeJSON(w, code, map[string]interface{}{
		"error":   message,
		"code":    code,
		"success": false,
	})
}
