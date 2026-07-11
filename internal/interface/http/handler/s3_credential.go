package handler

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/yeying-community/warehouse/internal/domain/s3credential"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

type S3CredentialHandler struct {
	repo   repository.S3CredentialRepository
	logger *zap.Logger
}

func NewS3CredentialHandler(repo repository.S3CredentialRepository, logger *zap.Logger) *S3CredentialHandler {
	return &S3CredentialHandler{repo: repo, logger: logger}
}

func (h *S3CredentialHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := h.repo.ListByOwner(r.Context(), u.ID)
	if err != nil {
		h.logger.Error("failed to list s3 credentials", zap.Error(err))
		http.Error(w, "Failed to list S3 credentials", 500)
		return
	}
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, map[string]any{"id": item.ID, "name": item.Name, "accessKeyId": item.AccessKeyID, "status": item.Status, "createdAt": item.CreatedAt})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"items": rows})
}

func (h *S3CredentialHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", 400)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "name is required", 400)
		return
	}
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		http.Error(w, "failed to generate secret", 500)
		return
	}
	credential := &s3credential.Credential{ID: uuid.NewString(), OwnerUserID: u.ID, Name: req.Name, AccessKeyID: "AK" + randomID(), Secret: base64.RawURLEncoding.EncodeToString(secretBytes), Status: s3credential.StatusActive}
	if err := h.repo.Create(r.Context(), credential); err != nil {
		h.logger.Error("failed to create s3 credential", zap.Error(err))
		http.Error(w, "Failed to create S3 credential", 500)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": credential.ID, "name": credential.Name, "accessKeyId": credential.AccessKeyID, "secret": credential.Secret, "status": credential.Status, "warning": "The secret is shown once and cannot be recovered."})
}

func (h *S3CredentialHandler) HandleRevoke(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", 400)
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		http.Error(w, "id is required", 400)
		return
	}
	if err := h.repo.RevokeByID(r.Context(), u.ID, req.ID); err != nil {
		if errors.Is(err, s3credential.ErrNotFound) {
			http.Error(w, "S3 credential not found", 404)
			return
		}
		h.logger.Error("failed to revoke s3 credential", zap.Error(err))
		http.Error(w, "Failed to revoke S3 credential", 500)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func randomID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
