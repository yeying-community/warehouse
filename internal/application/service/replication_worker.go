package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/replication"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

type replicationFSApplyRequest struct {
	OutboxID int64  `json:"outboxId"`
	Op       string `json:"op"`
	Path     string `json:"path,omitempty"`
	FromPath string `json:"fromPath,omitempty"`
	ToPath   string `json:"toPath,omitempty"`
	IsDir    bool   `json:"isDir"`
}

// ReplicationWorker dispatches outbox events from active to standby.
type ReplicationWorker struct {
	config       *config.Config
	outbox       repository.ReplicationOutboxRepository
	peerResolver ReplicationPeerResolver
	client       *http.Client
	logger       *zap.Logger
	now          func() time.Time
}

// NewReplicationWorker creates the active-side outbox dispatcher.
func NewReplicationWorker(cfg *config.Config, outbox repository.ReplicationOutboxRepository, peerResolver ReplicationPeerResolver, logger *zap.Logger) *ReplicationWorker {
	if cfg == nil || outbox == nil {
		return nil
	}
	return &ReplicationWorker{
		config:       cfg,
		outbox:       outbox,
		peerResolver: peerResolver,
		client:       &http.Client{Timeout: cfg.Internal.Replication.RequestTimeout},
		logger:       logger,
		now:          time.Now,
	}
}

// Enabled reports whether the worker should run for the current node.
func (w *ReplicationWorker) Enabled() bool {
	if w == nil || w.config == nil {
		return false
	}
	replCfg := w.config.Internal.Replication
	return replCfg.Enabled && strings.ToLower(strings.TrimSpace(w.config.Node.Role)) == "active"
}

// Run starts the periodic dispatch loop until ctx is canceled.
func (w *ReplicationWorker) Run(ctx context.Context) {
	if !w.Enabled() {
		return
	}

	interval := w.config.Internal.Replication.DispatchInterval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	w.logger.Info("replication worker started",
		zap.String("source_node_id", w.config.Node.ID),
		zap.String("configured_target_node_id", w.config.Internal.Replication.PeerNodeID),
		zap.Duration("dispatch_interval", interval),
		zap.Int("batch_size", w.config.Internal.Replication.BatchSize),
	)
	defer w.logger.Info("replication worker stopped",
		zap.String("source_node_id", w.config.Node.ID),
		zap.String("configured_target_node_id", w.config.Internal.Replication.PeerNodeID),
	)

	if err := w.DispatchOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		w.logger.Error("replication worker dispatch failed", zap.Error(err))
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.DispatchOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				w.logger.Error("replication worker dispatch failed", zap.Error(err))
			}
		}
	}
}

// DispatchOnce processes a single ordered outbox batch.
func (w *ReplicationWorker) DispatchOnce(ctx context.Context) error {
	if !w.Enabled() {
		return nil
	}

	peer, err := w.resolveDispatchPeer(ctx)
	if err != nil {
		return err
	}
	if peer == nil {
		return nil
	}
	if peer.AssignmentGeneration == nil || *peer.AssignmentGeneration <= 0 {
		return fmt.Errorf("resolved replication peer %q has no assignment generation", peer.NodeID)
	}

	sourceNodeID := w.config.Node.ID
	targetNodeID := peer.NodeID
	events, err := w.outbox.ListPending(ctx, sourceNodeID, targetNodeID, peer.AssignmentGeneration, w.config.Internal.Replication.BatchSize)
	if err != nil {
		return fmt.Errorf("list pending replication events: %w", err)
	}
	if len(events) == 0 {
		return nil
	}

	for _, event := range events {
		if err := w.dispatchEvent(ctx, peer, event); err != nil {
			nextRetryAt := w.now().Add(w.retryDelay(event.AttemptCount))
			markErr := w.outbox.MarkFailed(ctx, event.ID, err.Error(), nextRetryAt)
			if markErr != nil {
				return fmt.Errorf("dispatch event %d failed: %v; mark failed: %w", event.ID, err, markErr)
			}
			w.logger.Warn("replication event dispatch failed",
				zap.Int64("outbox_id", event.ID),
				zap.String("op", event.Op),
				zap.Time("next_retry_at", nextRetryAt),
				zap.Error(err),
			)
			break
		}
		if err := w.outbox.MarkDispatched(ctx, event.ID, w.now()); err != nil {
			return fmt.Errorf("mark event %d dispatched: %w", event.ID, err)
		}
		w.logger.Debug("replication event dispatched",
			zap.Int64("outbox_id", event.ID),
			zap.String("op", event.Op),
		)
	}

	return nil
}

