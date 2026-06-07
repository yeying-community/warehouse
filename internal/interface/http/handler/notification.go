package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/notification"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

type NotificationHandler struct {
	service *service.NotificationService
	logger  *zap.Logger
}

func NewNotificationHandler(service *service.NotificationService, logger *zap.Logger) *NotificationHandler {
	return &NotificationHandler{
		service: service,
		logger:  logger,
	}
}

func (h *NotificationHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	items, err := h.service.ListForUser(r.Context(), u, parseLimit(r, 20))
	if err != nil {
		h.logger.Error("failed to list notifications", zap.String("username", u.Username), zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to list notifications")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"items": notificationResponses(items)})
}

func (h *NotificationHandler) HandleUnreadCount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	count, err := h.service.UnreadCountForUser(r.Context(), u)
	if err != nil {
		h.logger.Error("failed to count notifications", zap.String("username", u.Username), zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to count notifications")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"count": count})
}

func (h *NotificationHandler) HandleMarkRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := h.service.MarkReadForUser(r.Context(), u, req.IDs); err != nil {
		h.logger.Error("failed to mark notifications read", zap.String("username", u.Username), zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to mark notifications read")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"message": "ok"})
}

func (h *NotificationHandler) HandleMarkAllRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if err := h.service.MarkAllReadForUser(r.Context(), u); err != nil {
		h.logger.Error("failed to mark all notifications read", zap.String("username", u.Username), zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to mark all notifications read")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"message": "ok"})
}

func (h *NotificationHandler) HandleAdminList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	items, err := h.service.ListForAdmin(r.Context(), parseLimit(r, 20))
	if err != nil {
		h.logger.Error("failed to list admin notifications", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to list admin notifications")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"items": notificationResponses(items)})
}

func (h *NotificationHandler) HandleAdminUnreadCount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	count, err := h.service.UnreadCountForAdmin(r.Context())
	if err != nil {
		h.logger.Error("failed to count admin notifications", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to count admin notifications")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"count": count})
}

func (h *NotificationHandler) HandleAdminMarkRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := h.service.MarkReadForAdmin(r.Context(), req.IDs); err != nil {
		h.logger.Error("failed to mark admin notifications read", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to mark admin notifications read")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"message": "ok"})
}

func (h *NotificationHandler) HandleAdminMarkAllRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if err := h.service.MarkAllReadForAdmin(r.Context()); err != nil {
		h.logger.Error("failed to mark all admin notifications read", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to mark all admin notifications read")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"message": "ok"})
}

type notificationResponse struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Severity  string `json:"severity"`
	ActionURL string `json:"actionUrl,omitempty"`
	ReadAt    string `json:"readAt,omitempty"`
	CreatedAt string `json:"createdAt"`
}

func notificationResponses(items []*notification.Notification) []notificationResponse {
	resp := make([]notificationResponse, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		row := notificationResponse{
			ID:        item.ID,
			Type:      item.Type,
			Title:     item.Title,
			Content:   item.Content,
			Severity:  item.Severity,
			ActionURL: item.ActionURL,
			CreatedAt: item.CreatedAt.Format(timeLayout),
		}
		if item.ReadAt != nil {
			row.ReadAt = item.ReadAt.Format(timeLayout)
		}
		resp = append(resp, row)
	}
	return resp
}

func parseLimit(r *http.Request, fallback int) int {
	value := r.URL.Query().Get("limit")
	if value == "" {
		return fallback
	}
	limit, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return limit
}

func (h *NotificationHandler) writeJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
	}
}

func (h *NotificationHandler) writeError(w http.ResponseWriter, code int, message string) {
	h.writeJSON(w, code, map[string]any{
		"error":   message,
		"code":    code,
		"success": false,
	})
}
