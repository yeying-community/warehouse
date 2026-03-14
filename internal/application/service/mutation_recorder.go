package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/yeying-community/warehouse/internal/domain/replication"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
	"go.uber.org/zap"
)

// MutationRecorder persists file mutations as durable replication events.
type MutationRecorder interface {
	EnsureDir(ctx context.Context, fullPath string) error
	UpsertFile(ctx context.Context, fullPath string) error
	MovePath(ctx context.Context, fromFullPath, toFullPath string, isDir bool) error
	CopyPath(ctx context.Context, fromFullPath, toFullPath string, isDir bool) error
	RemovePath(ctx context.Context, fullPath string, isDir bool) error
}

type noopMutationRecorder struct{}

func (noopMutationRecorder) EnsureDir(context.Context, string) error              { return nil }
func (noopMutationRecorder) UpsertFile(context.Context, string) error             { return nil }
func (noopMutationRecorder) MovePath(context.Context, string, string, bool) error { return nil }
func (noopMutationRecorder) CopyPath(context.Context, string, string, bool) error { return nil }
func (noopMutationRecorder) RemovePath(context.Context, string, bool) error       { return nil }

// OutboxMutationRecorder records mutations into replication_outbox.
type OutboxMutationRecorder struct {
	outbox       repository.ReplicationOutboxRepository
	peerResolver ReplicationPeerResolver
	logger       *zap.Logger
	webdavRoot   string
	sourceNodeID string
}

// NewMutationRecorder creates an active-node outbox-backed mutation recorder.
func NewMutationRecorder(
	cfg *config.Config,
	outbox repository.ReplicationOutboxRepository,
	peerResolver ReplicationPeerResolver,
	logger *zap.Logger,
) MutationRecorder {
	if cfg == nil || outbox == nil {
		return noopMutationRecorder{}
	}
	if !cfg.Replication.Enabled || strings.ToLower(strings.TrimSpace(cfg.Node.Role)) != "active" {
		return noopMutationRecorder{}
	}

	webdavRoot, err := filepath.Abs(strings.TrimSpace(cfg.WebDAV.Directory))
	if err != nil {
		if logger != nil {
			logger.Warn("failed to resolve webdav root for mutation recorder", zap.Error(err))
		}
		return noopMutationRecorder{}
	}

	return &OutboxMutationRecorder{
		outbox:       outbox,
		peerResolver: peerResolver,
		logger:       logger,
		webdavRoot:   filepath.Clean(webdavRoot),
		sourceNodeID: cfg.Node.ID,
	}
}

func (r *OutboxMutationRecorder) EnsureDir(ctx context.Context, fullPath string) error {
	normalized, err := r.normalizeFullPath(fullPath)
	if err != nil {
		return err
	}
	if normalized == "/" {
		return nil
	}
	targets, err := r.resolveTargets(ctx)
	if err != nil {
		return err
	}
	return r.appendBatch(ctx, newOutboxEventsForTargets(targets, func(target *ResolvedReplicationPeer) *replication.OutboxEvent {
		return &replication.OutboxEvent{
			SourceNodeID:         r.sourceNodeID,
			TargetNodeID:         target.NodeID,
			AssignmentGeneration: target.AssignmentGeneration,
			Op:                   replication.OpEnsureDir,
			Path:                 stringPointer(normalized),
			IsDir:                true,
		}
	}))
}

func (r *OutboxMutationRecorder) UpsertFile(ctx context.Context, fullPath string) error {
	normalized, err := r.normalizeFullPath(fullPath)
	if err != nil {
		return err
	}
	if normalized == "/" {
		return fmt.Errorf("cannot record upsert for root path")
	}

	size, sha256Hex, err := fileDigest(fullPath)
	if err != nil {
		return err
	}
	targets, err := r.resolveTargets(ctx)
	if err != nil {
		return err
	}

	return r.appendBatch(ctx, newOutboxEventsForTargets(targets, func(target *ResolvedReplicationPeer) *replication.OutboxEvent {
		return &replication.OutboxEvent{
			SourceNodeID:         r.sourceNodeID,
			TargetNodeID:         target.NodeID,
			AssignmentGeneration: target.AssignmentGeneration,
			Op:                   replication.OpUpsertFile,
			Path:                 stringPointer(normalized),
			ContentSHA256:        stringPointer(sha256Hex),
			FileSize:             int64Pointer(size),
		}
	}))
}

func (r *OutboxMutationRecorder) MovePath(ctx context.Context, fromFullPath, toFullPath string, isDir bool) error {
	fromPath, err := r.normalizeFullPath(fromFullPath)
	if err != nil {
		return err
	}
	toPath, err := r.normalizeFullPath(toFullPath)
	if err != nil {
		return err
	}
	if fromPath == "/" || toPath == "/" {
		return fmt.Errorf("cannot record move involving root path")
	}
	targets, err := r.resolveTargets(ctx)
	if err != nil {
		return err
	}
	return r.appendBatch(ctx, newOutboxEventsForTargets(targets, func(target *ResolvedReplicationPeer) *replication.OutboxEvent {
		return &replication.OutboxEvent{
			SourceNodeID:         r.sourceNodeID,
			TargetNodeID:         target.NodeID,
			AssignmentGeneration: target.AssignmentGeneration,
			Op:                   replication.OpMovePath,
			FromPath:             stringPointer(fromPath),
			ToPath:               stringPointer(toPath),
			IsDir:                isDir,
		}
	}))
}

