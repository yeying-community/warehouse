package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/cluster"
	"github.com/yeying-community/warehouse/internal/domain/replication"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

type replicationOutboxStatusReader interface {
	GetStatusSummary(rctx context.Context, sourceNodeID, targetNodeID string) (*replication.OutboxStatus, error)
}

type replicationOffsetStore interface {
	Get(ctx context.Context, sourceNodeID, targetNodeID string) (*replication.Offset, error)
	Upsert(ctx context.Context, offset *replication.Offset) error
}

type replicationReconcileStore interface {
	CreateJob(ctx context.Context, job *replication.ReconcileJob) error
	ReplaceItems(ctx context.Context, jobID int64, items []*replication.ReconcileItem) error
	UpdateJobResult(ctx context.Context, jobID int64, status string, scannedItems, pendingItems int64, completedAt *time.Time, lastError *string) error
	GetLatestJob(ctx context.Context, sourceNodeID, targetNodeID string) (*replication.ReconcileJob, error)
	ListPendingItems(ctx context.Context, jobID int64, limit int) ([]*replication.ReconcileItem, error)
	UpdateItemsState(ctx context.Context, itemIDs []int64, state string) error
	CountPendingItems(ctx context.Context, jobID int64) (int64, error)
}

type replicationReconcileScanner interface {
	Scan(ctx context.Context) ([]*replication.ReconcileItem, error)
}

const (
	defaultPeriodicReconcileInterval = 30 * time.Second
)

// InternalReplicationHandler exposes replication-related internal control endpoints.
type InternalReplicationHandler struct {
	logger                *zap.Logger
	config                *config.Config
	outbox                replicationOutboxStatusReader
	offsets               replicationOffsetStore
	reconcileStore        replicationReconcileStore
	reconcileScanner      replicationReconcileScanner
	peerResolver          service.ReplicationPeerResolver
	assignments           repository.ClusterReplicationAssignmentRepository
	autoReconcileInterval time.Duration
	reconcileExecutionMu  sync.Mutex
}

// NewInternalReplicationHandler creates a new internal replication handler.
func NewInternalReplicationHandler(
	cfg *config.Config,
	logger *zap.Logger,
	outbox replicationOutboxStatusReader,
	offsets replicationOffsetStore,
	reconcileStore replicationReconcileStore,
	reconcileScanner replicationReconcileScanner,
	peerResolver service.ReplicationPeerResolver,
	assignments ...repository.ClusterReplicationAssignmentRepository,
) *InternalReplicationHandler {
	var assignmentRepo repository.ClusterReplicationAssignmentRepository
	if len(assignments) > 0 {
		assignmentRepo = assignments[0]
	}
	return &InternalReplicationHandler{
		logger:                logger,
		config:                cfg,
		outbox:                outbox,
		offsets:               offsets,
		reconcileStore:        reconcileStore,
		reconcileScanner:      reconcileScanner,
		peerResolver:          peerResolver,
		assignments:           assignmentRepo,
		autoReconcileInterval: defaultPeriodicReconcileInterval,
	}
}

type internalReplicationStatusResponse struct {
	Node        internalNodeStatus          `json:"node"`
	Replication internalReplicationStatus   `json:"replication"`
	Reconcile   *internalReconcileJobStatus `json:"reconcile,omitempty"`
}

type internalNodeStatus struct {
	ID           string `json:"id"`
	Role         string `json:"role"`
	AdvertiseURL string `json:"advertiseUrl,omitempty"`
}

type internalReplicationStatus struct {
	Enabled                bool       `json:"enabled"`
	Mode                   string     `json:"mode"`
	State                  string     `json:"state"`
	WorkerEnabled          bool       `json:"workerEnabled"`
	ResolvedPeerNodeID     string     `json:"resolvedPeerNodeId,omitempty"`
	ResolvedPeerBaseURL    string     `json:"resolvedPeerBaseUrl,omitempty"`
	ResolvedPeerSource     string     `json:"resolvedPeerSource,omitempty"`
	ResolvedGeneration     *int64     `json:"resolvedGeneration,omitempty"`
	ResolvedPeerHealthy    *bool      `json:"resolvedPeerHealthy,omitempty"`
	PeerLastHeartbeatAt    *time.Time `json:"peerLastHeartbeatAt,omitempty"`
	LastOutboxID           *int64     `json:"lastOutboxId,omitempty"`
	LastAppliedOutboxID    *int64     `json:"lastAppliedOutboxId,omitempty"`
	LastAppliedGeneration  *int64     `json:"lastAppliedGeneration,omitempty"`
	LastDispatchedOutboxID *int64     `json:"lastDispatchedOutboxId,omitempty"`
	PendingEvents          *int64     `json:"pendingEvents,omitempty"`
	FailedEvents           *int64     `json:"failedEvents,omitempty"`
	LagSeconds             *int64     `json:"lagSeconds,omitempty"`
	LastFailedOutboxID     *int64     `json:"lastFailedOutboxId,omitempty"`
	LastFailureAttempt     *int       `json:"lastFailureAttempt,omitempty"`
	NextRetryAt            *time.Time `json:"nextRetryAt,omitempty"`
	LastError              *string    `json:"lastError,omitempty"`
	Notes                  []string   `json:"notes,omitempty"`
}

