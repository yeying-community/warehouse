package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/yeying-community/warehouse/internal/application/assetspace"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

// AssetsHandler 提供资产空间元信息接口
type AssetsHandler struct {
	assetSpaceManager *assetspace.Manager
	logger            *zap.Logger
}

// NewAssetsHandler 创建资产空间处理器
func NewAssetsHandler(assetSpaceManager *assetspace.Manager, logger *zap.Logger) *AssetsHandler {
	return &AssetsHandler{
		assetSpaceManager: assetSpaceManager,
		logger:            logger,
	}
}

// GetSpaces 获取资产空间信息
// GET /api/v1/public/assets/spaces
func (h *AssetsHandler) GetSpaces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET method is allowed")
		return
	}

	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		h.logger.Error("user not found in context")
		h.sendError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized")
		return
	}

	if h.assetSpaceManager != nil {
		if err := h.assetSpaceManager.EnsureForUser(u); err != nil {
			h.logger.Error("failed to ensure user asset spaces",
				zap.String("username", u.Username),
				zap.String("directory", u.Directory),
				zap.Error(err))
			h.sendError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to initialize user spaces")
			return
		}
	}

	defaultSpace := assetspace.PersonalSpaceKey
	spaces := []assetspace.Space{
		{Key: assetspace.PersonalSpaceKey, Name: "个人资产", Path: "/personal"},
		{Key: assetspace.AppsSpaceKey, Name: "应用资产", Path: "/apps"},
	}
	if h.assetSpaceManager != nil {
		defaultSpace = h.assetSpaceManager.DefaultSpace()
		spaces = h.assetSpaceManager.Spaces()
	}

	h.sendSDKSuccess(w, map[string]interface{}{
		"defaultSpace": defaultSpace,
		"spaces":       spaces,
	})
}

func (h *AssetsHandler) sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
	}
}

func (h *AssetsHandler) sendSDKSuccess(w http.ResponseWriter, data interface{}) {
	h.sendSDKResponse(w, http.StatusOK, 0, "ok", data)
}

func (h *AssetsHandler) sendSDKResponse(w http.ResponseWriter, status int, code int, message string, data interface{}) {
	response := sdkResponse{
		Code:      code,
		Message:   message,
		Data:      data,
		Timestamp: time.Now().UnixMilli(),
	}
	h.sendJSON(w, status, response)
}

func (h *AssetsHandler) sendError(w http.ResponseWriter, status int, code, message string) {
	h.sendSDKResponse(w, status, status, message, nil)
}
