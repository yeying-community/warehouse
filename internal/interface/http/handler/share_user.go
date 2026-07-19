package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/yeying-community/warehouse/internal/infrastructure/atomicfile"
	webdavfs "github.com/yeying-community/warehouse/internal/infrastructure/webdav"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
	"golang.org/x/net/webdav"
)

// ShareUserHandler 定向分享处理器
type ShareUserHandler struct {
	shareUserService *service.ShareUserService
	userRepo         user.Repository
	mutationRecorder service.MutationRecorder
	logger           *zap.Logger
}

type bufferedResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

type shareTargetGroupResp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type shareUserItemResp struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Path          string                 `json:"path"`
	IsDir         bool                   `json:"isDir"`
	Permissions   []string               `json:"permissions"`
	TargetWallet  string                 `json:"targetWallet,omitempty"`
	TargetType    string                 `json:"targetType,omitempty"`
	TargetCount   int                    `json:"targetCount,omitempty"`
	AudienceCount int                    `json:"audienceCount,omitempty"`
	TargetGroups  []shareTargetGroupResp `json:"targetGroups,omitempty"`
	AllUsers      bool                   `json:"allUsers,omitempty"`
	OwnerWallet   string                 `json:"ownerWallet,omitempty"`
	OwnerName     string                 `json:"ownerName,omitempty"`
	ExpiresAt     string                 `json:"expiresAt,omitempty"`
	CreatedAt     string                 `json:"createdAt"`
}

func newBufferedResponse() *bufferedResponse {
	return &bufferedResponse{header: make(http.Header), status: http.StatusOK}
}

func (r *bufferedResponse) Header() http.Header {
	return r.header
}

func (r *bufferedResponse) WriteHeader(status int) {
	r.status = status
}

func (r *bufferedResponse) Write(p []byte) (int, error) {
	return r.body.Write(p)
}

func (r *bufferedResponse) FlushTo(w http.ResponseWriter) {
	for key, values := range r.header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(r.status)
	_, _ = r.body.WriteTo(w)
}

// NewShareUserHandler 创建定向分享处理器
func NewShareUserHandler(
	shareUserService *service.ShareUserService,
	userRepo user.Repository,
	mutationRecorder service.MutationRecorder,
	logger *zap.Logger,
) *ShareUserHandler {
	if mutationRecorder == nil {
		mutationRecorder = service.NewMutationRecorder(nil, nil, nil, nil)
	}
	return &ShareUserHandler{
		shareUserService: shareUserService,
		userRepo:         userRepo,
		mutationRecorder: mutationRecorder,
		logger:           logger,
	}
}