type internalReconcileJobStatus struct {
	Enabled              bool       `json:"enabled"`
	LatestJobID          int64      `json:"latestJobId"`
	Status               string     `json:"status"`
	AssignmentGeneration *int64     `json:"assignmentGeneration,omitempty"`
	WatermarkOutboxID    int64      `json:"watermarkOutboxId"`
	ScannedItems         int64      `json:"scannedItems"`
	PendingItems         int64      `json:"pendingItems"`
	StartedAt            time.Time  `json:"startedAt"`
	CompletedAt          *time.Time `json:"completedAt,omitempty"`
	LastError            *string    `json:"lastError,omitempty"`
}

// HandleStatus returns the current internal replication configuration and persisted status summary.
func (h *InternalReplicationHandler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	replicationCfg := h.config.Replication
	response := internalReplicationStatusResponse{
		Node: internalNodeStatus{
			ID:           h.config.Node.ID,
			Role:         h.config.Node.Role,
			AdvertiseURL: strings.TrimSpace(h.config.Node.AdvertiseURL),
		},
		Replication: internalReplicationStatus{
			Enabled:       replicationCfg.Enabled,
			Mode:          "internal_async",
			State:         "disabled",
			WorkerEnabled: h.workerEnabled(),
		},
	}
	if !replicationCfg.Enabled {
		response.Replication.Notes = []string{"internal replication is disabled"}
		h.writeJSON(w, http.StatusOK, response)
		return
	}

	targetNodeID := strings.TrimSpace(r.URL.Query().Get("targetNodeId"))
	resolvedPeer, err := h.resolveTargetPeerForNode(r.Context(), targetNodeID, false)
	if err != nil {
		h.logger.Error("failed to resolve replication peer", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "Failed to resolve replication peer")
		return
	}
	if resolvedPeer != nil {
		response.Replication.ResolvedPeerNodeID = resolvedPeer.NodeID
		response.Replication.ResolvedPeerBaseURL = resolvedPeer.BaseURL
		response.Replication.ResolvedPeerSource = resolvedPeer.Source
		response.Replication.ResolvedGeneration = resolvedPeer.AssignmentGeneration
		response.Replication.PeerLastHeartbeatAt = resolvedPeer.LastHeartbeatAt
		response.Replication.ResolvedPeerHealthy = boolPointer(resolvedPeer.Healthy)
	}

	sourceNodeID, targetNodeID := h.replicationPair(resolvedPeer)
	if h.outbox != nil && sourceNodeID != "" && targetNodeID != "" {
		summary, err := h.outbox.GetStatusSummary(r.Context(), sourceNodeID, targetNodeID)
		if err != nil {
			h.logger.Error("failed to load replication outbox status",
				zap.String("source_node_id", sourceNodeID),
				zap.String("target_node_id", targetNodeID),
				zap.Error(err))
			h.writeError(w, http.StatusInternalServerError, "Failed to load replication outbox status")
			return
		}
		if summary != nil {
			response.Replication.PendingEvents = &summary.PendingEvents
			response.Replication.FailedEvents = &summary.FailedEvents
			response.Replication.LastOutboxID = summary.LastOutboxID
			response.Replication.LastDispatchedOutboxID = summary.LastDispatchedOutboxID
			response.Replication.LastFailedOutboxID = summary.LastFailedOutboxID
			response.Replication.LastFailureAttempt = summary.LastFailureAttempt
			response.Replication.NextRetryAt = summary.NextRetryAt
			response.Replication.LastError = summary.LastError
			if summary.OldestPendingCreatedAt != nil {
				lagSeconds := int64(time.Since(*summary.OldestPendingCreatedAt).Seconds())
				if lagSeconds < 0 {
					lagSeconds = 0
				}
				response.Replication.LagSeconds = &lagSeconds
			}
		}
	}

	if h.offsets != nil && sourceNodeID != "" && targetNodeID != "" {
		offset, err := h.offsets.Get(r.Context(), sourceNodeID, targetNodeID)
		if err != nil {
			if !errors.Is(err, replication.ErrOffsetNotFound) {
				h.logger.Error("failed to load replication offset",
					zap.String("source_node_id", sourceNodeID),
					zap.String("target_node_id", targetNodeID),
					zap.Error(err))
				h.writeError(w, http.StatusInternalServerError, "Failed to load replication offset")
				return
			}
		} else {
			response.Replication.LastAppliedOutboxID = &offset.LastAppliedOutboxID
			response.Replication.LastAppliedGeneration = offset.AssignmentGeneration
		}
	}

	if h.reconcileStore != nil && sourceNodeID != "" && targetNodeID != "" {
		job, err := h.reconcileStore.GetLatestJob(r.Context(), sourceNodeID, targetNodeID)
		if err != nil {
			if !errors.Is(err, replication.ErrReconcileJobNotFound) {
				h.logger.Error("failed to load reconcile status",
					zap.String("source_node_id", sourceNodeID),
					zap.String("target_node_id", targetNodeID),
					zap.Error(err))
				h.writeError(w, http.StatusInternalServerError, "Failed to load reconcile status")
				return
			}
		} else {
			response.Reconcile = &internalReconcileJobStatus{
				Enabled:              true,
				LatestJobID:          job.ID,
				Status:               job.Status,
				AssignmentGeneration: job.AssignmentGeneration,
				WatermarkOutboxID:    job.WatermarkOutboxID,
				ScannedItems:         job.ScannedItems,
				PendingItems:         job.PendingItems,
				StartedAt:            job.StartedAt,
				CompletedAt:          job.CompletedAt,
				LastError:            job.LastError,
			}
		}
	}

	response.Replication.State = h.determineState(response.Replication)
	response.Replication.Notes = h.buildNotes(response.Replication)
	h.writeJSON(w, http.StatusOK, response)
}

