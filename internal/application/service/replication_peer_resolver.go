package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/cluster"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
)

const (
	defaultClusterHeartbeatInterval = 5 * time.Second
	defaultClusterNodeStaleness     = 15 * time.Second
)

var (
	ErrReplicationPeerUnavailable       = errors.New("replication peer is unavailable")
	ErrReplicationAssignmentUnavailable = errors.New("replication assignment is unavailable")
)

// ResolvedReplicationPeer describes the selected peer for the current node.
type ResolvedReplicationPeer struct {
	NodeID               string
	BaseURL              string
	Source               string
	Healthy              bool
	LastHeartbeatAt      *time.Time
	AssignmentGeneration *int64
}

// ReplicationPeerResolver chooses the current peer using static config and/or shared control plane state.
type ReplicationPeerResolver interface {
	ResolveTarget(ctx context.Context) (*ResolvedReplicationPeer, error)
	ResolveDispatchTarget(ctx context.Context) (*ResolvedReplicationPeer, error)
	ResolveByNodeID(ctx context.Context, nodeID string, requireHealthy bool) (*ResolvedReplicationPeer, error)
}

// ClusterReplicationPeerResolver discovers peers via cluster_nodes with static fallback.
type ClusterReplicationPeerResolver struct {
	config        *config.Config
	nodes         repository.ClusterNodeRepository
	assignments   repository.ClusterReplicationAssignmentRepository
	now           func() time.Time
	maxStaleness  time.Duration
	selfRole      string
	peerRole      string
	configuredID  string
	configuredURL string
}

// NewReplicationPeerResolver creates a new peer resolver.
func NewReplicationPeerResolver(
	cfg *config.Config,
	nodes repository.ClusterNodeRepository,
	assignments ...repository.ClusterReplicationAssignmentRepository,
) ReplicationPeerResolver {
	if cfg == nil {
		return nil
	}
	var assignmentRepo repository.ClusterReplicationAssignmentRepository
	if len(assignments) > 0 {
		assignmentRepo = assignments[0]
	}
	selfRole := strings.ToLower(strings.TrimSpace(cfg.Node.Role))
	peerRole := "standby"
	if selfRole == "standby" {
		peerRole = "active"
	}
	return &ClusterReplicationPeerResolver{
		config:        cfg,
		nodes:         nodes,
		assignments:   assignmentRepo,
		now:           time.Now,
		maxStaleness:  defaultClusterNodeStaleness,
		selfRole:      selfRole,
		peerRole:      peerRole,
		configuredID:  strings.TrimSpace(cfg.Internal.Replication.PeerNodeID),
		configuredURL: strings.TrimSpace(cfg.Internal.Replication.PeerBaseURL),
	}
}

// ResolveTarget resolves a peer for durable source->target pairing. Stale registry rows are accepted.
func (r *ClusterReplicationPeerResolver) ResolveTarget(ctx context.Context) (*ResolvedReplicationPeer, error) {
	if r == nil {
		return nil, nil
	}
	peer, err := r.resolveByAssignment(ctx, false)
	if err != nil || peer != nil {
		return peer, err
	}
	if r.assignments != nil {
		return nil, nil
	}
	if r.configuredID != "" {
		return r.ResolveByNodeID(ctx, r.configuredID, false)
	}
	return r.resolveByRole(ctx, false)
}

// ResolveDispatchTarget resolves a currently reachable peer for active dispatch/reconcile traffic.
func (r *ClusterReplicationPeerResolver) ResolveDispatchTarget(ctx context.Context) (*ResolvedReplicationPeer, error) {
	if r == nil {
		return nil, nil
	}
	peer, err := r.resolveByAssignment(ctx, true)
	if err != nil || peer != nil {
		return peer, err
	}
	if r.assignments != nil {
		return nil, nil
	}
	if r.configuredID != "" {
		return r.ResolveByNodeID(ctx, r.configuredID, true)
	}
	return r.resolveByRole(ctx, true)
}