// HandleDAV exposes shared content as a WebDAV virtual root:
// /dav/share/{shareId}/...
func (h *ShareUserHandler) HandleDAV(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		writeDAVOptions(w)
		return
	}

	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		h.logger.Error("user not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	shareID, relPath, davPrefix, err := h.parseDAVSharePath(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	item, owner, err := h.shareUserService.ResolveForTarget(r.Context(), u, shareID, requiredActionsForShareDAV(r.Method)...)
	if err != nil {
		writeShareUserError(w, err)
		return
	}
	perms := permissionsFromStored(item.Permissions)

	_, baseFull, err := h.shareUserService.ResolveSharePath(owner, item, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, targetFull, err := h.shareUserService.ResolveSharePath(owner, item, relPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.checkDAVSharePermission(r, perms, shareID, targetFull); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	if isMutatingShareDAVMethod(r.Method) {
		targetWasDir := false
		if info, err := os.Stat(targetFull); err == nil {
			targetWasDir = info.IsDir()
		}
		h.serveMutatingShareDAV(w, r, shareDAVContext{
			shareID:      shareID,
			davPrefix:    davPrefix,
			baseFull:     baseFull,
			targetFull:   targetFull,
			targetWasDir: targetWasDir,
			owner:        owner,
			item:         item,
		})
		return
	}

	h.serveShareDAV(w, r, davPrefix, baseFull)
}

type shareDAVContext struct {
	shareID      string
	davPrefix    string
	baseFull     string
	targetFull   string
	targetWasDir bool
	owner        *user.User
	item         *shareuser.ShareUserItem
}

func writeDAVOptions(w http.ResponseWriter) {
	methods := []string{
		"OPTIONS",
		"GET", "HEAD", "PUT", "DELETE",
		"PROPFIND", "PROPPATCH",
		"MKCOL", "COPY", "MOVE",
		"LOCK", "UNLOCK",
	}
	w.Header().Set("Allow", strings.Join(methods, ", "))
	w.Header().Set("DAV", "1, 2")
	w.Header().Set("MS-Author-Via", "DAV")
	w.Header().Set("Accept-Ranges", "bytes")
	w.WriteHeader(http.StatusOK)
}

func (h *ShareUserHandler) parseDAVSharePath(rawPath string) (shareID, relPath, davPrefix string, err error) {
	cfg := h.shareUserService.Config()
	webdavPrefix := "/dav/"
	if cfg != nil && strings.TrimSpace(cfg.WebDAV.Prefix) != "" {
		webdavPrefix = strings.Trim(strings.TrimSpace(cfg.WebDAV.Prefix), "/")
		webdavPrefix = "/" + webdavPrefix + "/"
	}
	sharePrefix := webdavPrefix + "share/"
	if !strings.HasPrefix(rawPath, sharePrefix) {
		return "", "", "", fmt.Errorf("invalid share dav path")
	}
	rest := strings.TrimPrefix(rawPath, sharePrefix)
	parts := strings.SplitN(rest, "/", 2)
	shareID = strings.TrimSpace(parts[0])
	if shareID == "" {
		return "", "", "", fmt.Errorf("shareId is required")
	}
	if len(parts) > 1 {
		relPath = normalizeRelPath(parts[1])
	}
	davPrefix = strings.TrimSuffix(sharePrefix+shareID, "/")
	return shareID, relPath, davPrefix, nil
}

func requiredActionsForShareDAV(method string) []string {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, "PROPFIND", "REPORT":
		return []string{"read"}
	case http.MethodPut, "MKCOL", http.MethodPost:
		return []string{"create", "update"}
	case "MOVE":
		return []string{"move"}
	case http.MethodDelete:
		return []string{"delete"}
	case "COPY":
		return []string{"copy", "read", "create"}
	default:
		return []string{"read"}
	}
}

func (h *ShareUserHandler) checkDAVSharePermission(r *http.Request, perms *user.Permissions, shareID, targetFull string) error {
	if perms == nil {
		return fmt.Errorf("permission denied")
	}
	switch strings.ToUpper(strings.TrimSpace(r.Method)) {
	case http.MethodGet, http.MethodHead, "PROPFIND", "REPORT":
		if !perms.Has("read") {
			return fmt.Errorf("permission denied")
		}
	case "MKCOL":
		if !perms.Has("create") {
			return fmt.Errorf("permission denied")
		}
	case http.MethodPut:
		if _, err := os.Stat(targetFull); err == nil {
			if !perms.Has("update") {
				return fmt.Errorf("permission denied")
			}
		} else if os.IsNotExist(err) {
			if !perms.Has("create") {
				return fmt.Errorf("permission denied")
			}
		} else {
			return fmt.Errorf("permission denied")
		}
	case "MOVE":
		if !perms.Has("update") {
			return fmt.Errorf("permission denied")
		}
		if err := h.ensureSameDAVShareDestination(r, shareID); err != nil {
			return err
		}
	case "COPY":
		if !perms.Has("read") || !perms.Has("create") {
			return fmt.Errorf("permission denied")
		}
		if err := h.ensureSameDAVShareDestination(r, shareID); err != nil {
			return err
		}
	case http.MethodDelete:
		if !perms.Has("delete") {
			return fmt.Errorf("permission denied")
		}
	default:
		return fmt.Errorf("method not allowed")
	}
	return nil
}

func (h *ShareUserHandler) ensureSameDAVShareDestination(r *http.Request, shareID string) error {
	destination := strings.TrimSpace(r.Header.Get("Destination"))
	if destination == "" {
		return fmt.Errorf("missing Destination header")
	}
	destPath := destination
	if strings.HasPrefix(destPath, "http://") || strings.HasPrefix(destPath, "https://") {
		parsed, err := url.Parse(destPath)
		if err != nil {
			return fmt.Errorf("invalid Destination header")
		}
		destPath = parsed.Path
	}
	destShareID, _, _, err := h.parseDAVSharePath(destPath)
	if err != nil {
		return err
	}
	if destShareID != shareID {
		return fmt.Errorf("cross-share move is not allowed")
	}
	return nil
}

func isMutatingShareDAVMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPut, http.MethodPost, "MKCOL", http.MethodDelete, "MOVE", "COPY":
		return true
	default:
		return false
	}
}