type internalReconcileStartRequest struct {
	TargetNodeID string `json:"targetNodeId,omitempty"`
}

type internalReconcileStartResponse struct {
	Success           bool   `json:"success"`
	JobID             int64  `json:"jobId"`
	SourceNodeID      string `json:"sourceNodeId"`
	TargetNodeID      string `json:"targetNodeId"`
	WatermarkOutboxID int64  `json:"watermarkOutboxId"`
	ScannedItems      int64  `json:"scannedItems"`
	PendingItems      int64  `json:"pendingItems"`
	Status            string `json:"status"`
}

// HandleReconcileStart scans active local data and persists a reconcile job with pending items.
func (h *InternalReplicationHandler) HandleReconcileStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(h.config.Node.Role), "active") {
		h.writeError(w, http.StatusConflict, "reconcile start is only available on active nodes")
		return
	}
	if h.reconcileStore == nil || h.reconcileScanner == nil {
		h.writeError(w, http.StatusConflict, "reconcile components are not configured")
		return
	}

	var req internalReconcileStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		h.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	targetNodeID := strings.TrimSpace(req.TargetNodeID)
	resp, err := h.startReconcile(r.Context(), targetNodeID)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// RunStartupReconcile tries to trigger one full historical reconcile after startup.
func (h *InternalReplicationHandler) RunStartupReconcile(ctx context.Context) {
	if !strings.EqualFold(strings.TrimSpace(h.config.Node.Role), "active") {
		return
	}
	if h.reconcileStore == nil || h.reconcileScanner == nil {
		return
	}

	completedTargets := make(map[string]struct{})
	const maxAttempts = 24
	const retryInterval = 5 * time.Second
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		peers, err := h.resolveTargetPeers(ctx, false)
		if err != nil {
			h.logger.Warn("startup reconcile failed to resolve target peers, will retry",
				zap.Int("attempt", attempt),
				zap.Int("max_attempts", maxAttempts),
				zap.Error(err))
			if !h.waitStartupReconcileRetry(ctx, retryInterval) {
				return
			}
			continue
		}
		if len(peers) == 0 {
			h.logger.Info("startup reconcile is waiting for an effective assignment",
				zap.Int("attempt", attempt),
				zap.Int("max_attempts", maxAttempts),
				zap.Error(service.ErrReplicationAssignmentUnavailable))
			if !h.waitStartupReconcileRetry(ctx, retryInterval) {
				return
			}
			continue
		}

		attemptedTargets := 0
		pendingRetry := false
		for _, peer := range peers {
			if peer == nil {
				continue
			}
			resolvedTargetNodeID := strings.TrimSpace(peer.NodeID)
			if resolvedTargetNodeID == "" {
				continue
			}
			if _, ok := completedTargets[resolvedTargetNodeID]; ok {
				continue
			}
			attemptedTargets++

			resp, err := h.startReconcile(ctx, resolvedTargetNodeID)
			if err == nil {
				completedTargets[resolvedTargetNodeID] = struct{}{}
				h.logger.Info("startup reconcile finished",
					zap.Int64("job_id", resp.JobID),
					zap.String("target_node_id", resp.TargetNodeID),
					zap.Int64("scanned_items", resp.ScannedItems),
					zap.Int64("pending_items", resp.PendingItems))
				continue
			}

			pendingRetry = true
			if errors.Is(err, service.ErrReplicationAssignmentUnavailable) {
				h.logger.Info("startup reconcile is waiting for an effective assignment",
					zap.Int("attempt", attempt),
					zap.Int("max_attempts", maxAttempts),
					zap.String("target_node_id", resolvedTargetNodeID),
					zap.Error(err))
				continue
			}
			h.logger.Warn("startup reconcile attempt failed, will retry",
				zap.Int("attempt", attempt),
				zap.Int("max_attempts", maxAttempts),
				zap.String("target_node_id", resolvedTargetNodeID),
				zap.Error(err))
		}

		if pendingRetry {
			if !h.waitStartupReconcileRetry(ctx, retryInterval) {
				return
			}
			continue
		}
		if attemptedTargets == 0 {
			return
		}
		if len(completedTargets) > 0 {
			return
		}
	}

	h.logger.Warn("startup reconcile stopped after max retry attempts",
		zap.Int("max_attempts", maxAttempts))
}

