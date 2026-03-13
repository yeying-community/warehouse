package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// InternalReplicationHandler exposes replication-related internal control endpoints.
type InternalReplicationHandler struct {
	logger           *zap.Logger
	config           *config.Config
	outbox           replicationOutboxStatusReader
	offsets          replicationOffsetStore
	reconcileStore   replicationReconcileStore
	reconcileScanner replicationReconcileScanner
	peerResolver     service.ReplicationPeerResolver
	assignments      repository.ClusterReplicationAssignmentRepository
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
		logger:           logger,
		config:           cfg,
		outbox:           outbox,
		offsets:          offsets,
		reconcileStore:   reconcileStore,
		reconcileScanner: reconcileScanner,
		peerResolver:     peerResolver,
		assignments:      assignmentRepo,
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
	PeerNodeID             string     `json:"peerNodeId,omitempty"`
	PeerBaseURL            string     `json:"peerBaseUrl,omitempty"`
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

	replicationCfg := h.config.Internal.Replication
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
			PeerNodeID:    replicationCfg.PeerNodeID,
			PeerBaseURL:   replicationCfg.PeerBaseURL,
		},
	}
	if !replicationCfg.Enabled {
		response.Replication.Notes = []string{"internal replication is disabled"}
		h.writeJSON(w, http.StatusOK, response)
		return
	}

	resolvedPeer, err := h.resolveTargetPeer(r.Context())
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
	if targetNodeID == "" {
		targetNodeID = strings.TrimSpace(h.config.Internal.Replication.PeerNodeID)
	}
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

	const maxAttempts = 24
	const retryInterval = 5 * time.Second
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		resp, err := h.startReconcile(ctx, "")
		if err == nil {
			h.logger.Info("startup reconcile finished",
				zap.Int64("job_id", resp.JobID),
				zap.String("target_node_id", resp.TargetNodeID),
				zap.Int64("scanned_items", resp.ScannedItems),
				zap.Int64("pending_items", resp.PendingItems))
			return
		}

		h.logger.Warn("startup reconcile attempt failed, will retry",
			zap.Int("attempt", attempt),
			zap.Int("max_attempts", maxAttempts),
			zap.Error(err))

		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}

	h.logger.Warn("startup reconcile stopped after max retry attempts",
		zap.Int("max_attempts", maxAttempts))
}

