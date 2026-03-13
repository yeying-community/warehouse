package service

import (
	"context"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/cluster"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
	"go.uber.org/zap"
)

// NodeHeartbeatRegistrar periodically refreshes the local node registration row.
type NodeHeartbeatRegistrar struct {
	config   *config.Config
	nodes    repository.ClusterNodeRepository
	logger   *zap.Logger
	now      func() time.Time
	interval time.Duration
}

// NewNodeHeartbeatRegistrar creates a node heartbeat registrar.
func NewNodeHeartbeatRegistrar(cfg *config.Config, nodes repository.ClusterNodeRepository, logger *zap.Logger) *NodeHeartbeatRegistrar {
	if cfg == nil || nodes == nil {
		return nil
	}
	return &NodeHeartbeatRegistrar{
		config:   cfg,
		nodes:    nodes,
		logger:   logger,
		now:      time.Now,
		interval: defaultClusterHeartbeatInterval,
	}
}

// Enabled reports whether the current node should register into cluster_nodes.
func (r *NodeHeartbeatRegistrar) Enabled() bool {
	if r == nil || r.config == nil {
		return false
	}
	return r.config.Internal.Replication.Enabled &&
		strings.TrimSpace(r.config.Node.ID) != "" &&
		strings.TrimSpace(r.config.Node.AdvertiseURL) != ""
}

// Run starts the heartbeat loop until ctx is canceled.
func (r *NodeHeartbeatRegistrar) Run(ctx context.Context) {
	if !r.Enabled() {
		return
	}

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	if r.logger != nil {
		r.logger.Info("cluster node heartbeat registrar started",
			zap.String("node_id", r.config.Node.ID),
			zap.String("role", r.config.Node.Role),
			zap.String("advertise_url", r.config.Node.AdvertiseURL),
			zap.Duration("interval", r.interval))
		defer r.logger.Info("cluster node heartbeat registrar stopped",
			zap.String("node_id", r.config.Node.ID))
	}

	r.upsert(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.upsert(ctx)
		}
	}
}

func (r *NodeHeartbeatRegistrar) upsert(ctx context.Context) {
	if err := r.nodes.UpsertHeartbeat(ctx, &cluster.Node{
		NodeID:          r.config.Node.ID,
		Role:            r.config.Node.Role,
		AdvertiseURL:    r.config.Node.AdvertiseURL,
		LastHeartbeatAt: r.now().UTC(),
	}); err != nil && r.logger != nil {
		r.logger.Warn("failed to upsert cluster node heartbeat",
			zap.String("node_id", r.config.Node.ID),
			zap.Error(err))
	}
}
