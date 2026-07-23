package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/auth"
	"github.com/yeying-community/warehouse/internal/domain/shareuser"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

type UploadSessionHandler struct {
	service *service.UploadSessionService
	logger  *zap.Logger
}

type uploadSessionResponse struct {
	ID           string                      `json:"id"`
	Scope        string                      `json:"scope"`
	Path         string                      `json:"path"`
	ShareID      string                      `json:"shareId,omitempty"`
	Size         int64                       `json:"size"`
	ChunkSize    int64                       `json:"chunkSize"`
	FileName     string                      `json:"fileName"`
	ContentType  string                      `json:"contentType,omitempty"`
	LastModified int64                       `json:"lastModified,omitempty"`
	Status       string                      `json:"status"`
	CreatedAt    string                      `json:"createdAt"`
	UpdatedAt    string                      `json:"updatedAt"`
	ExpiresAt    string                      `json:"expiresAt"`
	Parts        []service.UploadSessionPart `json:"parts"`
}

func NewUploadSessionHandler(uploadService *service.UploadSessionService, logger *zap.Logger) *UploadSessionHandler {
	return &UploadSessionHandler{service: uploadService, logger: logger}
}

func (h *UploadSessionHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
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
		Path         string `json:"path"`
		ShareID      string `json:"shareId"`
		Size         int64  `json:"size"`
		ChunkSize    int64  `json:"chunkSize"`
		FileName     string `json:"fileName"`
		ContentType  string `json:"contentType"`
		LastModified int64  `json:"lastModified"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	session, err := h.service.Create(r.Context(), u, service.UploadSessionCreateInput{
		Path:         req.Path,
		ShareID:      req.ShareID,
		Size:         req.Size,
		ChunkSize:    req.ChunkSize,
		FileName:     req.FileName,
		ContentType:  req.ContentType,
		LastModified: req.LastModified,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, buildUploadSessionResponse(session))
}

func (h *UploadSessionHandler) HandleItem(w http.ResponseWriter, r *http.Request) {
	const prefix = "/api/v1/public/uploads/sessions/"
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		http.Error(w, "session id is required", http.StatusBadRequest)
		return
	}
	id := parts[0]
	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		h.handleGet(w, r, id)
	case len(parts) == 1 && r.Method == http.MethodDelete:
		h.handleAbort(w, r, id)
	case len(parts) == 2 && parts[1] == "complete" && r.Method == http.MethodPost:
		h.handleComplete(w, r, id)
	case len(parts) == 3 && parts[1] == "parts" && r.Method == http.MethodPut:
		partNumber, err := strconv.Atoi(parts[2])
		if err != nil || partNumber < 1 {
			http.Error(w, "invalid part number", http.StatusBadRequest)
			return
		}
		h.handleUploadPart(w, r, id, partNumber)
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

func (h *UploadSessionHandler) handleGet(w http.ResponseWriter, r *http.Request, id string) {
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	session, err := h.service.Get(r.Context(), u, id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, buildUploadSessionResponse(session))
}

func (h *UploadSessionHandler) handleUploadPart(w http.ResponseWriter, r *http.Request, id string, partNumber int) {
	service.ClearUploadDeadlines(w, h.logger)
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	checksum := uploadSessionPartChecksum(r)
	if checksum == "" {
		http.Error(w, "checksum is required", http.StatusBadRequest)
		return
	}
	session, part, err := h.service.UploadPart(r.Context(), u, id, partNumber, checksum, r.Body)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"session": buildUploadSessionResponse(session),
		"part":    part,
	})
}

func (h *UploadSessionHandler) handleComplete(w http.ResponseWriter, r *http.Request, id string) {
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	session, err := h.service.Complete(r.Context(), u, id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, buildUploadSessionResponse(session))
}

func (h *UploadSessionHandler) handleAbort(w http.ResponseWriter, r *http.Request, id string) {
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if err := h.service.Abort(r.Context(), u, id); err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]bool{"aborted": true})
}

func buildUploadSessionResponse(session *service.UploadSession) uploadSessionResponse {
	if session == nil {
		return uploadSessionResponse{Parts: []service.UploadSessionPart{}}
	}
	return uploadSessionResponse{
		ID:           session.ID,
		Scope:        session.Scope,
		Path:         session.TargetPath,
		ShareID:      session.ShareID,
		Size:         session.Size,
		ChunkSize:    session.ChunkSize,
		FileName:     session.FileName,
		ContentType:  session.ContentType,
		LastModified: session.LastModified,
		Status:       session.Status,
		CreatedAt:    session.CreatedAt.Format(timeLayout),
		UpdatedAt:    session.UpdatedAt.Format(timeLayout),
		ExpiresAt:    session.ExpiresAt.Format(timeLayout),
		Parts:        service.UploadSessionParts(session),
	}
}

func (h *UploadSessionHandler) writeJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(data); err != nil && h.logger != nil {
		h.logger.Error("failed to write upload session response", zap.Error(err))
	}
}

func uploadSessionPartChecksum(r *http.Request) string {
	if r == nil {
		return ""
	}
	if value := strings.TrimSpace(r.Header.Get("X-Warehouse-Checksum-SHA256")); value != "" {
		return value
	}
	return strings.TrimSpace(r.Header.Get("x-amz-checksum-sha256"))
}

func (h *UploadSessionHandler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrUploadSessionNotFound):
		http.Error(w, "Not found", http.StatusNotFound)
	case errors.Is(err, service.ErrUploadSessionForbidden), errors.Is(err, auth.ErrAppScopeDenied), errors.Is(err, auth.ErrAppScopeRequired):
		http.Error(w, "Forbidden", http.StatusForbidden)
	case errors.Is(err, service.ErrUploadSessionTooLarge):
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
	case errors.Is(err, service.ErrUploadSessionChecksum):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, service.ErrUploadSessionInvalid):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, shareuser.ErrShareNotFound):
		http.Error(w, "Not found", http.StatusNotFound)
	case errors.Is(err, shareuser.ErrShareExpired):
		http.Error(w, "share expired", http.StatusGone)
	default:
		if h.logger != nil {
			h.logger.Error("upload session error", zap.Error(err))
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