func (h *InternalReplicationHandler) startReconcile(ctx context.Context, targetNodeID string) (*internalReconcileStartResponse, error) {
	peer, err := h.resolveTargetPeerForNode(ctx, targetNodeID, false)
	if err != nil {
		return nil, fmt.Errorf("resolve target peer: %w", err)
	}
	if peer == nil || strings.TrimSpace(peer.NodeID) == "" {
		return nil, fmt.Errorf("targetNodeId is required")
	}
	if peer.AssignmentGeneration == nil || *peer.AssignmentGeneration <= 0 {
		return nil, fmt.Errorf("resolved target peer %q has no assignment generation", peer.NodeID)
	}
	targetNodeID = peer.NodeID

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

	items, err := h.reconcileScanner.Scan(ctx)
	if err != nil {
		lastErr := err.Error()
		_ = h.reconcileStore.UpdateJobResult(ctx, job.ID, replication.ReconcileJobStatusFailed, 0, 0, nil, &lastErr)
		return nil, fmt.Errorf("scan local data for reconcile: %w", err)
	}

	if err := h.reconcileStore.ReplaceItems(ctx, job.ID, items); err != nil {
		lastErr := err.Error()
		_ = h.reconcileStore.UpdateJobResult(ctx, job.ID, replication.ReconcileJobStatusFailed, int64(len(items)), int64(len(items)), nil, &lastErr)
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
			return nil, fmt.Errorf("dispatch reconcile items: %w", dispatchErr)
		}
		pendingItems = remaining
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
		return nil, fmt.Errorf("finalize reconcile job: %w", err)
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

func (h *InternalReplicationHandler) replicationPair(peer *service.ResolvedReplicationPeer) (string, string) {
	peerNodeID := strings.TrimSpace(h.config.Internal.Replication.PeerNodeID)
	if peer != nil && strings.TrimSpace(peer.NodeID) != "" {
		peerNodeID = strings.TrimSpace(peer.NodeID)
	}
	if strings.EqualFold(strings.TrimSpace(h.config.Node.Role), "standby") {
		return peerNodeID, h.config.Node.ID
	}
	return h.config.Node.ID, peerNodeID
}

func (h *InternalReplicationHandler) workerEnabled() bool {
	replCfg := h.config.Internal.Replication
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
			notes = append(notes, "no standby peer resolved from assignment, config, or cluster registry")
		} else if status.ResolvedGeneration == nil {
			notes = append(notes, "resolved standby peer has no assignment generation; replication dispatch is paused")
		} else if status.ResolvedPeerBaseURL == "" {
			notes = append(notes, "resolved standby peer has no usable base url yet")
		} else if status.ResolvedPeerHealthy != nil && !*status.ResolvedPeerHealthy && strings.TrimSpace(status.PeerBaseURL) == "" {
			notes = append(notes, "resolved standby peer heartbeat is stale, dispatch is paused until it recovers")
		}
	case "standby":
		notes = append(notes, "standby nodes only accept internal replication apply traffic")
		if status.ResolvedPeerNodeID == "" {
			notes = append(notes, "active peer is not currently resolved from assignment, config, or cluster registry")
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

func (h *InternalReplicationHandler) resolveTargetPeer(ctx context.Context) (*service.ResolvedReplicationPeer, error) {
	if h.peerResolver == nil {
		peerNodeID := strings.TrimSpace(h.config.Internal.Replication.PeerNodeID)
		if peerNodeID == "" {
			return nil, nil
		}
		return &service.ResolvedReplicationPeer{
			NodeID:  peerNodeID,
			BaseURL: strings.TrimSpace(h.config.Internal.Replication.PeerBaseURL),
			Source:  "config",
		}, nil
	}
	return h.peerResolver.ResolveTarget(ctx)
}

func (h *InternalReplicationHandler) resolveTargetPeerForNode(ctx context.Context, nodeID string, requireHealthy bool) (*service.ResolvedReplicationPeer, error) {
	if h.peerResolver == nil {
		nodeID = strings.TrimSpace(nodeID)
		configuredNodeID := strings.TrimSpace(h.config.Internal.Replication.PeerNodeID)
		if nodeID == "" {
			nodeID = configuredNodeID
		}
		if nodeID == "" {
			return nil, nil
		}
		baseURL := strings.TrimSpace(h.config.Internal.Replication.PeerBaseURL)
		if requireHealthy && baseURL == "" {
			return nil, nil
		}
		return &service.ResolvedReplicationPeer{
			NodeID:  nodeID,
			BaseURL: baseURL,
			Source:  "config",
			Healthy: baseURL != "",
		}, nil
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		if requireHealthy {
			return h.peerResolver.ResolveDispatchTarget(ctx)
		}
		return h.peerResolver.ResolveTarget(ctx)
	}
	if requireHealthy {
		peer, err := h.peerResolver.ResolveDispatchTarget(ctx)
		if err != nil {
			return nil, err
		}
		if peer != nil && strings.TrimSpace(peer.NodeID) == nodeID {
			return peer, nil
		}
	} else {
		peer, err := h.peerResolver.ResolveTarget(ctx)
		if err != nil {
			return nil, err
		}
		if peer != nil && strings.TrimSpace(peer.NodeID) == nodeID {
			return peer, nil
		}
	}
	return h.peerResolver.ResolveByNodeID(ctx, nodeID, requireHealthy)
}

func boolPointer(value bool) *bool {
	v := value
	return &v
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
