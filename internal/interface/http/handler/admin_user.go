package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/yeying-community/webdav/internal/domain/user"
	"github.com/yeying-community/webdav/internal/infrastructure/crypto"
	"go.uber.org/zap"
)

// AdminUserHandler manages users with admin permissions.
type AdminUserHandler struct {
	logger         *zap.Logger
	userRepository user.Repository
	passwordHasher *crypto.PasswordHasher
}

// NewAdminUserHandler creates a new AdminUserHandler.
func NewAdminUserHandler(logger *zap.Logger, userRepo user.Repository) *AdminUserHandler {
	return &AdminUserHandler{
		logger:         logger,
		userRepository: userRepo,
		passwordHasher: crypto.NewPasswordHasher(),
	}
}

type adminRuleRequest struct {
	Path        string   `json:"path"`
	Permissions []string `json:"permissions"`
	Regex       bool     `json:"regex"`
}

type adminUserCreateRequest struct {
	Username      string             `json:"username"`
	Password      string             `json:"password,omitempty"`
	WalletAddress string             `json:"wallet_address,omitempty"`
	Directory     string             `json:"directory,omitempty"`
	Permissions   []string           `json:"permissions,omitempty"`
	Quota         *int64             `json:"quota,omitempty"`
	Rules         []adminRuleRequest `json:"rules,omitempty"`
}

type adminUserUpdateRequest struct {
	Username      string              `json:"username"`
	NewUsername   *string             `json:"new_username,omitempty"`
	WalletAddress *string             `json:"wallet_address,omitempty"`
	Directory     *string             `json:"directory,omitempty"`
	Permissions   []string            `json:"permissions,omitempty"`
	Quota         *int64              `json:"quota,omitempty"`
	Rules         *[]adminRuleRequest `json:"rules,omitempty"`
}

type adminUserDeleteRequest struct {
	Username string `json:"username"`
}

type adminUserResetPasswordRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type adminRuleResponse struct {
	Path        string   `json:"path"`
	Permissions []string `json:"permissions"`
	Regex       bool     `json:"regex"`
}

type adminUserResponse struct {
	ID            string              `json:"id"`
	Username      string              `json:"username"`
	WalletAddress string              `json:"wallet_address,omitempty"`
	Directory     string              `json:"directory"`
	Permissions   []string            `json:"permissions"`
	Quota         int64               `json:"quota"`
	UsedSpace     int64               `json:"used_space"`
	Rules         []adminRuleResponse `json:"rules,omitempty"`
	CreatedAt     string              `json:"created_at,omitempty"`
	UpdatedAt     string              `json:"updated_at,omitempty"`
	HasPassword   bool                `json:"has_password"`
}

