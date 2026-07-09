package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/notification"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

type NotificationHandler struct {
	service        *service.NotificationService
	adminAddresses map[string]struct{}
	logger         *zap.Logger
}

func NewNotificationHandler(service *service.NotificationService, adminAddresses []string, logger *zap.Logger) *NotificationHandler {
	admins := make(map[string]struct{}, len(adminAddresses))
	for _, raw := range adminAddresses {
		addr := strings.ToLower(strings.TrimSpace(raw))
		if addr != "" {
			admins[addr] = struct{}{}
		}
	}
	return &NotificationHandler{
		service:        service,
		adminAddresses: admins,
		logger:         logger,
	}
}

func (h *NotificationHandler) canReceiveAdminNotifications(u *user.User) bool {
	if h == nil || u == nil || len(h.adminAddresses) == 0 {
		return false
	}
	addr := strings.ToLower(strings.TrimSpace(u.WalletAddress))
	if addr == "" {
		return false
	}
	_, ok := h.adminAddresses[addr]
	return ok
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
	items, err := h.service.ListForCurrentUser(r.Context(), u, h.canReceiveAdminNotifications(u), parseLimit(r, 20))
	if err != nil {
		if isRequestCanceled(err) {
			return
		}
		h.logger.Error("failed to list notifications", zap.String("username", u.Username), zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to list notifications")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"items":       notificationResponses(items),
		"canAnnounce": h.canReceiveAdminNotifications(u),
	})
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
	count, err := h.service.UnreadCountForCurrentUser(r.Context(), u, h.canReceiveAdminNotifications(u))
	if err != nil {
		if isRequestCanceled(err) {
			return
		}
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
	if err := h.service.MarkReadForCurrentUser(r.Context(), u, h.canReceiveAdminNotifications(u), req.IDs); err != nil {
		if isRequestCanceled(err) {
			return
		}
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
	if err := h.service.MarkAllReadForCurrentUser(r.Context(), u, h.canReceiveAdminNotifications(u)); err != nil {
		if isRequestCanceled(err) {
			return
		}
		h.logger.Error("failed to mark all notifications read", zap.String("username", u.Username), zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to mark all notifications read")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"message": "ok"})
}

func (h *NotificationHandler) HandlePreferences(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.GetPreferences(r.Context(), u)
		if err != nil {
			h.logger.Error("failed to get notification preferences", zap.String("username", u.Username), zap.Error(err))
			h.writeError(w, http.StatusInternalServerError, "Failed to get notification preferences")
			return
		}
		h.writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var req struct {
			Type    string `json:"type"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.writeError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		if err := h.service.SetPreference(r.Context(), u, req.Type, req.Enabled); err != nil {
			h.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.writeJSON(w, http.StatusOK, map[string]any{"message": "ok"})
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *NotificationHandler) HandleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	h.streamCounts(w, r, func(ctx context.Context) (map[string]int, error) {
		count, err := h.service.UnreadCountForCurrentUser(ctx, u, h.canReceiveAdminNotifications(u))
		return map[string]int{"count": count}, err
	})
}

func (h *NotificationHandler) HandleAdminList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	items, err := h.service.ListForAdmin(r.Context(), parseLimit(r, 20))
	if err != nil {
		if isRequestCanceled(err) {
			return
		}
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
		if isRequestCanceled(err) {
			return
		}
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
		if isRequestCanceled(err) {
			return
		}
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
		if isRequestCanceled(err) {
			return
		}
		h.logger.Error("failed to mark all admin notifications read", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to mark all admin notifications read")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"message": "ok"})
}

func (h *NotificationHandler) HandleAdminCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		RecipientRole   string   `json:"recipientRole"`
		TargetUsernames []string `json:"targetUsernames"`
		Title           string   `json:"title"`
		Content         string   `json:"content"`
		Severity        string   `json:"severity"`
		ActionURL       string   `json:"actionUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	count, err := h.service.CreateAnnouncement(r.Context(), service.AnnouncementInput{
		RecipientRole:   req.RecipientRole,
		TargetUsernames: req.TargetUsernames,
		Title:           req.Title,
		Content:         req.Content,
		Severity:        req.Severity,
		ActionURL:       req.ActionURL,
	})
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"message": "ok", "count": count})
}

func (h *NotificationHandler) HandleAdminStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	h.streamCounts(w, r, func(ctx context.Context) (map[string]int, error) {
		count, err := h.service.UnreadCountForAdmin(ctx)
		return map[string]int{"admin": count}, err
	})
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

func isRequestCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
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

func (h *NotificationHandler) streamCounts(w http.ResponseWriter, r *http.Request, load func(context.Context) (map[string]int, error)) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "Streaming is not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	write := func() bool {
		counts, err := load(r.Context())
		if err != nil {
			if isRequestCanceled(err) {
				return false
			}
			h.logger.Warn("failed to load notification stream counts", zap.Error(err))
			counts = map[string]int{}
		}
		payload, err := json.Marshal(counts)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "event: unread\ndata: %s\n\n", payload); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	if !write() {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !write() {
				return
			}
		}
	}
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