// RunAutoReconcile runs startup reconcile once, then periodically re-checks whether any standby still needs a baseline.
func (h *InternalReplicationHandler) RunAutoReconcile(ctx context.Context) {
	if !strings.EqualFold(strings.TrimSpace(h.config.Node.Role), "active") {
		return
	}
	if h.reconcileStore == nil || h.reconcileScanner == nil {
		return
	}

	h.RunStartupReconcile(ctx)

	interval := h.autoReconcileInterval
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := h.runPeriodicReconcileOnce(ctx); err != nil && !errors.Is(err, context.Canceled) && h.logger != nil {
				h.logger.Warn("periodic reconcile sweep failed", zap.Error(err))
			}
		}
	}
}

func (h *InternalReplicationHandler) startReconcile(ctx context.Context, targetNodeID string) (*internalReconcileStartResponse, error) {
	h.reconcileExecutionMu.Lock()
	defer h.reconcileExecutionMu.Unlock()

	peer, err := h.resolveTargetPeerForNode(ctx, targetNodeID, false)
	if err != nil {
		return nil, fmt.Errorf("resolve target peer: %w", err)
	}
	if peer == nil || strings.TrimSpace(peer.NodeID) == "" {
		if h.assignments != nil {
			return nil, fmt.Errorf("no effective replication assignment is available yet: %w", service.ErrReplicationAssignmentUnavailable)
		}
		return nil, fmt.Errorf("targetNodeId is required")
	}
	if peer.AssignmentGeneration == nil || *peer.AssignmentGeneration <= 0 {
		return nil, fmt.Errorf("resolved target peer %q has no assignment generation", peer.NodeID)
	}
	targetNodeID = peer.NodeID
	assignment, err := h.loadReconcileAssignment(ctx, targetNodeID, *peer.AssignmentGeneration)
	if err != nil {
		return nil, fmt.Errorf("load reconcile assignment: %w", err)
	}

	watermarkOutboxID := int64(0)
	if h.outbox != nil {
		summary, err := h.outbox.GetStatusSummary(ctx, h.config.Node.ID, targetNodeID)
		if err != nil {
			return nil, fmt.Errorf("load outbox status: %w", err)
		}
		if summary != nil && summary.LastOutboxID != nil {
			watermarkOutboxID = *summary.LastOutboxID
		}
	}

	job := &replication.ReconcileJob{
		SourceNodeID:         h.config.Node.ID,
		TargetNodeID:         targetNodeID,
		AssignmentGeneration: peer.AssignmentGeneration,
		WatermarkOutboxID:    watermarkOutboxID,
		Status:               replication.ReconcileJobStatusRunning,
		StartedAt:            time.Now(),
	}
	if err := h.reconcileStore.CreateJob(ctx, job); err != nil {
		return nil, fmt.Errorf("create reconcile job: %w", err)
	}
	if assignment != nil {
		if err := h.updateReconcileAssignmentState(ctx, assignment, cluster.AssignmentStateReconciling, int64Pointer(job.ID), nil); err != nil {
			lastErr := err.Error()
			_ = h.reconcileStore.UpdateJobResult(ctx, job.ID, replication.ReconcileJobStatusFailed, 0, 0, nil, &lastErr)
			return nil, fmt.Errorf("mark assignment reconciling: %w", err)
		}
	}

	items, err := h.reconcileScanner.Scan(ctx)
	if err != nil {
		lastErr := err.Error()
		_ = h.reconcileStore.UpdateJobResult(ctx, job.ID, replication.ReconcileJobStatusFailed, 0, 0, nil, &lastErr)
		h.recordReconcileAssignmentFailure(ctx, assignment, job.ID, err)
		return nil, fmt.Errorf("scan local data for reconcile: %w", err)
	}

	if err := h.reconcileStore.ReplaceItems(ctx, job.ID, items); err != nil {
		lastErr := err.Error()
		_ = h.reconcileStore.UpdateJobResult(ctx, job.ID, replication.ReconcileJobStatusFailed, int64(len(items)), int64(len(items)), nil, &lastErr)
		h.recordReconcileAssignmentFailure(ctx, assignment, job.ID, err)
		return nil, fmt.Errorf("persist reconcile items: %w", err)
	}

	scannedItems := int64(len(items))
	pendingItems := scannedItems
	dispatchPeer, err := h.resolveTargetPeerForNode(ctx, targetNodeID, true)
	if err != nil {
		return nil, fmt.Errorf("resolve dispatch peer: %w", err)
	}
	if pendingItems > 0 && dispatchPeer != nil && strings.TrimSpace(dispatchPeer.BaseURL) != "" {
		remaining, dispatchErr := h.dispatchReconcilePendingItems(ctx, job.ID, dispatchPeer, targetNodeID)
		if dispatchErr != nil {
			lastErr := dispatchErr.Error()
			_ = h.reconcileStore.UpdateJobResult(
				ctx,
				job.ID,
				replication.ReconcileJobStatusFailed,
				scannedItems,
				remaining,
				nil,
				&lastErr,
			)
			h.recordReconcileAssignmentFailure(ctx, assignment, job.ID, dispatchErr)
			return nil, fmt.Errorf("dispatch reconcile items: %w", dispatchErr)
		}
		pendingItems = remaining
	}
	if pendingItems == 0 && dispatchPeer != nil && strings.TrimSpace(dispatchPeer.BaseURL) != "" {
		bootstrapGeneration := peer.AssignmentGeneration
		if dispatchPeer.AssignmentGeneration != nil && *dispatchPeer.AssignmentGeneration > 0 {
			bootstrapGeneration = dispatchPeer.AssignmentGeneration
		}
		if bootstrapGeneration == nil || *bootstrapGeneration <= 0 {
			err := fmt.Errorf("resolved dispatch peer %q has no assignment generation for bootstrap mark", dispatchPeer.NodeID)
			lastErr := err.Error()
			_ = h.reconcileStore.UpdateJobResult(
				ctx,
				job.ID,
				replication.ReconcileJobStatusFailed,
				scannedItems,
				pendingItems,
				nil,
				&lastErr,
			)
			h.recordReconcileAssignmentFailure(ctx, assignment, job.ID, err)
			return nil, fmt.Errorf("bootstrap mark after reconcile: %w", err)
		}
		client := &http.Client{Timeout: h.config.Replication.RequestTimeout}
		if err := h.sendBootstrapMark(ctx, client, dispatchPeer.BaseURL, *bootstrapGeneration, watermarkOutboxID); err != nil {
			lastErr := err.Error()
			_ = h.reconcileStore.UpdateJobResult(
				ctx,
				job.ID,
				replication.ReconcileJobStatusFailed,
				scannedItems,
				pendingItems,
				nil,
				&lastErr,
			)
			h.recordReconcileAssignmentFailure(ctx, assignment, job.ID, err)
			return nil, fmt.Errorf("bootstrap mark after reconcile: %w", err)
		}
	}

	completedAt := time.Now()
	if err := h.reconcileStore.UpdateJobResult(
		ctx,
		job.ID,
		replication.ReconcileJobStatusReady,
		scannedItems,
		pendingItems,
		&completedAt,
		nil,
	); err != nil {
		h.recordReconcileAssignmentFailure(ctx, assignment, job.ID, err)
		return nil, fmt.Errorf("finalize reconcile job: %w", err)
	}
	if assignment != nil {
		if err := h.updateReconcileAssignmentState(ctx, assignment, cluster.AssignmentStateReplicating, int64Pointer(job.ID), nil); err != nil {
			return nil, fmt.Errorf("mark assignment replicating: %w", err)
		}
	}

	return &internalReconcileStartResponse{
		Success:           true,
		JobID:             job.ID,
		SourceNodeID:      job.SourceNodeID,
		TargetNodeID:      job.TargetNodeID,
		WatermarkOutboxID: watermarkOutboxID,
		ScannedItems:      scannedItems,
		PendingItems:      pendingItems,
		Status:            replication.ReconcileJobStatusReady,
	}, nil
}

