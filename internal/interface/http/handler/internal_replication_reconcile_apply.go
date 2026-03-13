package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/replication"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

type internalReconcileApplyItemRequest struct {
	ItemID        int64      `json:"itemId"`
	Path          string     `json:"path"`
	IsDir         bool       `json:"isDir"`
	ModifiedAt    *time.Time `json:"modifiedAt,omitempty"`
	ContentBase64 string     `json:"contentBase64,omitempty"`
}

type internalReconcileApplyBatchRequest struct {
	JobID int64                               `json:"jobId"`
	Items []internalReconcileApplyItemRequest `json:"items"`
}

type internalReconcileApplyBatchResponse struct {
	Success bool  `json:"success"`
	Applied int64 `json:"applied"`
}

// HandleReconcileApplyBatch applies one historical reconcile batch on standby.
func (h *InternalReplicationHandler) HandleReconcileApplyBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if err := h.requireStandbyRole(); err != nil {
		h.writeError(w, http.StatusConflict, err.Error())
		return
	}
	sourceNodeID := strings.TrimSpace(r.Header.Get(middleware.InternalNodeIDHeader))
	if sourceNodeID == "" {
		h.writeError(w, http.StatusBadRequest, "missing "+middleware.InternalNodeIDHeader)
		return
	}
	assignment, err := h.requireAssignedSource(r.Context(), sourceNodeID)
	if err != nil {
		h.writeStandbyAuthorizationError(w, err)
		return
	}
	requestGeneration, err := parseAssignmentGenerationHeader(r.Header.Get(middleware.InternalAssignmentGenerationHeader))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateAssignmentGeneration(requestGeneration, assignment, nil); err != nil {
		h.writeApplyError(w, err)
		return
	}

	var req internalReconcileApplyBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.JobID <= 0 {
		h.writeError(w, http.StatusBadRequest, "jobId is required")
		return
	}
	if len(req.Items) == 0 {
		h.writeError(w, http.StatusBadRequest, "items is required")
		return
	}

	for _, item := range req.Items {
		if err := h.applyReconcileItem(item); err != nil {
			h.logger.Error("failed to apply reconcile item",
				zap.Int64("job_id", req.JobID),
				zap.Int64("item_id", item.ItemID),
				zap.String("path", item.Path),
				zap.Error(err))
			h.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	h.writeJSON(w, http.StatusOK, internalReconcileApplyBatchResponse{
		Success: true,
		Applied: int64(len(req.Items)),
	})
}

func (h *InternalReplicationHandler) applyReconcileItem(item internalReconcileApplyItemRequest) error {
	storagePath := strings.TrimSpace(item.Path)
	if storagePath == "" {
		return fmt.Errorf("path is required")
	}
	fullPath, err := h.resolveReplicaPath(storagePath)
	if err != nil {
		return err
	}

	if item.IsDir {
		if fullPath == h.webdavRoot() {
			return nil
		}
		if err := os.MkdirAll(fullPath, 0o755); err != nil {
			return err
		}
		return applyReplicaMtime(fullPath, item.ModifiedAt)
	}

	rawContent, err := base64.StdEncoding.DecodeString(item.ContentBase64)
	if err != nil {
		return fmt.Errorf("decode file content for %q: %w", storagePath, err)
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return err
	}
	tempPath := fullPath + ".tmp.reconcile"
	if err := os.WriteFile(tempPath, rawContent, 0o644); err != nil {
		return fmt.Errorf("write temp reconcile file %q: %w", tempPath, err)
	}
	if err := os.Rename(tempPath, fullPath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("replace reconcile file %q: %w", fullPath, err)
	}
	return applyReplicaMtime(fullPath, item.ModifiedAt)
}

func (h *InternalReplicationHandler) dispatchReconcilePendingItems(ctx context.Context, jobID int64, peer *service.ResolvedReplicationPeer, targetNodeID string) (int64, error) {
	if h.reconcileStore == nil {
		return 0, fmt.Errorf("reconcile store is not configured")
	}
	if peer == nil || strings.TrimSpace(peer.BaseURL) == "" {
		return h.pendingCountSafe(ctx, jobID), fmt.Errorf("reconcile target peer base url is unavailable")
	}
	if peer.AssignmentGeneration == nil || *peer.AssignmentGeneration <= 0 {
		return h.pendingCountSafe(ctx, jobID), fmt.Errorf("reconcile target peer %q has no assignment generation", peer.NodeID)
	}

	batchSize := h.config.Internal.Replication.BatchSize
	if batchSize <= 0 {
		batchSize = 32
	}
	client := &http.Client{Timeout: h.config.Internal.Replication.RequestTimeout}

	for {
		items, err := h.reconcileStore.ListPendingItems(ctx, jobID, batchSize)
		if err != nil {
			return 0, err
		}
		if len(items) == 0 {
			return 0, nil
		}

		payloadItems := make([]internalReconcileApplyItemRequest, 0, len(items))
		itemIDs := make([]int64, 0, len(items))
		for _, item := range items {
			reqItem := internalReconcileApplyItemRequest{
				ItemID:     item.ID,
				Path:       item.Path,
				IsDir:      item.IsDir,
				ModifiedAt: item.ModifiedAt,
			}
			if !item.IsDir {
				fullPath, err := h.resolveReconcileLocalPath(item.Path)
				if err != nil {
					return h.pendingCountSafe(ctx, jobID), err
				}
				raw, err := os.ReadFile(fullPath)
				if err != nil {
					return h.pendingCountSafe(ctx, jobID), fmt.Errorf("read local reconcile file %q: %w", fullPath, err)
				}
				reqItem.ContentBase64 = base64.StdEncoding.EncodeToString(raw)
			}
			payloadItems = append(payloadItems, reqItem)
			itemIDs = append(itemIDs, item.ID)
		}

		if err := h.sendReconcileBatch(ctx, client, peer.BaseURL, targetNodeID, jobID, *peer.AssignmentGeneration, payloadItems); err != nil {
			return h.pendingCountSafe(ctx, jobID), err
		}
		if err := h.reconcileStore.UpdateItemsState(ctx, itemIDs, replication.ReconcileItemStateApplied); err != nil {
			return h.pendingCountSafe(ctx, jobID), err
		}
	}
}

func (h *InternalReplicationHandler) sendReconcileBatch(
	ctx context.Context,
	client *http.Client,
	baseURL, targetNodeID string,
	jobID int64,
	assignmentGeneration int64,
	items []internalReconcileApplyItemRequest,
) error {
	body, err := json.Marshal(internalReconcileApplyBatchRequest{
		JobID: jobID,
		Items: items,
	})
	if err != nil {
		return fmt.Errorf("marshal reconcile batch request: %w", err)
	}

	requestURL := strings.TrimRight(baseURL, "/") + "/api/v1/internal/replication/reconcile/apply-batch"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create reconcile batch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	h.signInternalRequest(req, payloadSHA256Hex(body))
	req.Header.Set(middleware.InternalAssignmentGenerationHeader, fmt.Sprintf("%d", assignmentGeneration))
	req.Header.Set("X-Warehouse-Reconcile-Target-Node-Id", targetNodeID)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send reconcile batch request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("reconcile peer returned %s: %s", resp.Status, msg)
	}
	return nil
}