func (w *ReplicationWorker) dispatchEvent(ctx context.Context, peer *ResolvedReplicationPeer, event *replication.OutboxEvent) error {
	switch event.Op {
	case replication.OpEnsureDir, replication.OpMovePath, replication.OpCopyPath, replication.OpRemovePath:
		return w.dispatchFSApply(ctx, peer, event)
	case replication.OpUpsertFile:
		return w.dispatchFileApply(ctx, peer, event)
	default:
		return fmt.Errorf("unsupported replication event op %q", event.Op)
	}
}

func (w *ReplicationWorker) dispatchFSApply(ctx context.Context, peer *ResolvedReplicationPeer, event *replication.OutboxEvent) error {
	requestBody := replicationFSApplyRequest{
		OutboxID: event.ID,
		Op:       event.Op,
		IsDir:    event.IsDir,
	}
	if event.Path != nil {
		requestBody.Path = *event.Path
	}
	if event.FromPath != nil {
		requestBody.FromPath = *event.FromPath
	}
	if event.ToPath != nil {
		requestBody.ToPath = *event.ToPath
	}
	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("marshal fs apply request: %w", err)
	}

	requestURL := strings.TrimRight(peer.BaseURL, "/") + "/api/v1/internal/replication/fs/apply"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create fs apply request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if err := w.signRequest(req, payloadSHA256(bodyBytes)); err != nil {
		return err
	}
	assignmentGeneration, err := w.requiredAssignmentGeneration(peer, event)
	if err != nil {
		return err
	}
	req.Header.Set(middleware.InternalAssignmentGenerationHeader, fmt.Sprintf("%d", assignmentGeneration))

	return w.doRequest(req)
}

func (w *ReplicationWorker) dispatchFileApply(ctx context.Context, peer *ResolvedReplicationPeer, event *replication.OutboxEvent) error {
	if event.Path == nil || strings.TrimSpace(*event.Path) == "" {
		return fmt.Errorf("upsert_file event %d is missing path", event.ID)
	}
	if event.ContentSHA256 == nil || strings.TrimSpace(*event.ContentSHA256) == "" {
		return fmt.Errorf("upsert_file event %d is missing content sha256", event.ID)
	}
	if event.FileSize == nil || *event.FileSize < 0 {
		return fmt.Errorf("upsert_file event %d is missing file size", event.ID)
	}

	fullPath, err := w.resolveLocalPath(*event.Path)
	if err != nil {
		return err
	}
	file, err := os.Open(fullPath)
	if err != nil {
		return fmt.Errorf("open replication source file %q: %w", fullPath, err)
	}
	defer file.Close()

	values := url.Values{}
	values.Set("outboxId", fmt.Sprintf("%d", event.ID))
	values.Set("path", *event.Path)
	values.Set("fileSize", fmt.Sprintf("%d", *event.FileSize))
	requestURL := strings.TrimRight(peer.BaseURL, "/") + "/api/v1/internal/replication/file?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, requestURL, file)
	if err != nil {
		return fmt.Errorf("create file apply request: %w", err)
	}
	req.ContentLength = *event.FileSize
	if err := w.signRequest(req, *event.ContentSHA256); err != nil {
		return err
	}
	assignmentGeneration, err := w.requiredAssignmentGeneration(peer, event)
	if err != nil {
		return err
	}
	req.Header.Set(middleware.InternalAssignmentGenerationHeader, fmt.Sprintf("%d", assignmentGeneration))

	return w.doRequest(req)
}