func (h *InternalReplicationHandler) loadReconcileAssignment(ctx context.Context, targetNodeID string, expectedGeneration int64) (*cluster.ReplicationAssignment, error) {
	if h == nil || h.assignments == nil {
		return nil, nil
	}

	assignment, err := h.assignments.GetByPair(ctx, strings.TrimSpace(h.config.Node.ID), strings.TrimSpace(targetNodeID))
	if err != nil {
		return nil, err
	}
	if assignment == nil {
		return nil, cluster.ErrReplicationAssignmentNotFound
	}
	if assignment.State == cluster.AssignmentStatePaused {
		return nil, fmt.Errorf("replication assignment for target %q is paused", targetNodeID)
	}
	if expectedGeneration > 0 && assignment.Generation != expectedGeneration {
		return nil, cluster.ErrReplicationAssignmentGenerationMismatch
	}
	return assignment, nil
}

func (h *InternalReplicationHandler) updateReconcileAssignmentState(ctx context.Context, assignment *cluster.ReplicationAssignment, state string, jobID *int64, lastError *string) error {
	if h == nil || h.assignments == nil || assignment == nil {
		return nil
	}

	updated := *assignment
	updated.State = state
	updated.LastReconcileJobID = jobID
	updated.LastError = lastError
	if state == cluster.AssignmentStateReplicating {
		updated.FailureCount = 0
		updated.NextRetryAt = nil
	}
	if err := h.assignments.UpdateState(ctx, &updated); err != nil {
		return err
	}
	*assignment = updated
	return nil
}