func (h *ShareUserHandler) serveShareDAV(w http.ResponseWriter, r *http.Request, davPrefix, baseFull string) {
	handler := &webdav.Handler{
		Prefix:     davPrefix,
		FileSystem: webdavfs.NewUnicodeFileSystem(baseFull),
		LockSystem: webdav.NewMemLS(),
		Logger:     h.createShareDAVLogger(),
	}
	w.Header().Set("DAV", "1, 2")
	w.Header().Set("MS-Author-Via", "DAV")
	handler.ServeHTTP(w, r)
}

func (h *ShareUserHandler) serveMutatingShareDAV(w http.ResponseWriter, r *http.Request, ctx shareDAVContext) {
	rec := newBufferedResponse()
	h.serveShareDAV(rec, r, ctx.davPrefix, ctx.baseFull)
	if rec.status >= 200 && rec.status < 300 {
		if err := h.recordShareDAVMutation(r, ctx); err != nil {
			h.logger.Error("failed to record share dav mutation", zap.Error(err))
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}
	rec.FlushTo(w)
}

func (h *ShareUserHandler) recordShareDAVMutation(r *http.Request, ctx shareDAVContext) error {
	switch strings.ToUpper(strings.TrimSpace(r.Method)) {
	case http.MethodPut, http.MethodPost:
		if err := h.mutationRecorder.EnsureDir(r.Context(), filepath.Dir(ctx.targetFull)); err != nil {
			return err
		}
		return h.mutationRecorder.UpsertFile(r.Context(), ctx.targetFull)
	case "MKCOL":
		return h.mutationRecorder.EnsureDir(r.Context(), ctx.targetFull)
	case http.MethodDelete:
		return h.mutationRecorder.RemovePath(r.Context(), ctx.targetFull, ctx.targetWasDir)
	case "MOVE":
		toFull, err := h.resolveDAVShareDestinationFullPath(r, ctx)
		if err != nil {
			return err
		}
		info, err := os.Stat(toFull)
		isDir := false
		if err == nil {
			isDir = info.IsDir()
		}
		if err := service.SyncUserSharePathsForOwnerMove(r.Context(), h.shareUserService.Repository(), h.shareUserService.Config(), ctx.owner, ctx.targetFull, toFull); err != nil {
			return err
		}
		if err := h.mutationRecorder.EnsureDir(r.Context(), filepath.Dir(toFull)); err != nil {
			return err
		}
		return h.mutationRecorder.MovePath(r.Context(), ctx.targetFull, toFull, isDir)
	case "COPY":
		toFull, err := h.resolveDAVShareDestinationFullPath(r, ctx)
		if err != nil {
			return err
		}
		info, err := os.Stat(toFull)
		isDir := false
		if err == nil {
			isDir = info.IsDir()
		}
		if err := h.mutationRecorder.EnsureDir(r.Context(), filepath.Dir(toFull)); err != nil {
			return err
		}
		return h.mutationRecorder.CopyPath(r.Context(), ctx.targetFull, toFull, isDir)
	}
	return nil
}

func (h *ShareUserHandler) resolveDAVShareDestinationFullPath(r *http.Request, ctx shareDAVContext) (string, error) {
	destination := strings.TrimSpace(r.Header.Get("Destination"))
	if destination == "" {
		return "", fmt.Errorf("missing Destination header")
	}
	destPath := destination
	if strings.HasPrefix(destPath, "http://") || strings.HasPrefix(destPath, "https://") {
		parsed, err := url.Parse(destPath)
		if err != nil {
			return "", fmt.Errorf("invalid Destination header")
		}
		destPath = parsed.Path
	}
	_, relPath, _, err := h.parseDAVSharePath(destPath)
	if err != nil {
		return "", err
	}
	_, fullPath, err := h.shareUserService.ResolveSharePath(ctx.owner, ctx.item, relPath)
	if err != nil {
		return "", err
	}
	return fullPath, nil
}

func (h *ShareUserHandler) createShareDAVLogger() func(*http.Request, error) {
	return func(r *http.Request, err error) {
		if err == nil {
			return
		}
		h.logger.Warn("share dav error",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Error(err),
		)
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
		Path            string   `json:"path"`
		TargetAddresses []string `json:"targetAddresses"`
		TargetMode      string   `json:"targetMode"`
		GroupIDs        []string `json:"groupIds"`
		Permissions     []string `json:"permissions"`
		ExpiresIn       int64    `json:"expiresIn"`
		ExpiresValue    int64    `json:"expiresValue"`
		ExpiresUnit     string   `json:"expiresUnit"`
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

	perms, err := parsePermissionList(req.Permissions)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	expiry := service.ShareExpiryInput{
		ExpiresIn:    req.ExpiresIn,
		ExpiresValue: req.ExpiresValue,
		ExpiresUnit:  req.ExpiresUnit,
	}

	mode := strings.TrimSpace(strings.ToLower(req.TargetMode))
	if mode == "" {
		http.Error(w, "targetMode is required", http.StatusBadRequest)
		return
	}

	var item *shareuser.ShareUserItem
	switch mode {
	case "all_users":
		item, err = h.shareUserService.CreateForAllUsers(r.Context(), u, req.Path, perms.String(), expiry)
	case "groups":
		if len(req.GroupIDs) == 0 {
			http.Error(w, "groupIds is required", http.StatusBadRequest)
			return
		}
		item, err = h.shareUserService.CreateByGroups(r.Context(), u, req.GroupIDs, req.Path, perms.String(), expiry)
	case "addresses":
		if countNonEmpty(req.TargetAddresses) == 0 {
			http.Error(w, "targetAddresses is required", http.StatusBadRequest)
			return
		}
		item, err = h.shareUserService.CreateByWallets(r.Context(), u, req.TargetAddresses, req.Path, perms.String(), expiry)
	default:
		http.Error(w, "invalid targetMode, supported: addresses|groups|all_users", http.StatusBadRequest)
		return
	}
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
		"id":            item.ID,
		"name":          item.Name,
		"path":          item.Path,
		"isDir":         item.IsDir,
		"permissions":   permissionsToStrings(perms),
		"targetWallet":  formatTargetWallet(item),
		"targetType":    item.AudienceType,
		"targetCount":   item.TargetCount,
		"audienceCount": item.AudienceCount,
		"allUsers":      item.AllUsers || item.AudienceType == shareuser.AudienceTypeAllUsers,
		"createdAt":     item.CreatedAt.Format(timeLayout),
	}
	if item.ExpiresAt != nil {
		resp["expiresAt"] = item.ExpiresAt.Format(timeLayout)
	}
	if item.AudienceType == "groups" {
		resp["targetGroups"] = h.targetGroupsForShare(r.Context(), item.ID)
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

	resp := struct {
		Items []shareUserItemResp `json:"items"`
	}{
		Items: make([]shareUserItemResp, 0, len(items)),
	}

	for _, item := range items {
		resp.Items = append(resp.Items, h.buildShareUserItemResp(r.Context(), item, formatTargetWallet(item), u.WalletAddress, u.Username))
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

	resp := struct {
		Items []shareUserItemResp `json:"items"`
	}{
		Items: make([]shareUserItemResp, 0, len(items)),
	}

	for _, item := range items {
		ownerWallet := ""
		if owner, err := h.userRepo.FindByID(r.Context(), item.OwnerUserID); err == nil {
			ownerWallet = owner.WalletAddress
		}
		resp.Items = append(resp.Items, h.buildShareUserItemResp(r.Context(), item, formatTargetWalletForViewer(item, u.WalletAddress), ownerWallet, item.OwnerUsername))
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

// HandleListAudiences 查看共享受众（仅 owner 可查看）
func (h *ShareUserHandler) HandleListAudiences(w http.ResponseWriter, r *http.Request) {
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

	shareID := strings.TrimSpace(r.URL.Query().Get("shareId"))
	if shareID == "" {
		http.Error(w, "shareId is required", http.StatusBadRequest)
		return
	}

	audiences, err := h.shareUserService.ListAudiences(r.Context(), u, shareID)
	if err != nil {
		if errors.Is(err, auth.ErrAppScopeDenied) || errors.Is(err, auth.ErrAppScopeRequired) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		h.logger.Error("failed to list share audiences",
			zap.String("owner", u.Username),
			zap.String("share_id", shareID),
			zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	type audienceResp struct {
		Type            string `json:"type"`
		TargetUserID    string `json:"targetUserId,omitempty"`
		TargetWallet    string `json:"targetWallet,omitempty"`
		SourceGroupID   string `json:"sourceGroupId,omitempty"`
		SourceGroupName string `json:"sourceGroupName,omitempty"`
	}
	resp := struct {
		Items []audienceResp `json:"items"`
	}{
		Items: make([]audienceResp, 0, len(audiences)),
	}
	for _, aud := range audiences {
		resp.Items = append(resp.Items, audienceResp{
			Type:            aud.AudienceType,
			TargetUserID:    aud.TargetUserID,
			TargetWallet:    aud.TargetWallet,
			SourceGroupID:   aud.SourceGroupID,
			SourceGroupName: aud.SourceGroupName,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
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
			if isIgnoredShareName(entry.Name()) {
				continue
			}
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
		if isIgnoredShareName(info.Name()) {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
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
	disposition := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("disposition")))
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
	if isIgnoredSharePath(fullPath) {
		http.Error(w, "Not Found", http.StatusNotFound)
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

	if disposition == "inline" {
		setInlineContentDisposition(w, info.Name())
	} else {
		setAttachmentContentDisposition(w, info.Name())
	}

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
		h.logger.Warn("invalid shared upload multipart body",
			zap.String("share_id", shareID),
			zap.String("path", relPath),
			zap.String("content_type", r.Header.Get("Content-Type")),
			zap.Int64("content_length", r.ContentLength),
			zap.Error(err),
		)
		http.Error(w, "Invalid upload body", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		h.logger.Warn("shared upload multipart missing file field",
			zap.String("share_id", shareID),
			zap.String("path", relPath),
			zap.String("content_type", r.Header.Get("Content-Type")),
			zap.Int64("content_length", r.ContentLength),
			zap.Error(err),
		)
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		http.Error(w, "Failed to create directory", http.StatusInternalServerError)
		return
	}

	if err := atomicfile.WriteAll(fullPath, file, 0o666); err != nil {
		http.Error(w, "Failed to write file", http.StatusInternalServerError)
		return
	}
	if err := h.mutationRecorder.EnsureDir(r.Context(), filepath.Dir(fullPath)); err != nil {
		h.logger.Error("failed to record share upload parent dir mutation", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if err := h.mutationRecorder.UpsertFile(r.Context(), fullPath); err != nil {
		h.logger.Error("failed to record share upload mutation", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
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
	if err := h.mutationRecorder.EnsureDir(r.Context(), fullPath); err != nil {
		h.logger.Error("failed to record share create folder mutation", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
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
	info, err := os.Stat(fromPath)
	if err != nil {
		http.Error(w, "Failed to stat source", http.StatusInternalServerError)
		return
	}

	if err := os.Rename(fromPath, toPath); err != nil {
		http.Error(w, "Failed to rename", http.StatusInternalServerError)
		return
	}
	if err := service.SyncUserSharePathsForOwnerMove(r.Context(), h.shareUserService.Repository(), h.shareUserService.Config(), owner, fromPath, toPath); err != nil {
		h.logger.Error("failed to sync share paths after share rename", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if err := h.mutationRecorder.EnsureDir(r.Context(), filepath.Dir(toPath)); err != nil {
		h.logger.Error("failed to record share rename parent dir mutation", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if err := h.mutationRecorder.MovePath(r.Context(), fromPath, toPath, info.IsDir()); err != nil {
		h.logger.Error("failed to record share rename mutation", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
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
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to stat target", http.StatusInternalServerError)
		return
	}

	if err := os.RemoveAll(fullPath); err != nil {
		http.Error(w, "Failed to delete", http.StatusInternalServerError)
		return
	}
	if err := h.mutationRecorder.RemovePath(r.Context(), fullPath, info.IsDir()); err != nil {
		h.logger.Error("failed to record share delete mutation", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
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

func isIgnoredSharePath(fullPath string) bool {
	return isIgnoredShareName(path.Base(strings.TrimSuffix(filepath.ToSlash(fullPath), "/")))
}

func isIgnoredShareName(name string) bool {
	return webdavfs.IsIgnoredName(strings.TrimSpace(name))
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

func (h *ShareUserHandler) buildShareUserItemResp(ctx context.Context, item *shareuser.ShareUserItem, targetWallet, ownerWallet, ownerName string) shareUserItemResp {
	perms := permissionsFromStored(item.Permissions)
	row := shareUserItemResp{
		ID:            item.ID,
		Name:          item.Name,
		Path:          item.Path,
		IsDir:         item.IsDir,
		Permissions:   permissionsToStrings(perms),
		TargetWallet:  targetWallet,
		TargetType:    item.AudienceType,
		TargetCount:   item.TargetCount,
		AudienceCount: item.AudienceCount,
		AllUsers:      item.AllUsers,
		OwnerWallet:   ownerWallet,
		OwnerName:     ownerName,
		CreatedAt:     item.CreatedAt.Format(timeLayout),
	}
	if item.ExpiresAt != nil {
		row.ExpiresAt = item.ExpiresAt.Format(timeLayout)
	}
	if item.AudienceType == "groups" {
		row.TargetWallet = ""
		row.TargetGroups = h.targetGroupsForShare(ctx, item.ID)
	}
	if item.AllUsers || item.AudienceType == shareuser.AudienceTypeAllUsers {
		row.TargetWallet = ""
	}
	return row
}

func (h *ShareUserHandler) targetGroupsForShare(ctx context.Context, shareID string) []shareTargetGroupResp {
	if h == nil || h.shareUserService == nil || h.shareUserService.Repository() == nil {
		return nil
	}
	audiences, err := h.shareUserService.Repository().ListAudiencesByShareID(ctx, shareID)
	if err != nil {
		if h.logger != nil {
			h.logger.Warn("failed to load share target groups",
				zap.String("share_id", shareID),
				zap.Error(err))
		}
		return nil
	}
	seen := make(map[string]struct{})
	groups := make([]shareTargetGroupResp, 0)
	for _, aud := range audiences {
		groupID := strings.TrimSpace(aud.SourceGroupID)
		if groupID == "" {
			continue
		}
		if _, ok := seen[groupID]; ok {
			continue
		}
		seen[groupID] = struct{}{}
		groups = append(groups, shareTargetGroupResp{
			ID:   groupID,
			Name: strings.TrimSpace(aud.SourceGroupName),
		})
	}
	return groups
}

func formatTargetWallet(item *shareuser.ShareUserItem) string {
	if item == nil {
		return ""
	}
	if item.AllUsers || item.AudienceType == shareuser.AudienceTypeAllUsers {
		return ""
	}
	if item.AudienceType == "groups" {
		return ""
	}
	if item.TargetCount > 1 || item.AudienceType == "addresses" {
		return fmt.Sprintf("@addresses:%d", item.TargetCount)
	}
	return strings.TrimSpace(item.TargetWalletAddress)
}

func formatTargetWalletForViewer(item *shareuser.ShareUserItem, viewerWallet string) string {
	if item == nil {
		return ""
	}
	if item.AllUsers || item.AudienceType == shareuser.AudienceTypeAllUsers {
		return ""
	}
	if item.AudienceType == "groups" {
		return ""
	}
	if item.TargetCount > 1 || item.AudienceType == "addresses" {
		return fmt.Sprintf("@addresses:%d", item.TargetCount)
	}
	if wallet := strings.TrimSpace(item.TargetWalletAddress); wallet != "" {
		return wallet
	}
	return strings.TrimSpace(viewerWallet)
}

func countNonEmpty(items []string) int {
	count := 0
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			count++
		}
	}
	return count
}