// HandleList lists all users.
func (h *AdminUserHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	users, err := h.userRepository.List(r.Context())
	if err != nil {
		h.logger.Error("failed to list users", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to list users")
		return
	}

	resp := make([]adminUserResponse, 0, len(users))
	for _, u := range users {
		resp = append(resp, buildAdminUserResponse(u))
	}

	h.writeJSON(w, http.StatusOK, map[string]any{"items": resp})
}

// HandleCreate creates a user.
func (h *AdminUserHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req adminUserCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid request body", zap.Error(err))
		h.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	username := strings.TrimSpace(req.Username)
	if username == "" {
		h.writeError(w, http.StatusBadRequest, "Username is required")
		return
	}

	if strings.TrimSpace(req.Password) == "" && strings.TrimSpace(req.WalletAddress) == "" {
		h.writeError(w, http.StatusBadRequest, "Password or wallet_address is required")
		return
	}

	if _, err := h.userRepository.FindByUsername(r.Context(), username); err == nil {
		h.writeError(w, http.StatusConflict, "Username already exists")
		return
	} else if err != nil && err != user.ErrUserNotFound {
		h.logger.Error("failed to check user", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to check user")
		return
	}

	directory := strings.TrimSpace(req.Directory)
	if directory == "" {
		directory = username
	}

	u := user.NewUser(username, directory)

	if strings.TrimSpace(req.Password) != "" {
		hashed, err := h.passwordHasher.Hash(req.Password)
		if err != nil {
			h.logger.Error("failed to hash password", zap.Error(err))
			h.writeError(w, http.StatusInternalServerError, "Failed to hash password")
			return
		}
		u.SetPassword(hashed)
	}

	if strings.TrimSpace(req.WalletAddress) != "" {
		if err := u.SetWalletAddress(strings.TrimSpace(req.WalletAddress)); err != nil {
			h.writeError(w, http.StatusBadRequest, "Invalid wallet_address")
			return
		}
	}

	if len(req.Permissions) > 0 {
		perms, err := parsePermissionList(req.Permissions)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		u.Permissions = perms
	}

	if req.Quota != nil {
		if err := u.SetQuota(*req.Quota); err != nil {
			h.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	if len(req.Rules) > 0 {
		rules, err := buildAdminRules(req.Rules)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		u.Rules = rules
	}

	if err := h.userRepository.Save(r.Context(), u); err != nil {
		if err == user.ErrDuplicateUsername || err == user.ErrDuplicateAddress {
			h.writeError(w, http.StatusConflict, err.Error())
			return
		}
		h.logger.Error("failed to create user", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to create user")
		return
	}

	h.writeJSON(w, http.StatusCreated, buildAdminUserResponse(u))
}

// HandleUpdate updates a user.
func (h *AdminUserHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req adminUserUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid request body", zap.Error(err))
		h.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	target := strings.TrimSpace(req.Username)
	if target == "" {
		h.writeError(w, http.StatusBadRequest, "Username is required")
		return
	}

	u, err := h.userRepository.FindByUsername(r.Context(), target)
	if err != nil {
		if err == user.ErrUserNotFound {
			h.writeError(w, http.StatusNotFound, "User not found")
			return
		}
		h.logger.Error("failed to find user", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to find user")
		return
	}

	if req.NewUsername != nil {
		newName := strings.TrimSpace(*req.NewUsername)
		if newName == "" {
			h.writeError(w, http.StatusBadRequest, "new_username cannot be empty")
			return
		}
		u.Username = newName
	}

	if req.Directory != nil {
		dir := strings.TrimSpace(*req.Directory)
		if dir == "" {
			h.writeError(w, http.StatusBadRequest, "directory cannot be empty")
			return
		}
		u.Directory = dir
	}

	if req.WalletAddress != nil {
		addr := strings.TrimSpace(*req.WalletAddress)
		if addr == "" {
			h.writeError(w, http.StatusBadRequest, "wallet_address cannot be empty")
			return
		}
		if err := u.SetWalletAddress(addr); err != nil {
			h.writeError(w, http.StatusBadRequest, "Invalid wallet_address")
			return
		}
	}

	if len(req.Permissions) > 0 {
		perms, err := parsePermissionList(req.Permissions)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		u.Permissions = perms
	}

	if req.Quota != nil {
		if err := u.SetQuota(*req.Quota); err != nil {
			h.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	if req.Rules != nil {
		rules, err := buildAdminRules(*req.Rules)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		u.Rules = rules
	}

	if u.Directory == "" {
		u.Directory = u.Username
	}

	u.UpdatedAt = time.Now()

	if err := h.userRepository.Save(r.Context(), u); err != nil {
		if err == user.ErrDuplicateUsername || err == user.ErrDuplicateAddress {
			h.writeError(w, http.StatusConflict, err.Error())
			return
		}
		h.logger.Error("failed to update user", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to update user")
		return
	}

	h.writeJSON(w, http.StatusOK, buildAdminUserResponse(u))
}

// HandleDelete deletes a user.
func (h *AdminUserHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req adminUserDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid request body", zap.Error(err))
		h.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	username := strings.TrimSpace(req.Username)
	if username == "" {
		h.writeError(w, http.StatusBadRequest, "Username is required")
		return
	}

	if err := h.userRepository.Delete(r.Context(), username); err != nil {
		if err == user.ErrUserNotFound {
			h.writeError(w, http.StatusNotFound, "User not found")
			return
		}
		h.logger.Error("failed to delete user", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to delete user")
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// HandleResetPassword resets a user's password.
func (h *AdminUserHandler) HandleResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req adminUserResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid request body", zap.Error(err))
		h.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	username := strings.TrimSpace(req.Username)
	if username == "" || strings.TrimSpace(req.Password) == "" {
		h.writeError(w, http.StatusBadRequest, "Username and password are required")
		return
	}

	u, err := h.userRepository.FindByUsername(r.Context(), username)
	if err != nil {
		if err == user.ErrUserNotFound {
			h.writeError(w, http.StatusNotFound, "User not found")
			return
		}
		h.logger.Error("failed to find user", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to find user")
		return
	}

	hashed, err := h.passwordHasher.Hash(req.Password)
	if err != nil {
		h.logger.Error("failed to hash password", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to hash password")
		return
	}

	u.SetPassword(hashed)
	u.UpdatedAt = time.Now()

	if err := h.userRepository.Save(r.Context(), u); err != nil {
		h.logger.Error("failed to reset password", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to reset password")
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func buildAdminRules(items []adminRuleRequest) ([]*user.Rule, error) {
	rules := make([]*user.Rule, 0, len(items))
	for _, item := range items {
		path := strings.TrimSpace(item.Path)
		if path == "" {
			return nil, errInvalidRule("path is required")
		}
		perms, err := parsePermissionList(item.Permissions)
		if err != nil {
			return nil, err
		}
		rules = append(rules, &user.Rule{
			Path:        path,
			Permissions: perms,
			Regex:       item.Regex,
		})
	}
	return rules, nil
}

func buildAdminUserResponse(u *user.User) adminUserResponse {
	resp := adminUserResponse{
		ID:            u.ID,
		Username:      u.Username,
		WalletAddress: u.WalletAddress,
		Directory:     u.Directory,
		Permissions:   permissionsToStrings(u.Permissions),
		Quota:         u.Quota,
		UsedSpace:     u.UsedSpace,
		HasPassword:   u.HasPassword(),
	}

	if len(u.Rules) > 0 {
		resp.Rules = make([]adminRuleResponse, 0, len(u.Rules))
		for _, rule := range u.Rules {
			resp.Rules = append(resp.Rules, adminRuleResponse{
				Path:        rule.Path,
				Permissions: permissionsToStrings(rule.Permissions),
				Regex:       rule.Regex,
			})
		}
	}

	if !u.CreatedAt.IsZero() {
		resp.CreatedAt = u.CreatedAt.Format(timeLayout)
	}
	if !u.UpdatedAt.IsZero() {
		resp.UpdatedAt = u.UpdatedAt.Format(timeLayout)
	}

	return resp
}

type adminRuleError struct {
	message string
}

func (e adminRuleError) Error() string {
	return e.message
}

func errInvalidRule(msg string) error {
	return adminRuleError{message: msg}
}

func (h *AdminUserHandler) writeJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
	}
}

func (h *AdminUserHandler) writeError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":   message,
		"code":    code,
		"success": false,
	})
}