func (h *InternalReplicationHandler) recordReconcileAssignmentFailure(ctx context.Context, assignment *cluster.ReplicationAssignment, jobID int64, reconcileErr error) {
	if h == nil || assignment == nil || reconcileErr == nil {
		return
	}

	updated := *assignment
	updated.LastReconcileJobID = int64Pointer(jobID)
	updated.FailureCount = assignment.FailureCount + 1
	state := cluster.AssignmentStateError
	lastErr := reconcileErr.Error()
	if threshold := h.reconcileAutoPauseFailures(); threshold > 0 && updated.FailureCount >= threshold {
		state = cluster.AssignmentStatePaused
		lastErr = fmt.Sprintf("assignment auto-paused after %d consecutive reconcile failures: %s", updated.FailureCount, lastErr)
		updated.NextRetryAt = nil
	} else {
		nextRetryAt := h.nextReconcileRetryAt(updated.FailureCount)
		updated.NextRetryAt = &nextRetryAt
	}
	updated.State = state
	updated.LastError = &lastErr
	if err := h.assignments.UpdateState(ctx, &updated); err != nil && h.logger != nil {
		h.logger.Warn("failed to persist reconcile assignment error",
			zap.String("source_node_id", assignment.ActiveNodeID),
			zap.String("target_node_id", assignment.StandbyNodeID),
			zap.Int64("generation", assignment.Generation),
			zap.Error(err))
		return
	}
	*assignment = updated
}

func (h *InternalReplicationHandler) reconcileAutoPauseFailures() int {
	if h == nil {
		return 0
	}
	if h.config.Replication.ReconcileAutoPauseFailures < 0 {
		return 0
	}
	return h.config.Replication.ReconcileAutoPauseFailures
}

func (h *InternalReplicationHandler) nextReconcileRetryAt(failureCount int) time.Time {
	now := time.Now().UTC()
	if h == nil {
		return now
	}
	return now.Add(h.reconcileRetryDelay(failureCount))
}

func (h *InternalReplicationHandler) reconcileRetryDelay(failureCount int) time.Duration {
	base := h.config.Replication.RetryBackoffBase
	maxDelay := h.config.Replication.MaxRetryBackoff
	if base <= 0 {
		base = 2 * time.Second
	}
	if maxDelay < base {
		maxDelay = 5 * time.Minute
	}
	if failureCount <= 1 {
		return base
	}

	delay := base
	for i := 1; i < failureCount; i++ {
		if delay >= maxDelay/2 {
			return maxDelay
		}
		delay *= 2
	}
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}

func (h *InternalReplicationHandler) replicationPair(peer *service.ResolvedReplicationPeer) (string, string) {
	peerNodeID := ""
	if peer != nil {
		peerNodeID = strings.TrimSpace(peer.NodeID)
	}
	if strings.EqualFold(strings.TrimSpace(h.config.Node.Role), "standby") {
		return peerNodeID, h.config.Node.ID
	}
	return h.config.Node.ID, peerNodeID
}

func (h *InternalReplicationHandler) workerEnabled() bool {
	replCfg := h.config.Replication
	return replCfg.Enabled &&
		strings.EqualFold(strings.TrimSpace(h.config.Node.Role), "active")
}

func (h *InternalReplicationHandler) determineState(status internalReplicationStatus) string {
	if !status.Enabled {
		return "disabled"
	}

	role := strings.ToLower(strings.TrimSpace(h.config.Node.Role))
	hasPending := status.PendingEvents != nil && *status.PendingEvents > 0
	hasFailed := status.FailedEvents != nil && *status.FailedEvents > 0
	hasOutboxHistory := status.LastOutboxID != nil && *status.LastOutboxID > 0
	hasApplyHistory := status.LastAppliedOutboxID != nil

	switch role {
	case "active":
		if hasFailed {
			return "retrying"
		}
		if hasPending {
			return "dispatching"
		}
		return "idle"
	case "standby":
		if !hasApplyHistory && hasOutboxHistory {
			return "bootstrap_required"
		}
		if hasFailed {
			return "retrying"
		}
		if hasPending {
			return "catching_up"
		}
		if hasApplyHistory || hasOutboxHistory {
			return "caught_up"
		}
		return "idle"
	default:
		if hasFailed {
			return "retrying"
		}
		if hasPending {
			return "active"
		}
		return "idle"
	}
}

