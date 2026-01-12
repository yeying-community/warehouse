package handler

import (
	"encoding/json"
	"net/http"

	"github.com/yeying-community/webdav/internal/domain/user"
	"github.com/yeying-community/webdav/internal/interface/http/middleware"
	"go.uber.org/zap"
)

// UserHandler 用户信息处理器
type UserHandler struct {
	logger *zap.Logger
}

// NewUserHandler 创建用户信息处理器
func NewUserHandler(logger *zap.Logger) *UserHandler {
	return &UserHandler{
		logger: logger,
	}
}

// UserInfoResponse 用户信息响应
type UserInfoResponse struct {
	Username      string   `json:"username"`
	WalletAddress string   `json:"wallet_address,omitempty"`
	Permissions   []string `json:"permissions"`
	CreatedAt     string   `json:"created_at,omitempty"`
	UpdatedAt     string   `json:"updated_at,omitempty"`
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
		Permissions:   permissionsToStrings(u.Permissions),
	}

	if !u.CreatedAt.IsZero() {
		response.CreatedAt = u.CreatedAt.Format(timeLayout)
	}
	if !u.UpdatedAt.IsZero() {
		response.UpdatedAt = u.UpdatedAt.Format(timeLayout)
	}

	h.writeJSON(w, http.StatusOK, response)
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
