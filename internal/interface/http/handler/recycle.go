package handler

import (
	"encoding/json"
	"net/http"

	"github.com/yeying-community/webdav/internal/application/service"
	"github.com/yeying-community/webdav/internal/domain/user"
	"github.com/yeying-community/webdav/internal/interface/http/middleware"
	"go.uber.org/zap"
)

// RecycleHandler 回收站处理器
type RecycleHandler struct {
	recycleService *service.RecycleService
	userRepo       user.Repository
	logger         *zap.Logger
}

// NewRecycleHandler 创建回收站处理器
func NewRecycleHandler(
	recycleService *service.RecycleService,
	userRepo user.Repository,
	logger *zap.Logger,
) *RecycleHandler {
	return &RecycleHandler{
		recycleService: recycleService,
		userRepo:       userRepo,
		logger:         logger,
	}
}

// HandleList 处理获取回收站列表
func (h *RecycleHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		h.logger.Error("user not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	response, err := h.recycleService.List(r.Context(), u)
	if err != nil {
		h.logger.Error("failed to list recycle items",
			zap.String("username", u.Username),
			zap.Error(err))
		http.Error(w, "Failed to list recycle items", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// HandleRecover 处理恢复文件
func (h *RecycleHandler) HandleRecover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		h.logger.Error("user not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Hash string `json:"hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid request body", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Hash == "" {
		http.Error(w, "hash is required", http.StatusBadRequest)
		return
	}

	if err := h.recycleService.Recover(r.Context(), u, req.Hash); err != nil {
		h.logger.Error("failed to recover file",
			zap.String("username", u.Username),
			zap.String("hash", req.Hash),
			zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"message":"recovered successfully"}`)); err != nil {
		h.logger.Error("failed to write response", zap.Error(err))
	}
}

// HandleRemove 处理永久删除
func (h *RecycleHandler) HandleRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		h.logger.Error("user not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Hash string `json:"hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid request body", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Hash == "" {
		http.Error(w, "hash is required", http.StatusBadRequest)
		return
	}

	if err := h.recycleService.Remove(r.Context(), u, req.Hash); err != nil {
		h.logger.Error("failed to remove file",
			zap.String("username", u.Username),
			zap.String("hash", req.Hash),
			zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"message":"removed successfully"}`)); err != nil {
		h.logger.Error("failed to write response", zap.Error(err))
	}
}