func (h *InternalReplicationHandler) buildNotes(status internalReplicationStatus) []string {
	if !status.Enabled {
		return []string{"internal replication is disabled"}
	}

	role := strings.ToLower(strings.TrimSpace(h.config.Node.Role))
	notes := make([]string, 0, 3)
	switch role {
	case "active":
		notes = append(notes, "only active nodes dispatch internal replication events")
		if status.ResolvedPeerNodeID == "" {
			notes = append(notes, h.missingResolvedPeerNote("standby"))
		} else if status.ResolvedGeneration == nil {
			notes = append(notes, "resolved standby peer has no assignment generation; replication dispatch is paused")
		} else if status.ResolvedPeerBaseURL == "" {
			notes = append(notes, "resolved standby peer has no usable base url yet")
		} else if status.ResolvedPeerHealthy != nil && !*status.ResolvedPeerHealthy {
			notes = append(notes, "resolved standby peer heartbeat is stale, dispatch is paused until it recovers")
		}
	case "standby":
		notes = append(notes, "standby nodes only accept internal replication apply traffic")
		if status.ResolvedPeerNodeID == "" {
			notes = append(notes, h.missingResolvedPeerNote("active"))
		}
		if status.LastAppliedOutboxID == nil && status.LastOutboxID != nil && *status.LastOutboxID > 0 {
			notes = append(notes, "standby offset is not initialized; finish baseline copy before promotion")
		}
	}
	if status.FailedEvents != nil && *status.FailedEvents > 0 {
		notes = append(notes, "pendingEvents includes events currently waiting for retry")
	}

	return notes
}

func (h *InternalReplicationHandler) missingResolvedPeerNote(peerRole string) string {
	peerRole = strings.TrimSpace(peerRole)
	if h != nil && h.assignments != nil {
		return fmt.Sprintf("no %s peer resolved from an effective assignment", peerRole)
	}
	return fmt.Sprintf("no %s peer resolved from cluster registry", peerRole)
}

func (h *InternalReplicationHandler) resolveTargetPeer(ctx context.Context) (*service.ResolvedReplicationPeer, error) {
	return h.resolveTargetPeerForNode(ctx, "", false)
}

func (h *InternalReplicationHandler) runPeriodicReconcileOnce(ctx context.Context) error {
	peers, err := h.resolveTargetPeers(ctx, true)
	if err != nil {
		return err
	}
	if len(peers) == 0 {
		return nil
	}

	var sweepErr error
	for _, peer := range peers {
		shouldRun, reason, err := h.shouldRunAutomaticReconcileForPeer(ctx, peer)
		if err != nil {
			sweepErr = errors.Join(sweepErr, fmt.Errorf("inspect auto reconcile target %q: %w", peerNodeID(peer), err))
			continue
		}
		if !shouldRun {
			continue
		}

		resp, err := h.startReconcile(ctx, peer.NodeID)
		if err != nil {
			sweepErr = errors.Join(sweepErr, fmt.Errorf("auto reconcile target %q: %w", peer.NodeID, err))
			if h.logger != nil {
				h.logger.Warn("automatic reconcile attempt failed",
					zap.String("target_node_id", peer.NodeID),
					zap.String("reason", reason),
					zap.Error(err))
			}
			continue
		}
		if h.logger != nil {
			h.logger.Info("automatic reconcile finished",
				zap.String("target_node_id", resp.TargetNodeID),
				zap.String("reason", reason),
				zap.Int64("job_id", resp.JobID),
				zap.Int64("scanned_items", resp.ScannedItems),
				zap.Int64("pending_items", resp.PendingItems))
		}
	}
	return sweepErr
}

func (h *InternalReplicationHandler) shouldRunAutomaticReconcileForPeer(ctx context.Context, peer *service.ResolvedReplicationPeer) (bool, string, error) {
	if peer == nil || strings.TrimSpace(peer.NodeID) == "" {
		return false, "", nil
	}
	if peer.AssignmentGeneration == nil || *peer.AssignmentGeneration <= 0 {
		return false, "", fmt.Errorf("target %q has no assignment generation", peer.NodeID)
	}

	assignment, err := h.loadReconcileAssignment(ctx, peer.NodeID, *peer.AssignmentGeneration)
	if err != nil {
		return false, "", err
	}
	if assignment == nil {
		return false, "", nil
	}

	switch assignment.State {
	case cluster.AssignmentStatePending:
		return true, "assignment_pending", nil
	case cluster.AssignmentStateReconciling:
		return true, "assignment_reconciling", nil
	case cluster.AssignmentStatePaused:
		return false, "", nil
	case cluster.AssignmentStateReplicating:
		if h.offsets == nil {
			return false, "", nil
		}
		offset, err := h.offsets.Get(ctx, strings.TrimSpace(h.config.Node.ID), strings.TrimSpace(peer.NodeID))
		if errors.Is(err, replication.ErrOffsetNotFound) {
			return true, "missing_offset", nil
		}
		if err != nil {
			return false, "", err
		}
		if offset == nil || offset.AssignmentGeneration == nil {
			return true, "missing_generation_baseline", nil
		}
		if *offset.AssignmentGeneration != assignment.Generation {
			return true, "offset_generation_mismatch", nil
		}
	}
	return false, "", nil
}

