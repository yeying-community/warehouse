package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/replication"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

type internalReplicationBootstrapMarkRequest struct {
	OutboxID *int64 `json:"outboxId,omitempty"`
}

type internalReplicationBootstrapMarkResponse struct {
	Success                     bool   `json:"success"`
	SourceNodeID                string `json:"sourceNodeId"`
	TargetNodeID                string `json:"targetNodeId"`
	LastAppliedOutboxID         int64  `json:"lastAppliedOutboxId"`
	PreviousLastAppliedOutboxID *int64 `json:"previousLastAppliedOutboxId,omitempty"`
	UsedCurrentOutboxID         bool   `json:"usedCurrentOutboxId,omitempty"`
}

// HandleBootstrapMark records the baseline outbox sequence after an offline full copy to standby.
func (h *InternalReplicationHandler) HandleBootstrapMark(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if err := h.requireStandbyRole(); err != nil {
		h.writeError(w, http.StatusConflict, err.Error())
		return
	}

	var req internalReplicationBootstrapMarkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid request body")
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

	targetNodeID := h.config.Node.ID
	currentOffset, err := h.offsets.Get(r.Context(), sourceNodeID, targetNodeID)
	if err != nil && !errors.Is(err, replication.ErrOffsetNotFound) {
		h.logger.Error("failed to load current replication offset before bootstrap mark",
			zap.String("source_node_id", sourceNodeID),
			zap.String("target_node_id", targetNodeID),
			zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to load current replication offset")
		return
	}
	if err := validateBootstrapAssignmentGeneration(requestGeneration, assignment, currentOffset); err != nil {
		h.writeApplyError(w, err)
		return
	}

	baselineOutboxID, usedCurrentOutboxID, err := h.resolveBootstrapOutboxID(r.Context(), sourceNodeID, targetNodeID, req.OutboxID)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var previousLastApplied *int64
	if currentOffset != nil {
		previousLastApplied = &currentOffset.LastAppliedOutboxID
		if baselineOutboxID < currentOffset.LastAppliedOutboxID {
			h.writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":               "bootstrap outbox id cannot move backwards",
				"code":                http.StatusConflict,
				"success":             false,
				"requestedOutboxId":   baselineOutboxID,
				"lastAppliedOutboxId": currentOffset.LastAppliedOutboxID,
				"sourceNodeId":        sourceNodeID,
				"targetNodeId":        targetNodeID,
			})
			return
		}
	}

	now := time.Now()
	if err := h.offsets.Upsert(r.Context(), &replication.Offset{
		SourceNodeID:         sourceNodeID,
		TargetNodeID:         targetNodeID,
		AssignmentGeneration: effectiveAssignmentGeneration(requestGeneration, assignment),
		LastAppliedOutboxID:  baselineOutboxID,
		LastAppliedAt:        now,
		UpdatedAt:            now,
	}); err != nil {
		h.logger.Error("failed to persist replication bootstrap baseline",
			zap.String("source_node_id", sourceNodeID),
			zap.String("target_node_id", targetNodeID),
			zap.Int64("baseline_outbox_id", baselineOutboxID),
			zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to persist replication bootstrap baseline")
		return
	}

	h.writeJSON(w, http.StatusOK, internalReplicationBootstrapMarkResponse{
		Success:                     true,
		SourceNodeID:                sourceNodeID,
		TargetNodeID:                targetNodeID,
		LastAppliedOutboxID:         baselineOutboxID,
		PreviousLastAppliedOutboxID: previousLastApplied,
		UsedCurrentOutboxID:         usedCurrentOutboxID,
	})
}

func (h *InternalReplicationHandler) resolveBootstrapOutboxID(ctx context.Context, sourceNodeID, targetNodeID string, requestedOutboxID *int64) (int64, bool, error) {
	if requestedOutboxID != nil {
		if *requestedOutboxID < 0 {
			return 0, false, errors.New("outboxId must be greater than or equal to zero")
		}
		return *requestedOutboxID, false, nil
	}

	if h.outbox == nil {
		return 0, false, errors.New("outbox repository is not configured")
	}

	summary, err := h.outbox.GetStatusSummary(ctx, sourceNodeID, targetNodeID)
	if err != nil {
		return 0, false, err
	}
	if summary == nil || summary.LastOutboxID == nil {
		return 0, true, nil
	}
	return *summary.LastOutboxID, true, nil
}

func (h *InternalReplicationHandler) sendBootstrapMark(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	assignmentGeneration int64,
	outboxID int64,
) error {
	body, err := json.Marshal(internalReplicationBootstrapMarkRequest{
		OutboxID: int64Pointer(outboxID),
	})
	if err != nil {
		return fmt.Errorf("marshal bootstrap mark request: %w", err)
	}

	requestURL := strings.TrimRight(baseURL, "/") + "/api/v1/internal/replication/bootstrap/mark"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create bootstrap mark request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	h.signInternalRequest(req, payloadSHA256Hex(body))
	req.Header.Set(middleware.InternalAssignmentGenerationHeader, fmt.Sprintf("%d", assignmentGeneration))

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send bootstrap mark request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("bootstrap peer returned %s: %s", resp.Status, msg)
	}
	return nil
}
