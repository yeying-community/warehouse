package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/yeying-community/webdav/internal/application/service"
	"github.com/yeying-community/webdav/internal/domain/share"
	"github.com/yeying-community/webdav/internal/interface/http/middleware"
	"go.uber.org/zap"
)

// ShareHandler 文件分享处理器
type ShareHandler struct {
	shareService *service.ShareService
	logger       *zap.Logger
}

// NewShareHandler 创建分享处理器
func NewShareHandler(shareService *service.ShareService, logger *zap.Logger) *ShareHandler {
	return &ShareHandler{
		shareService: shareService,
		logger:       logger,
	}
}

// HandleCreate 创建分享链接
func (h *ShareHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
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
		Path      string `json:"path"`
		ExpiresIn int64  `json:"expiresIn"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid request body", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	item, err := h.shareService.Create(r.Context(), u, req.Path, req.ExpiresIn)
	if err != nil {
		h.logger.Error("failed to create share",
			zap.String("username", u.Username),
			zap.String("path", req.Path),
			zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := map[string]any{
		"token":         item.Token,
		"name":          item.Name,
		"path":          item.Path,
		"url":           h.buildShareURL(r, item.Token),
		"viewCount":     item.ViewCount,
		"downloadCount": item.DownloadCount,
	}
	if item.ExpiresAt != nil {
		resp["expiresAt"] = item.ExpiresAt.Format(timeLayout)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HandleList 获取分享列表
func (h *ShareHandler) HandleList(w http.ResponseWriter, r *http.Request) {
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

	items, err := h.shareService.List(r.Context(), u)
	if err != nil {
		h.logger.Error("failed to list share items",
			zap.String("username", u.Username),
			zap.Error(err))
		http.Error(w, "Failed to list share items", http.StatusInternalServerError)
		return
	}

	type itemResp struct {
		Token         string `json:"token"`
		Name          string `json:"name"`
		Path          string `json:"path"`
		URL           string `json:"url"`
		ViewCount     int64  `json:"viewCount"`
		DownloadCount int64  `json:"downloadCount"`
		ExpiresAt     string `json:"expiresAt,omitempty"`
		CreatedAt     string `json:"createdAt"`
	}

	resp := struct {
		Items []itemResp `json:"items"`
	}{
		Items: make([]itemResp, 0, len(items)),
	}

	for _, item := range items {
		rsp := itemResp{
			Token:         item.Token,
			Name:          item.Name,
			Path:          item.Path,
			URL:           h.buildShareURL(r, item.Token),
			ViewCount:     item.ViewCount,
			DownloadCount: item.DownloadCount,
			CreatedAt:     item.CreatedAt.Format(timeLayout),
		}
		if item.ExpiresAt != nil {
			rsp.ExpiresAt = item.ExpiresAt.Format(timeLayout)
		}
		resp.Items = append(resp.Items, rsp)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HandleRevoke 取消分享
func (h *ShareHandler) HandleRevoke(w http.ResponseWriter, r *http.Request) {
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
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid request body", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		http.Error(w, "token is required", http.StatusBadRequest)
		return
	}

	if err := h.shareService.Revoke(r.Context(), u, req.Token); err != nil {
		h.logger.Error("failed to revoke share",
			zap.String("username", u.Username),
			zap.String("token", req.Token),
			zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"message":"revoked successfully"}`)); err != nil {
		h.logger.Error("failed to write response", zap.Error(err))
	}
}

// HandleAccess 访问分享链接（公开）
func (h *ShareHandler) HandleAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := strings.TrimPrefix(r.URL.Path, "/api/v1/public/share/")
	token = strings.Split(token, "/")[0]
	if token == "" {
		http.NotFound(w, r)
		return
	}

	item, file, info, err := h.shareService.Resolve(r.Context(), token)
	if err != nil {
		if err == share.ErrShareNotFound || err == share.ErrInvalidShare || errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		if err == share.ErrShareExpired {
			http.Error(w, "share expired", http.StatusGone)
			return
		}
		h.logger.Error("failed to resolve share", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	if shouldCountAccess(r) {
		_ = h.shareService.IncrementView(r.Context(), token)
		if r.Method == http.MethodGet {
			_ = h.shareService.IncrementDownload(r.Context(), token)
		}
	}

	filename := url.PathEscape(item.Name)
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+filename)

	http.ServeContent(w, r, item.Name, info.ModTime(), file)
}

func (h *ShareHandler) buildShareURL(r *http.Request, token string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = forwarded
	}
	host := r.Host
	if forwardedHost := r.Header.Get("X-Forwarded-Host"); forwardedHost != "" {
		host = forwardedHost
	}
	return scheme + "://" + host + "/api/v1/public/share/" + token
}

const timeLayout = "2006-01-02T15:04:05Z07:00"

func shouldCountAccess(r *http.Request) bool {
	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		return true
	}
	// 只统计首段请求，避免 Range 分片导致计数膨胀
	return strings.HasPrefix(rangeHeader, "bytes=0-")
}