func (h *InternalReplicationHandler) resolveTargetPeers(ctx context.Context, requireHealthy bool) ([]*service.ResolvedReplicationPeer, error) {
	if h.peerResolver == nil {
		return nil, nil
	}
	if requireHealthy {
		return h.peerResolver.ResolveDispatchTargets(ctx)
	}
	return h.peerResolver.ResolveTargets(ctx)
}

func (h *InternalReplicationHandler) resolveTargetPeerForNode(ctx context.Context, nodeID string, requireHealthy bool) (*service.ResolvedReplicationPeer, error) {
	if h.peerResolver == nil {
		nodeID = strings.TrimSpace(nodeID)
		if nodeID == "" {
			return nil, nil
		}
		if requireHealthy {
			return nil, nil
		}
		return &service.ResolvedReplicationPeer{
			NodeID: nodeID,
			Source: "explicit",
		}, nil
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		peers, err := h.resolveTargetPeers(ctx, requireHealthy)
		if err != nil || len(peers) == 0 {
			return nil, err
		}
		return peers[0], nil
	}

	peers, err := h.resolveTargetPeers(ctx, requireHealthy)
	if err != nil {
		return nil, err
	}
	for _, peer := range peers {
		if peer != nil && strings.EqualFold(strings.TrimSpace(peer.NodeID), nodeID) {
			return peer, nil
		}
	}
	return h.peerResolver.ResolveByNodeID(ctx, nodeID, requireHealthy)
}

func (h *InternalReplicationHandler) waitStartupReconcileRetry(ctx context.Context, retryInterval time.Duration) bool {
	timer := time.NewTimer(retryInterval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func boolPointer(value bool) *bool {
	v := value
	return &v
}

func peerNodeID(peer *service.ResolvedReplicationPeer) string {
	if peer == nil {
		return ""
	}
	return strings.TrimSpace(peer.NodeID)
}

type standbyAssignmentAuthorizationError struct {
	StatusCode int
	Message    string
}

func (e *standbyAssignmentAuthorizationError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (h *InternalReplicationHandler) requireAssignedSource(ctx context.Context, sourceNodeID string) (*cluster.ReplicationAssignment, error) {
	if h == nil || h.assignments == nil || !strings.EqualFold(strings.TrimSpace(h.config.Node.Role), "standby") {
		return nil, nil
	}

	sourceNodeID = strings.TrimSpace(sourceNodeID)
	if sourceNodeID == "" {
		return nil, &standbyAssignmentAuthorizationError{
			StatusCode: http.StatusBadRequest,
			Message:    "missing " + middleware.InternalNodeIDHeader,
		}
	}

	assignment, err := h.assignments.GetEffectiveByStandby(ctx, strings.TrimSpace(h.config.Node.ID))
	if err != nil {
		return nil, fmt.Errorf("load standby assignment: %w", err)
	}
	if assignment == nil {
		return nil, &standbyAssignmentAuthorizationError{
			StatusCode: http.StatusConflict,
			Message:    "standby has no effective assignment",
		}
	}
	if assignment.LeaseExpired(time.Now().UTC()) {
		return nil, &standbyAssignmentAuthorizationError{
			StatusCode: http.StatusConflict,
			Message:    "standby assignment lease is expired",
		}
	}

	expectedSourceNodeID := strings.TrimSpace(assignment.ActiveNodeID)
	if expectedSourceNodeID == "" {
		return nil, &standbyAssignmentAuthorizationError{
			StatusCode: http.StatusConflict,
			Message:    "standby assignment has no active node id",
		}
	}
	if sourceNodeID != expectedSourceNodeID {
		return nil, &standbyAssignmentAuthorizationError{
			StatusCode: http.StatusForbidden,
			Message:    fmt.Sprintf("source node %q is not the assigned active node", sourceNodeID),
		}
	}
	return assignment, nil
}

func (h *InternalReplicationHandler) writeStandbyAuthorizationError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	var authErr *standbyAssignmentAuthorizationError
	if errors.As(err, &authErr) {
		h.writeError(w, authErr.StatusCode, authErr.Message)
		return
	}
	h.logger.Error("failed to authorize standby internal replication request", zap.Error(err))
	h.writeError(w, http.StatusInternalServerError, "Failed to authorize standby assignment")
}

func (h *InternalReplicationHandler) writeJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
	}
}

func (h *InternalReplicationHandler) writeError(w http.ResponseWriter, code int, message string) {
	h.writeJSON(w, code, map[string]interface{}{
		"error":   message,
		"code":    code,
		"success": false,
	})
}