func (h *InternalReplicationHandler) signInternalRequest(req *http.Request, payloadHash string) {
	timestamp := time.Now().UTC().Format(time.RFC3339)
	req.Header.Set(middleware.InternalNodeIDHeader, h.config.Node.ID)
	req.Header.Set(middleware.InternalTimestampHeader, timestamp)
	req.Header.Set(middleware.InternalContentSHA256Header, payloadHash)
	req.Header.Set(middleware.InternalSignatureHeader, middleware.SignInternalRequest(
		req.Method,
		req.URL.Path,
		h.config.Node.ID,
		timestamp,
		payloadHash,
		h.config.Internal.Replication.SharedSecret,
	))
}

func (h *InternalReplicationHandler) resolveReconcileLocalPath(storagePath string) (string, error) {
	cleaned := path.Clean("/" + strings.TrimSpace(storagePath))
	if cleaned == "/" || strings.HasPrefix(cleaned, "/..") {
		return "", fmt.Errorf("invalid storage path %q", storagePath)
	}
	return filepath.Join(filepath.Clean(h.config.WebDAV.Directory), filepath.FromSlash(strings.TrimPrefix(cleaned, "/"))), nil
}

func (h *InternalReplicationHandler) pendingCountSafe(ctx context.Context, jobID int64) int64 {
	pending, err := h.reconcileStore.CountPendingItems(ctx, jobID)
	if err != nil {
		return 0
	}
	return pending
}

func payloadSHA256Hex(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func applyReplicaMtime(fullPath string, modifiedAt *time.Time) error {
	if modifiedAt == nil {
		return nil
	}
	ts := modifiedAt.UTC()
	if err := os.Chtimes(fullPath, ts, ts); err != nil {
		return fmt.Errorf("set mtime for %q: %w", fullPath, err)
	}
	return nil
}
