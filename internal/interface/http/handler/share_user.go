package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/auth"
	"github.com/yeying-community/warehouse/internal/domain/shareuser"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

// ShareUserHandler 定向分享处理器
type ShareUserHandler struct {
	shareUserService *service.ShareUserService
	userRepo         user.Repository
	logger           *zap.Logger
}

// NewShareUserHandler 创建定向分享处理器
func NewShareUserHandler(shareUserService *service.ShareUserService, userRepo user.Repository, logger *zap.Logger) *ShareUserHandler {
	return &ShareUserHandler{
		shareUserService: shareUserService,
		userRepo:         userRepo,
		logger:           logger,
	}
}

// HandleCreate 创建定向分享
func (h *ShareUserHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
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
		Path          string   `json:"path"`
		TargetAddress string   `json:"targetAddress"`
		Permissions   []string `json:"permissions"`
		ExpiresIn     int64    `json:"expiresIn"`
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
	if strings.TrimSpace(req.TargetAddress) == "" {
		http.Error(w, "targetAddress is required", http.StatusBadRequest)
		return
	}

	perms, err := parsePermissionList(req.Permissions)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	item, err := h.shareUserService.Create(r.Context(), u, req.TargetAddress, req.Path, perms.String(), req.ExpiresIn)
	if err != nil {
		if errors.Is(err, auth.ErrAppScopeDenied) || errors.Is(err, auth.ErrAppScopeRequired) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		h.logger.Error("failed to create share user",
			zap.String("owner", u.Username),
			zap.String("path", req.Path),
			zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := map[string]any{
		"id":           item.ID,
		"name":         item.Name,
		"path":         item.Path,
		"isDir":        item.IsDir,
		"permissions":  permissionsToStrings(perms),
		"targetWallet": item.TargetWalletAddress,
		"createdAt":    item.CreatedAt.Format(timeLayout),
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

// HandleListMine 获取我分享的列表
func (h *ShareUserHandler) HandleListMine(w http.ResponseWriter, r *http.Request) {
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

	items, err := h.shareUserService.ListByOwner(r.Context(), u)
	if err != nil {
		if errors.Is(err, auth.ErrAppScopeDenied) || errors.Is(err, auth.ErrAppScopeRequired) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		h.logger.Error("failed to list share user items",
			zap.String("owner", u.Username),
			zap.Error(err))
		http.Error(w, "Failed to list share items", http.StatusInternalServerError)
		return
	}

	type itemResp struct {
		ID           string   `json:"id"`
		Name         string   `json:"name"`
		Path         string   `json:"path"`
		IsDir        bool     `json:"isDir"`
		Permissions  []string `json:"permissions"`
		TargetWallet string   `json:"targetWallet"`
		ExpiresAt    string   `json:"expiresAt,omitempty"`
		CreatedAt    string   `json:"createdAt"`
	}

	resp := struct {
		Items []itemResp `json:"items"`
	}{
		Items: make([]itemResp, 0, len(items)),
	}

	for _, item := range items {
		perms := permissionsFromStored(item.Permissions)
		row := itemResp{
			ID:           item.ID,
			Name:         item.Name,
			Path:         item.Path,
			IsDir:        item.IsDir,
			Permissions:  permissionsToStrings(perms),
			TargetWallet: item.TargetWalletAddress,
			CreatedAt:    item.CreatedAt.Format(timeLayout),
		}
		if item.ExpiresAt != nil {
			row.ExpiresAt = item.ExpiresAt.Format(timeLayout)
		}
		resp.Items = append(resp.Items, row)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HandleListReceived 获取分享给我的列表
func (h *ShareUserHandler) HandleListReceived(w http.ResponseWriter, r *http.Request) {
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

	items, err := h.shareUserService.ListByTarget(r.Context(), u)
	if err != nil {
		if errors.Is(err, auth.ErrAppScopeDenied) || errors.Is(err, auth.ErrAppScopeRequired) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		h.logger.Error("failed to list received share items",
			zap.String("target", u.Username),
			zap.Error(err))
		http.Error(w, "Failed to list share items", http.StatusInternalServerError)
		return
	}

	type itemResp struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Path        string   `json:"path"`
		IsDir       bool     `json:"isDir"`
		Permissions []string `json:"permissions"`
		OwnerWallet string   `json:"ownerWallet,omitempty"`
		OwnerName   string   `json:"ownerName,omitempty"`
		ExpiresAt   string   `json:"expiresAt,omitempty"`
		CreatedAt   string   `json:"createdAt"`
	}

	resp := struct {
		Items []itemResp `json:"items"`
	}{
		Items: make([]itemResp, 0, len(items)),
	}

	for _, item := range items {
		perms := permissionsFromStored(item.Permissions)
		row := itemResp{
			ID:          item.ID,
			Name:        item.Name,
			Path:        item.Path,
			IsDir:       item.IsDir,
			Permissions: permissionsToStrings(perms),
			OwnerName:   item.OwnerUsername,
			CreatedAt:   item.CreatedAt.Format(timeLayout),
		}
		if item.ExpiresAt != nil {
			row.ExpiresAt = item.ExpiresAt.Format(timeLayout)
		}
		if owner, err := h.userRepo.FindByID(r.Context(), item.OwnerUserID); err == nil {
			row.OwnerWallet = owner.WalletAddress
		}
		resp.Items = append(resp.Items, row)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HandleRevoke 取消分享
func (h *ShareUserHandler) HandleRevoke(w http.ResponseWriter, r *http.Request) {
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
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid request body", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	if err := h.shareUserService.Revoke(r.Context(), u, req.ID); err != nil {
		if errors.Is(err, auth.ErrAppScopeDenied) || errors.Is(err, auth.ErrAppScopeRequired) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		h.logger.Error("failed to revoke share user",
			zap.String("owner", u.Username),
			zap.String("share_id", req.ID),
			zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"message":"revoked successfully"}`)); err != nil {
		h.logger.Error("failed to write response", zap.Error(err))
	}
}

// HandleEntries 获取分享目录内容
func (h *ShareUserHandler) HandleEntries(w http.ResponseWriter, r *http.Request) {
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

	shareID := r.URL.Query().Get("shareId")
	relPath := r.URL.Query().Get("path")
	if strings.TrimSpace(shareID) == "" {
		http.Error(w, "shareId is required", http.StatusBadRequest)
		return
	}

	item, owner, err := h.shareUserService.ResolveForTarget(r.Context(), u, shareID, "read")
	if err != nil {
		writeShareUserError(w, err)
		return
	}

	perms := permissionsFromStored(item.Permissions)
	if !perms.Has("read") {
		http.Error(w, "permission denied", http.StatusForbidden)
		return
	}

	_, fullPath, err := h.shareUserService.ResolveSharePath(owner, item, relPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to stat path", http.StatusInternalServerError)
		return
	}

	type entryResp struct {
		Name     string `json:"name"`
		Path     string `json:"path"`
		IsDir    bool   `json:"isDir"`
		Size     int64  `json:"size"`
		Modified string `json:"modified"`
	}

	resp := struct {
		Items []entryResp `json:"items"`
	}{
		Items: make([]entryResp, 0),
	}

	if info.IsDir() {
		entries, err := os.ReadDir(fullPath)
		if err != nil {
			http.Error(w, "Failed to read directory", http.StatusInternalServerError)
			return
		}

		prefix := normalizeRelPath(relPath)
		for _, entry := range entries {
			entryInfo, err := entry.Info()
			if err != nil {
				continue
			}
			entryPath := buildShareEntryPath(prefix, entry.Name(), entryInfo.IsDir())
			resp.Items = append(resp.Items, entryResp{
				Name:     entryInfo.Name(),
				Path:     entryPath,
				IsDir:    entryInfo.IsDir(),
				Size:     entryInfo.Size(),
				Modified: entryInfo.ModTime().Format(timeLayout),
			})
		}
	} else {
		resp.Items = append(resp.Items, entryResp{
			Name:     info.Name(),
			Path:     "/" + info.Name(),
			IsDir:    false,
			Size:     info.Size(),
			Modified: info.ModTime().Format(timeLayout),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HandleDownload 下载分享文件
func (h *ShareUserHandler) HandleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		h.logger.Error("user not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	shareID := r.URL.Query().Get("shareId")
	relPath := r.URL.Query().Get("path")
	if strings.TrimSpace(shareID) == "" {
		http.Error(w, "shareId is required", http.StatusBadRequest)
		return
	}

	item, owner, err := h.shareUserService.ResolveForTarget(r.Context(), u, shareID, "read")
	if err != nil {
		writeShareUserError(w, err)
		return
	}

	perms := permissionsFromStored(item.Permissions)
	if !perms.Has("read") {
		http.Error(w, "permission denied", http.StatusForbidden)
		return
	}

	_, fullPath, err := h.shareUserService.ResolveSharePath(owner, item, relPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to open file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		http.Error(w, "Failed to stat file", http.StatusInternalServerError)
		return
	}
	if info.IsDir() {
		http.Error(w, "Path is a directory", http.StatusBadRequest)
		return
	}

	filename := url.PathEscape(info.Name())
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+filename)

	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

// HandleUpload 上传分享目录内文件
func (h *ShareUserHandler) HandleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		h.logger.Error("user not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	shareID := r.URL.Query().Get("shareId")
	relPath := r.URL.Query().Get("path")
	if strings.TrimSpace(shareID) == "" {
		http.Error(w, "shareId is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(relPath) == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	item, owner, err := h.shareUserService.ResolveForTarget(r.Context(), u, shareID, "create", "update")
	if err != nil {
		writeShareUserError(w, err)
		return
	}

	perms := permissionsFromStored(item.Permissions)
	if !perms.Has("create") {
		http.Error(w, "permission denied", http.StatusForbidden)
		return
	}

	_, fullPath, err := h.shareUserService.ResolveSharePath(owner, item, relPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, "Invalid upload body", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		http.Error(w, "Failed to create directory", http.StatusInternalServerError)
		return
	}

	dst, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		http.Error(w, "Failed to write file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		http.Error(w, "Failed to write file", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"message":"uploaded successfully"}`)); err != nil {
		h.logger.Error("failed to write response", zap.Error(err))
	}
}

// HandleCreateFolder 创建目录
func (h *ShareUserHandler) HandleCreateFolder(w http.ResponseWriter, r *http.Request) {
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
		ShareID string `json:"shareId"`
		Path    string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid request body", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ShareID) == "" {
		http.Error(w, "shareId is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	item, owner, err := h.shareUserService.ResolveForTarget(r.Context(), u, req.ShareID, "create")
	if err != nil {
		writeShareUserError(w, err)
		return
	}

	perms := permissionsFromStored(item.Permissions)
	if !perms.Has("create") {
		http.Error(w, "permission denied", http.StatusForbidden)
		return
	}

	_, fullPath, err := h.shareUserService.ResolveSharePath(owner, item, req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := os.MkdirAll(fullPath, 0755); err != nil {
		http.Error(w, "Failed to create folder", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"message":"created successfully"}`)); err != nil {
		h.logger.Error("failed to write response", zap.Error(err))
	}
}

// HandleRename 重命名
func (h *ShareUserHandler) HandleRename(w http.ResponseWriter, r *http.Request) {
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
		ShareID string `json:"shareId"`
		From    string `json:"from"`
		To      string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid request body", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ShareID) == "" {
		http.Error(w, "shareId is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.From) == "" || strings.TrimSpace(req.To) == "" {
		http.Error(w, "from and to are required", http.StatusBadRequest)
		return
	}

	item, owner, err := h.shareUserService.ResolveForTarget(r.Context(), u, req.ShareID, "move")
	if err != nil {
		writeShareUserError(w, err)
		return
	}

	perms := permissionsFromStored(item.Permissions)
	if !perms.Has("update") {
		http.Error(w, "permission denied", http.StatusForbidden)
		return
	}

	_, fromPath, err := h.shareUserService.ResolveSharePath(owner, item, req.From)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, toPath, err := h.shareUserService.ResolveSharePath(owner, item, req.To)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := os.Rename(fromPath, toPath); err != nil {
		http.Error(w, "Failed to rename", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"message":"renamed successfully"}`)); err != nil {
		h.logger.Error("failed to write response", zap.Error(err))
	}
}

// HandleDelete 删除分享内容
func (h *ShareUserHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
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
		ShareID string `json:"shareId"`
		Path    string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid request body", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ShareID) == "" {
		http.Error(w, "shareId is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	item, owner, err := h.shareUserService.ResolveForTarget(r.Context(), u, req.ShareID, "delete")
	if err != nil {
		writeShareUserError(w, err)
		return
	}

	perms := permissionsFromStored(item.Permissions)
	if !perms.Has("delete") {
		http.Error(w, "permission denied", http.StatusForbidden)
		return
	}

	_, fullPath, err := h.shareUserService.ResolveSharePath(owner, item, req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := os.RemoveAll(fullPath); err != nil {
		http.Error(w, "Failed to delete", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"message":"deleted successfully"}`)); err != nil {
		h.logger.Error("failed to write response", zap.Error(err))
	}
}

func writeShareUserError(w http.ResponseWriter, err error) {
	if errors.Is(err, auth.ErrAppScopeDenied) || errors.Is(err, auth.ErrAppScopeRequired) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if err == shareuser.ErrShareNotFound || errors.Is(err, shareuser.ErrShareNotFound) {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	if err == shareuser.ErrShareExpired || errors.Is(err, shareuser.ErrShareExpired) {
		http.Error(w, "share expired", http.StatusGone)
		return
	}
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func parsePermissionList(list []string) (*user.Permissions, error) {
	perms := &user.Permissions{}

	if len(list) == 1 {
		raw := strings.TrimSpace(list[0])
		if raw != "" && looksLikePermissionString(raw) {
			return user.ParsePermissions(raw), nil
		}
	}

	for _, item := range list {
		switch strings.ToLower(strings.TrimSpace(item)) {
		case "r", "read":
			perms.Read = true
		case "c", "create", "upload":
			perms.Create = true
		case "u", "update", "rename":
			perms.Update = true
		case "d", "delete", "remove":
			perms.Delete = true
		case "":
			continue
		default:
			return nil, fmt.Errorf("invalid permission: %s", item)
		}
	}

	if !perms.Read && !perms.Create && !perms.Update && !perms.Delete {
		perms.Read = true
	}

	return perms, nil
}

func permissionsFromStored(s string) *user.Permissions {
	if strings.TrimSpace(s) == "" {
		return user.DefaultPermissions()
	}
	return user.ParsePermissions(s)
}

func looksLikePermissionString(s string) bool {
	s = strings.ToUpper(strings.TrimSpace(s))
	for _, ch := range s {
		if ch != 'C' && ch != 'R' && ch != 'U' && ch != 'D' {
			return false
		}
	}
	return s != ""
}

func normalizeRelPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	clean := path.Clean("/" + strings.TrimLeft(raw, "/"))
	clean = strings.TrimPrefix(clean, "/")
	if clean == "." {
		return ""
	}
	return clean
}

func buildShareEntryPath(prefix, name string, isDir bool) string {
	var p string
	if prefix == "" {
		p = path.Join("/", name)
	} else {
		p = path.Join("/", prefix, name)
	}
	if isDir && !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}
