package handler

import (
	"encoding/json"
	"net/http"

	"github.com/yeying-community/warehouse/internal/domain/quota"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

// QuotaHandler 配额处理器
type QuotaHandler struct {
	quotaService quota.Service
	logger       *zap.Logger
}

// NewQuotaHandler 创建配额处理器
func NewQuotaHandler(quotaService quota.Service, logger *zap.Logger) *QuotaHandler {
	return &QuotaHandler{
		quotaService: quotaService,
		logger:       logger,
	}
}

// QuotaResponse 配额响应
type QuotaResponse struct {
	Quota      int64   `json:"quota"`      // 配额大小（字节）
	Used       int64   `json:"used"`       // 已使用（字节）
	Available  int64   `json:"available"`  // 可用空间（字节）
	Percentage float64 `json:"percentage"` // 使用百分比
	Unlimited  bool    `json:"unlimited"`  // 是否无限制
}

// GetUserQuota 获取用户配额信息
func (h *QuotaHandler) GetUserQuota(w http.ResponseWriter, r *http.Request) {
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

	// 获取配额信息
	quotaInfo, err := h.quotaService.GetQuota(r.Context(), u.ID)
	if err != nil {
		h.logger.Error("failed to get quota",
			zap.String("username", u.Username),
			zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to get quota information")
		return
	}

	// 构建响应
	response := QuotaResponse{
		Quota:     quotaInfo.Quota,
		Used:      quotaInfo.Used,
		Available: quotaInfo.Available,
		Unlimited: quotaInfo.Quota == 0,
	}

	// 计算使用百分比
	if quotaInfo.Quota > 0 {
		response.Percentage = float64(quotaInfo.Used) / float64(quotaInfo.Quota) * 100
	}

	// 返回 JSON 响应
	h.writeJSON(w, http.StatusOK, response)
}

// writeJSON 写入 JSON 响应
func (h *QuotaHandler) writeJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
	}
}

// writeError 写入错误响应
func (h *QuotaHandler) writeError(w http.ResponseWriter, code int, message string) {
	h.writeJSON(w, code, map[string]interface{}{
		"error":   message,
		"code":    code,
		"success": false,
	})
}