func (r *OutboxMutationRecorder) CopyPath(ctx context.Context, fromFullPath, toFullPath string, isDir bool) error {
	fromPath, err := r.normalizeFullPath(fromFullPath)
	if err != nil {
		return err
	}
	toPath, err := r.normalizeFullPath(toFullPath)
	if err != nil {
		return err
	}
	if fromPath == "/" || toPath == "/" {
		return fmt.Errorf("cannot record copy involving root path")
	}
	targets, err := r.resolveTargets(ctx)
	if err != nil {
		return err
	}
	return r.appendBatch(ctx, newOutboxEventsForTargets(targets, func(target *ResolvedReplicationPeer) *replication.OutboxEvent {
		return &replication.OutboxEvent{
			SourceNodeID:         r.sourceNodeID,
			TargetNodeID:         target.NodeID,
			AssignmentGeneration: target.AssignmentGeneration,
			Op:                   replication.OpCopyPath,
			FromPath:             stringPointer(fromPath),
			ToPath:               stringPointer(toPath),
			IsDir:                isDir,
		}
	}))
}

func (r *OutboxMutationRecorder) RemovePath(ctx context.Context, fullPath string, isDir bool) error {
	normalized, err := r.normalizeFullPath(fullPath)
	if err != nil {
		return err
	}
	if normalized == "/" {
		return fmt.Errorf("cannot record removal of root path")
	}
	targets, err := r.resolveTargets(ctx)
	if err != nil {
		return err
	}
	return r.appendBatch(ctx, newOutboxEventsForTargets(targets, func(target *ResolvedReplicationPeer) *replication.OutboxEvent {
		return &replication.OutboxEvent{
			SourceNodeID:         r.sourceNodeID,
			TargetNodeID:         target.NodeID,
			AssignmentGeneration: target.AssignmentGeneration,
			Op:                   replication.OpRemovePath,
			Path:                 stringPointer(normalized),
			IsDir:                isDir,
		}
	}))
}

func (r *OutboxMutationRecorder) appendBatch(ctx context.Context, events []*replication.OutboxEvent) error {
	if len(events) == 0 {
		return nil
	}
	if err := r.outbox.AppendBatch(ctx, events); err != nil {
		return err
	}
	if r.logger == nil {
		return nil
	}
	for _, event := range events {
		if event == nil {
			continue
		}
		r.logger.Debug("replication mutation recorded",
			zap.String("op", event.Op),
			zap.Int64("outbox_id", event.ID),
			zap.String("source_node_id", event.SourceNodeID),
			zap.String("target_node_id", event.TargetNodeID),
		)
	}
	return nil
}

func (r *OutboxMutationRecorder) resolveTargets(ctx context.Context) ([]*ResolvedReplicationPeer, error) {
	if r.peerResolver == nil {
		return nil, ErrReplicationPeerUnavailable
	}
	peers, err := r.peerResolver.ResolveTargets(ctx)
	if err != nil {
		return nil, err
	}
	if len(peers) == 0 {
		return nil, ErrReplicationPeerUnavailable
	}
	for _, peer := range peers {
		if peer == nil || strings.TrimSpace(peer.NodeID) == "" {
			return nil, ErrReplicationPeerUnavailable
		}
		if peer.AssignmentGeneration == nil || *peer.AssignmentGeneration <= 0 {
			return nil, ErrReplicationAssignmentUnavailable
		}
	}
	return peers, nil
}

func (r *OutboxMutationRecorder) normalizeFullPath(fullPath string) (string, error) {
	trimmed := strings.TrimSpace(fullPath)
	if trimmed == "" {
		return "", fmt.Errorf("path is empty")
	}
	absPath, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path %q: %w", fullPath, err)
	}
	absPath = filepath.Clean(absPath)

	rel, err := filepath.Rel(r.webdavRoot, absPath)
	if err != nil {
		return "", fmt.Errorf("normalize path %q relative to webdav root: %w", fullPath, err)
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return "/", nil
	}
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("path %q is outside webdav root %q", absPath, r.webdavRoot)
	}
	return "/" + strings.TrimPrefix(rel, "/"), nil
}

func fileDigest(fullPath string) (int64, string, error) {
	file, err := os.Open(fullPath)
	if err != nil {
		return 0, "", fmt.Errorf("open file %q for digest: %w", fullPath, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return 0, "", fmt.Errorf("stat file %q for digest: %w", fullPath, err)
	}
	if info.IsDir() {
		return 0, "", fmt.Errorf("cannot compute file digest for directory %q", fullPath)
	}

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return 0, "", fmt.Errorf("hash file %q: %w", fullPath, err)
	}
	return info.Size(), hex.EncodeToString(hasher.Sum(nil)), nil
}

func isReplicationPeerUnavailable(err error) bool {
	return errors.Is(err, ErrReplicationPeerUnavailable) || errors.Is(err, ErrReplicationAssignmentUnavailable)
}

func stringPointer(value string) *string {
	v := value
	return &v
}

func int64Pointer(value int64) *int64 {
	v := value
	return &v
}

func newOutboxEventsForTargets(targets []*ResolvedReplicationPeer, build func(target *ResolvedReplicationPeer) *replication.OutboxEvent) []*replication.OutboxEvent {
	if len(targets) == 0 {
		return nil
	}
	events := make([]*replication.OutboxEvent, 0, len(targets))
	for _, target := range targets {
		if target == nil {
			continue
		}
		event := build(target)
		if event == nil {
			continue
		}
		events = append(events, event)
	}
	return events
}
