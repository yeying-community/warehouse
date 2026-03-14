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

// ReplicationPeerResolver chooses the current peer from shared control-plane state.
type ReplicationPeerResolver interface {
	ResolveTarget(ctx context.Context) (*ResolvedReplicationPeer, error)
	ResolveDispatchTarget(ctx context.Context) (*ResolvedReplicationPeer, error)
	ResolveTargets(ctx context.Context) ([]*ResolvedReplicationPeer, error)
	ResolveDispatchTargets(ctx context.Context) ([]*ResolvedReplicationPeer, error)
	ResolveByNodeID(ctx context.Context, nodeID string, requireHealthy bool) (*ResolvedReplicationPeer, error)
}

// ClusterReplicationPeerResolver discovers peers via assignment + cluster_nodes.
type ClusterReplicationPeerResolver struct {
	config       *config.Config
	nodes        repository.ClusterNodeRepository
	assignments  repository.ClusterReplicationAssignmentRepository
	now          func() time.Time
	maxStaleness time.Duration
	selfRole     string
	peerRole     string
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
		config:       cfg,
		nodes:        nodes,
		assignments:  assignmentRepo,
		now:          time.Now,
		maxStaleness: defaultClusterNodeStaleness,
		selfRole:     selfRole,
		peerRole:     peerRole,
	}
}

// ResolveTarget resolves a peer for durable source->target pairing. Stale registry rows are accepted.
func (r *ClusterReplicationPeerResolver) ResolveTarget(ctx context.Context) (*ResolvedReplicationPeer, error) {
	peers, err := r.ResolveTargets(ctx)
	if err != nil || len(peers) == 0 {
		return nil, err
	}
	return peers[0], nil
}

// ResolveDispatchTarget resolves a currently reachable peer for active dispatch/reconcile traffic.
func (r *ClusterReplicationPeerResolver) ResolveDispatchTarget(ctx context.Context) (*ResolvedReplicationPeer, error) {
	if r == nil {
		return nil, nil
	}
	peers, err := r.ResolveDispatchTargets(ctx)
	if err != nil || len(peers) == 0 {
		return nil, err
	}
	return peers[0], nil
}

// ResolveTargets resolves all durable source->target peers for the current node.
func (r *ClusterReplicationPeerResolver) ResolveTargets(ctx context.Context) ([]*ResolvedReplicationPeer, error) {
	if r == nil {
		return nil, nil
	}
	peers, err := r.resolveByAssignmentMany(ctx, false)
	if err != nil || len(peers) > 0 {
		return peers, err
	}
	if r.assignments != nil {
		return nil, nil
	}
	return r.resolveByRoleMany(ctx, false)
}

// ResolveDispatchTargets resolves all currently reachable peers for dispatch/reconcile traffic.
func (r *ClusterReplicationPeerResolver) ResolveDispatchTargets(ctx context.Context) ([]*ResolvedReplicationPeer, error) {
	if r == nil {
		return nil, nil
	}
	peers, err := r.resolveByAssignmentMany(ctx, true)
	if err != nil || len(peers) > 0 {
		return peers, err
	}
	if r.assignments != nil {
		return nil, nil
	}
	return r.resolveByRoleMany(ctx, true)
}

// ResolveByNodeID resolves one explicit peer node.
func (r *ClusterReplicationPeerResolver) ResolveByNodeID(ctx context.Context, nodeID string, requireHealthy bool) (*ResolvedReplicationPeer, error) {
	nodeID = strings.TrimSpace(nodeID)
	if r == nil || nodeID == "" {
		return nil, nil
	}
	peer := &ResolvedReplicationPeer{
		NodeID: nodeID,
		Source: "explicit",
	}

	if r.nodes == nil {
		return r.finalizeResolvedPeer(peer, requireHealthy)
	}

	node, err := r.nodes.Get(ctx, nodeID)
	if err != nil {
		if errors.Is(err, cluster.ErrNodeNotFound) {
			return r.finalizeResolvedPeer(peer, requireHealthy)
		}
		return nil, err
	}
	healthy := node.Healthy(r.now().UTC(), r.maxStaleness)
	peer.Healthy = healthy
	peer.LastHeartbeatAt = &node.LastHeartbeatAt

	registryURL := strings.TrimSpace(node.AdvertiseURL)
	if registryURL != "" {
		peer.BaseURL = registryURL
		peer.Source = "registry"
	}

	return r.finalizeResolvedPeer(peer, requireHealthy)
}

func (r *ClusterReplicationPeerResolver) resolveByAssignmentMany(ctx context.Context, requireHealthy bool) ([]*ResolvedReplicationPeer, error) {
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
		peers := make([]*ResolvedReplicationPeer, 0, len(assignments))
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
				peers = append(peers, peer)
			}
		}
		return dedupeResolvedPeers(peers), nil
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
			return []*ResolvedReplicationPeer{peer}, nil
		}
		return nil, nil
	}

	return nil, nil
}

func (r *ClusterReplicationPeerResolver) resolveByRoleMany(ctx context.Context, requireHealthy bool) ([]*ResolvedReplicationPeer, error) {
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

	peers := make([]*ResolvedReplicationPeer, 0, len(nodes))
	for _, node := range nodes {
		if strings.TrimSpace(node.NodeID) == "" || strings.TrimSpace(node.AdvertiseURL) == "" {
			continue
		}
		healthy := node.Healthy(r.now().UTC(), r.maxStaleness)
		if requireHealthy && !healthy {
			continue
		}
		lastHeartbeatAt := node.LastHeartbeatAt
		peers = append(peers, &ResolvedReplicationPeer{
			NodeID:          strings.TrimSpace(node.NodeID),
			BaseURL:         strings.TrimSpace(node.AdvertiseURL),
			Source:          "registry",
			Healthy:         healthy,
			LastHeartbeatAt: &lastHeartbeatAt,
		})
	}
	return dedupeResolvedPeers(peers), nil
}

func (r *ClusterReplicationPeerResolver) finalizeResolvedPeer(peer *ResolvedReplicationPeer, requireHealthy bool) (*ResolvedReplicationPeer, error) {
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

func dedupeResolvedPeers(peers []*ResolvedReplicationPeer) []*ResolvedReplicationPeer {
	if len(peers) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(peers))
	result := make([]*ResolvedReplicationPeer, 0, len(peers))
	for _, peer := range peers {
		if peer == nil {
			continue
		}
		nodeID := strings.TrimSpace(peer.NodeID)
		if nodeID == "" {
			continue
		}
		if _, ok := seen[nodeID]; ok {
			continue
		}
		seen[nodeID] = struct{}{}
		result = append(result, peer)
	}
	return result
}