// ResolveByNodeID resolves one explicit peer node.
func (r *ClusterReplicationPeerResolver) ResolveByNodeID(ctx context.Context, nodeID string, requireHealthy bool) (*ResolvedReplicationPeer, error) {
	nodeID = strings.TrimSpace(nodeID)
	if r == nil || nodeID == "" {
		return nil, nil
	}
	configuredURLApplicable := r.configuredID != "" && nodeID == r.configuredID && r.configuredURL != ""
	source := "explicit"
	if r.configuredID != "" && nodeID == r.configuredID {
		source = "config"
	}

	peer := &ResolvedReplicationPeer{
		NodeID: nodeID,
		Source: source,
	}
	if configuredURLApplicable {
		peer.BaseURL = r.configuredURL
	}

	if r.nodes == nil {
		return r.finalizeResolvedPeer(peer, requireHealthy, configuredURLApplicable)
	}

	node, err := r.nodes.Get(ctx, nodeID)
	if err != nil {
		if errors.Is(err, cluster.ErrNodeNotFound) {
			return r.finalizeResolvedPeer(peer, requireHealthy, configuredURLApplicable)
		}
		return nil, err
	}
	healthy := node.Healthy(r.now().UTC(), r.maxStaleness)
	peer.Healthy = healthy
	peer.LastHeartbeatAt = &node.LastHeartbeatAt

	registryURL := strings.TrimSpace(node.AdvertiseURL)
	switch {
	case peer.BaseURL == "" && registryURL != "":
		peer.BaseURL = registryURL
		peer.Source = "registry"
	case peer.BaseURL != "" && registryURL != "" && peer.BaseURL == registryURL:
		peer.Source = source + "+registry"
	}

	return r.finalizeResolvedPeer(peer, requireHealthy, configuredURLApplicable)
}

func (r *ClusterReplicationPeerResolver) resolveByAssignment(ctx context.Context, requireHealthy bool) (*ResolvedReplicationPeer, error) {
	if r == nil || r.assignments == nil {
		return nil, nil
	}

	selfNodeID := strings.TrimSpace(r.config.Node.ID)
	if selfNodeID == "" {
		return nil, nil
	}

	switch r.selfRole {
	case "active":
		assignments, err := r.assignments.ListEffectiveByActive(ctx, selfNodeID)
		if err != nil {
			return nil, err
		}
		for _, assignment := range assignments {
			if assignment == nil {
				continue
			}
			peer, err := r.ResolveByNodeID(ctx, assignment.StandbyNodeID, requireHealthy)
			if err != nil {
				return nil, err
			}
			if peer != nil {
				peer.Source = prefixPeerSource("assignment", peer.Source)
				peer.AssignmentGeneration = int64Pointer(assignment.Generation)
				return peer, nil
			}
		}
	case "standby":
		assignment, err := r.assignments.GetEffectiveByStandby(ctx, selfNodeID)
		if err != nil {
			return nil, err
		}
		if assignment == nil {
			return nil, nil
		}
		peer, err := r.ResolveByNodeID(ctx, assignment.ActiveNodeID, requireHealthy)
		if err != nil {
			return nil, err
		}
		if peer != nil {
			peer.Source = prefixPeerSource("assignment", peer.Source)
			peer.AssignmentGeneration = int64Pointer(assignment.Generation)
		}
		return peer, nil
	}

	return nil, nil
}

func (r *ClusterReplicationPeerResolver) resolveByRole(ctx context.Context, requireHealthy bool) (*ResolvedReplicationPeer, error) {
	if r.nodes == nil {
		return nil, nil
	}

	nodes, err := r.nodes.ListByRole(ctx, r.peerRole)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, nil
	}

	var selected *cluster.Node
	for _, node := range nodes {
		if strings.TrimSpace(node.NodeID) == "" || strings.TrimSpace(node.AdvertiseURL) == "" {
			continue
		}
		if node.Healthy(r.now().UTC(), r.maxStaleness) {
			selected = node
			break
		}
		if !requireHealthy && selected == nil {
			selected = node
		}
	}
	if selected == nil {
		return nil, nil
	}

	lastHeartbeatAt := selected.LastHeartbeatAt
	return &ResolvedReplicationPeer{
		NodeID:          selected.NodeID,
		BaseURL:         strings.TrimSpace(selected.AdvertiseURL),
		Source:          "registry",
		Healthy:         selected.Healthy(r.now().UTC(), r.maxStaleness),
		LastHeartbeatAt: &lastHeartbeatAt,
	}, nil
}

func (r *ClusterReplicationPeerResolver) finalizeResolvedPeer(peer *ResolvedReplicationPeer, requireHealthy, configuredURL bool) (*ResolvedReplicationPeer, error) {
	if peer == nil {
		return nil, nil
	}
	peer.NodeID = strings.TrimSpace(peer.NodeID)
	peer.BaseURL = strings.TrimSpace(peer.BaseURL)
	if peer.NodeID == "" {
		return nil, nil
	}
	if !requireHealthy {
		return peer, nil
	}
	if peer.BaseURL == "" {
		return nil, nil
	}
	if configuredURL {
		return peer, nil
	}
	if !peer.Healthy {
		return nil, nil
	}
	return peer, nil
}

func prefixPeerSource(prefix, source string) string {
	prefix = strings.TrimSpace(prefix)
	source = strings.TrimSpace(source)
	switch {
	case prefix == "":
		return source
	case source == "":
		return prefix
	case source == prefix:
		return prefix
	default:
		return prefix + "+" + source
	}
}
