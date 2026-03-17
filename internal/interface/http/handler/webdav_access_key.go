package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/accesskey"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

type WebDAVAccessKeyHandler struct {
	service *service.WebDAVAccessKeyService
	logger  *zap.Logger
}

func NewWebDAVAccessKeyHandler(service *service.WebDAVAccessKeyService, logger *zap.Logger) *WebDAVAccessKeyHandler {
	return &WebDAVAccessKeyHandler{
		service: service,
		logger:  logger,
	}
}

func (h *WebDAVAccessKeyHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := h.service.List(r.Context(), u)
	if err != nil {
		h.logger.Error("failed to list webdav access keys", zap.Error(err))
		http.Error(w, "Failed to list access keys", http.StatusInternalServerError)
		return
	}

	resp := struct {
		Items []map[string]any `json:"items"`
	}{Items: make([]map[string]any, 0, len(items))}
	for _, item := range items {
		bindingPaths, err := h.service.ListBindingPaths(r.Context(), u, item.ID)
		if err != nil {
			h.logger.Error("failed to list access key bindings",
				zap.String("access_key_id", item.ID),
				zap.Error(err))
			http.Error(w, "Failed to list access key bindings", http.StatusInternalServerError)
			return
		}

		row := map[string]any{
			"id":           item.ID,
			"name":         item.Name,
			"keyId":        item.KeyID,
			"permissions":  permissionsToStrings(permissionsFromStored(item.Permissions)),
			"bindingPaths": bindingPaths,
			"status":       item.Status,
			"createdAt":    item.CreatedAt.Format(timeLayout),
		}
		if item.ExpiresAt != nil {
			row["expiresAt"] = item.ExpiresAt.Format(timeLayout)
		}
		if item.LastUsedAt != nil {
			row["lastUsedAt"] = item.LastUsedAt.Format(timeLayout)
		}
		resp.Items = append(resp.Items, row)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *WebDAVAccessKeyHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name         string   `json:"name"`
		Permissions  []string `json:"permissions"`
		ExpiresValue int64    `json:"expiresValue"`
		ExpiresUnit  string   `json:"expiresUnit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	item, secret, err := h.service.Create(r.Context(), u, service.CreateWebDAVAccessKeyInput{
		Name:        req.Name,
		Permissions: req.Permissions,
		Expiry: service.ShareExpiryInput{
			ExpiresValue: req.ExpiresValue,
			ExpiresUnit:  req.ExpiresUnit,
		},
	})
	if err != nil {
		switch {
		case errors.Is(err, accesskey.ErrInvalidName),
			errors.Is(err, accesskey.ErrDuplicateName),
			errors.Is(err, accesskey.ErrInvalidRootPath),
			errors.Is(err, accesskey.ErrInvalidPerms),
			strings.Contains(err.Error(), "expiresUnit"),
			strings.Contains(err.Error(), "invalid permission"),
			strings.Contains(err.Error(), "unsupported expiresUnit"):
			status := http.StatusBadRequest
			if errors.Is(err, accesskey.ErrDuplicateName) {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
		default:
			h.logger.Error("failed to create webdav access key", zap.Error(err))
			http.Error(w, "Failed to create access key", http.StatusInternalServerError)
		}
		return
	}

	resp := map[string]any{
		"id":           item.ID,
		"name":         item.Name,
		"keyId":        item.KeyID,
		"keySecret":    secret,
		"permissions":  permissionsToStrings(permissionsFromStored(item.Permissions)),
		"bindingPaths": []string{},
		"status":       item.Status,
		"createdAt":    item.CreatedAt.Format(timeLayout),
	}
	if item.ExpiresAt != nil {
		resp["expiresAt"] = item.ExpiresAt.Format(timeLayout)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *WebDAVAccessKeyHandler) HandleRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	if err := h.service.Revoke(r.Context(), u, req.ID); err != nil {
		switch {
		case errors.Is(err, accesskey.ErrNotFound):
			http.Error(w, "access key not found", http.StatusNotFound)
		case errors.Is(err, accesskey.ErrAlreadyRevoked):
			http.Error(w, "access key already revoked", http.StatusBadRequest)
		default:
			h.logger.Error("failed to revoke webdav access key", zap.Error(err))
			http.Error(w, "Failed to revoke access key", http.StatusInternalServerError)
		}
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"message":"revoked successfully"}`))
}

func (h *WebDAVAccessKeyHandler) HandleBind(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	if err := h.service.BindPath(r.Context(), u, req.ID, req.Path); err != nil {
		switch {
		case errors.Is(err, accesskey.ErrNotFound):
			http.Error(w, "access key not found", http.StatusNotFound)
		case errors.Is(err, accesskey.ErrAlreadyRevoked):
			http.Error(w, "access key already revoked", http.StatusBadRequest)
		case errors.Is(err, accesskey.ErrInvalidRootPath):
			http.Error(w, "invalid path", http.StatusBadRequest)
		default:
			h.logger.Error("failed to bind webdav access key path", zap.Error(err))
			http.Error(w, "Failed to bind access key path", http.StatusInternalServerError)
		}
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"message":"bound successfully"}`))
}