func (w *ReplicationWorker) requiredAssignmentGeneration(peer *ResolvedReplicationPeer, event *replication.OutboxEvent) (int64, error) {
	if peer == nil || peer.AssignmentGeneration == nil || *peer.AssignmentGeneration <= 0 {
		return 0, fmt.Errorf("resolved replication peer %q has no assignment generation", peerNodeID(peer))
	}
	if event == nil || event.AssignmentGeneration == nil || *event.AssignmentGeneration <= 0 {
		return 0, fmt.Errorf("replication event %d has no assignment generation", eventID(event))
	}
	if *event.AssignmentGeneration != *peer.AssignmentGeneration {
		return 0, fmt.Errorf(
			"replication event %d assignment generation %d does not match resolved peer generation %d",
			event.ID,
			*event.AssignmentGeneration,
			*peer.AssignmentGeneration,
		)
	}
	return *event.AssignmentGeneration, nil
}

func (w *ReplicationWorker) doRequest(req *http.Request) error {
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return fmt.Errorf("peer returned %s: %s", resp.Status, message)
	}
	return nil
}

func (w *ReplicationWorker) signRequest(req *http.Request, payloadHash string) error {
	timestamp := w.now().UTC().Format(time.RFC3339)
	req.Header.Set(middleware.InternalNodeIDHeader, w.config.Node.ID)
	req.Header.Set(middleware.InternalTimestampHeader, timestamp)
	req.Header.Set(middleware.InternalContentSHA256Header, payloadHash)
	req.Header.Set(middleware.InternalSignatureHeader, middleware.SignInternalRequest(
		req.Method,
		req.URL.Path,
		w.config.Node.ID,
		timestamp,
		payloadHash,
		w.config.Internal.Replication.SharedSecret,
	))
	return nil
}

func (w *ReplicationWorker) resolveDispatchPeer(ctx context.Context) (*ResolvedReplicationPeer, error) {
	if w.peerResolver == nil {
		nodeID := strings.TrimSpace(w.config.Internal.Replication.PeerNodeID)
		baseURL := strings.TrimSpace(w.config.Internal.Replication.PeerBaseURL)
		if nodeID == "" || baseURL == "" {
			return nil, nil
		}
		return &ResolvedReplicationPeer{
			NodeID:  nodeID,
			BaseURL: baseURL,
			Source:  "config",
			Healthy: true,
		}, nil
	}
	return w.peerResolver.ResolveDispatchTarget(ctx)
}

func (w *ReplicationWorker) resolveLocalPath(storagePath string) (string, error) {
	cleaned := path.Clean("/" + strings.TrimSpace(storagePath))
	if cleaned == "/" || strings.HasPrefix(cleaned, "/..") {
		return "", fmt.Errorf("invalid storage path %q", storagePath)
	}
	return filepath.Join(filepath.Clean(w.config.WebDAV.Directory), filepath.FromSlash(strings.TrimPrefix(cleaned, "/"))), nil
}

func (w *ReplicationWorker) retryDelay(attemptCount int) time.Duration {
	base := w.config.Internal.Replication.RetryBackoffBase
	maxDelay := w.config.Internal.Replication.MaxRetryBackoff
	if attemptCount <= 0 {
		return base
	}
	delay := base
	for i := 0; i < attemptCount; i++ {
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

func payloadSHA256(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func peerNodeID(peer *ResolvedReplicationPeer) string {
	if peer == nil {
		return ""
	}
	return strings.TrimSpace(peer.NodeID)
}

func eventID(event *replication.OutboxEvent) int64 {
	if event == nil {
		return 0
	}
	return event.ID
}
