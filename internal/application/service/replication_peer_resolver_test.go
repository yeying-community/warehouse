package service

import (
	"context"
	"testing"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/cluster"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
)

type fakeClusterNodeRepository struct {
	nodesByID map[string]*cluster.Node
	byRole    map[string][]*cluster.Node
	err       error
}

type fakeClusterAssignmentRepository struct {
	byActive  map[string][]*cluster.ReplicationAssignment
	byStandby map[string]*cluster.ReplicationAssignment
	byPair    map[string]*cluster.ReplicationAssignment
}

func (r *fakeClusterNodeRepository) UpsertHeartbeat(context.Context, *cluster.Node) error {
	return nil
}

func (r *fakeClusterNodeRepository) Get(_ context.Context, nodeID string) (*cluster.Node, error) {
	if r.err != nil {
		return nil, r.err
	}
	node, ok := r.nodesByID[nodeID]
	if !ok {
		return nil, cluster.ErrNodeNotFound
	}
	return node, nil
}

func (r *fakeClusterNodeRepository) ListByRole(_ context.Context, role string) ([]*cluster.Node, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.byRole[role], nil
}

func (r *fakeClusterAssignmentRepository) List(context.Context, repository.ClusterReplicationAssignmentFilter) ([]*cluster.ReplicationAssignment, error) {
	return nil, nil
}

func (r *fakeClusterAssignmentRepository) ListEffectiveByActive(_ context.Context, activeNodeID string) ([]*cluster.ReplicationAssignment, error) {
	assignments := r.byActive[activeNodeID]
	result := make([]*cluster.ReplicationAssignment, 0, len(assignments))
	for _, assignment := range assignments {
		if assignment == nil {
			continue
		}
		copied := *assignment
		result = append(result, &copied)
	}
	return result, nil
}

func (r *fakeClusterAssignmentRepository) GetEffectiveByStandby(_ context.Context, standbyNodeID string) (*cluster.ReplicationAssignment, error) {
	assignment := r.byStandby[standbyNodeID]
	if assignment == nil {
		return nil, nil
	}
	copied := *assignment
	return &copied, nil
}

func (r *fakeClusterAssignmentRepository) GetByPair(_ context.Context, activeNodeID, standbyNodeID string) (*cluster.ReplicationAssignment, error) {
	assignment := r.byPair[activeNodeID+"->"+standbyNodeID]
	if assignment == nil {
		return nil, nil
	}
	copied := *assignment
	return &copied, nil
}

func (r *fakeClusterAssignmentRepository) UpsertLease(context.Context, *cluster.ReplicationAssignment) error {
	return nil
}

func (r *fakeClusterAssignmentRepository) UpdateState(context.Context, *cluster.ReplicationAssignment) error {
	return nil
}

func (r *fakeClusterAssignmentRepository) ReleaseByActiveExcept(context.Context, string, []string) error {
	return nil
}

func TestReplicationPeerResolverResolveByNodeIDUsesRegistryURL(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Node.Role = "active"

	heartbeat := time.Date(2026, 3, 13, 8, 0, 0, 0, time.UTC)
	repo := &fakeClusterNodeRepository{
		nodesByID: map[string]*cluster.Node{
			"node-b": {
				NodeID:          "node-b",
				Role:            "standby",
				AdvertiseURL:    "http://127.0.0.1:6066",
				LastHeartbeatAt: heartbeat,
			},
		},
	}

	resolver := NewReplicationPeerResolver(cfg, repo).(*ClusterReplicationPeerResolver)
	resolver.now = func() time.Time { return heartbeat.Add(5 * time.Second) }

	peer, err := resolver.ResolveByNodeID(context.Background(), "node-b", true)
	if err != nil {
		t.Fatalf("ResolveByNodeID: %v", err)
	}
	if peer == nil {
		t.Fatalf("expected peer to be resolved")
	}
	if peer.NodeID != "node-b" || peer.BaseURL != "http://127.0.0.1:6066" {
		t.Fatalf("unexpected resolved peer: %#v", peer)
	}
	if !peer.Healthy || peer.Source != "registry" {
		t.Fatalf("unexpected peer metadata: %#v", peer)
	}
}

func TestReplicationPeerResolverFallsBackToLatestStandbyWithoutAssignmentRepo(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Node.Role = "active"

	now := time.Date(2026, 3, 13, 8, 0, 0, 0, time.UTC)
	repo := &fakeClusterNodeRepository{
		byRole: map[string][]*cluster.Node{
			"standby": {
				{
					NodeID:          "node-b",
					Role:            "standby",
					AdvertiseURL:    "http://127.0.0.1:6066",
					LastHeartbeatAt: now.Add(-3 * time.Second),
				},
				{
					NodeID:          "node-c",
					Role:            "standby",
					AdvertiseURL:    "http://127.0.0.1:6067",
					LastHeartbeatAt: now.Add(-30 * time.Second),
				},
			},
		},
	}

	resolver := NewReplicationPeerResolver(cfg, repo).(*ClusterReplicationPeerResolver)
	resolver.now = func() time.Time { return now }

	peer, err := resolver.ResolveTarget(context.Background())
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if peer == nil || peer.NodeID != "node-b" {
		t.Fatalf("expected latest healthy standby, got %#v", peer)
	}

	dispatchPeer, err := resolver.ResolveDispatchTarget(context.Background())
	if err != nil {
		t.Fatalf("ResolveDispatchTarget: %v", err)
	}
	if dispatchPeer == nil || dispatchPeer.NodeID != "node-b" {
		t.Fatalf("expected dispatch standby node-b, got %#v", dispatchPeer)
	}
}

func TestReplicationPeerResolverPrefersEffectiveAssignmentForActive(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-a"
	cfg.Node.Role = "active"

	now := time.Date(2026, 3, 13, 8, 0, 0, 0, time.UTC)
	nodes := &fakeClusterNodeRepository{
		nodesByID: map[string]*cluster.Node{
			"node-b": {
				NodeID:          "node-b",
				Role:            "standby",
				AdvertiseURL:    "http://127.0.0.1:6066",
				LastHeartbeatAt: now,
			},
			"node-c": {
				NodeID:          "node-c",
				Role:            "standby",
				AdvertiseURL:    "http://127.0.0.1:6067",
				LastHeartbeatAt: now.Add(-time.Second),
			},
		},
		byRole: map[string][]*cluster.Node{
			"standby": {
				{
					NodeID:          "node-b",
					Role:            "standby",
					AdvertiseURL:    "http://127.0.0.1:6066",
					LastHeartbeatAt: now,
				},
				{
					NodeID:          "node-c",
					Role:            "standby",
					AdvertiseURL:    "http://127.0.0.1:6067",
					LastHeartbeatAt: now.Add(-time.Second),
				},
			},
		},
	}
	assignments := &fakeClusterAssignmentRepository{
		byActive: map[string][]*cluster.ReplicationAssignment{
			"node-a": {
				{
					ActiveNodeID:   "node-a",
					StandbyNodeID:  "node-c",
					State:          cluster.AssignmentStateReplicating,
					LeaseExpiresAt: timePointer(now.Add(20 * time.Second)),
				},
			},
		},
	}

	resolver := NewReplicationPeerResolver(cfg, nodes, assignments).(*ClusterReplicationPeerResolver)
	resolver.now = func() time.Time { return now }

	peer, err := resolver.ResolveTarget(context.Background())
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if peer == nil || peer.NodeID != "node-c" {
		t.Fatalf("expected assignment standby node-c, got %#v", peer)
	}
	if peer.Source != "assignment+registry" {
		t.Fatalf("unexpected peer source: %#v", peer)
	}
}

func TestReplicationPeerResolverDoesNotFallbackWithoutEffectiveAssignment(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-a"
	cfg.Node.Role = "active"

	now := time.Date(2026, 3, 13, 8, 0, 0, 0, time.UTC)
	nodes := &fakeClusterNodeRepository{
		nodesByID: map[string]*cluster.Node{
			"node-b": {
				NodeID:          "node-b",
				Role:            "standby",
				AdvertiseURL:    "http://127.0.0.1:6066",
				LastHeartbeatAt: now,
			},
		},
		byRole: map[string][]*cluster.Node{
			"standby": {
				{
					NodeID:          "node-b",
					Role:            "standby",
					AdvertiseURL:    "http://127.0.0.1:6066",
					LastHeartbeatAt: now,
				},
			},
		},
	}
	assignments := &fakeClusterAssignmentRepository{}

	resolver := NewReplicationPeerResolver(cfg, nodes, assignments).(*ClusterReplicationPeerResolver)
	resolver.now = func() time.Time { return now }

	peer, err := resolver.ResolveTarget(context.Background())
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if peer != nil {
		t.Fatalf("expected no peer without effective assignment, got %#v", peer)
	}

	dispatchPeer, err := resolver.ResolveDispatchTarget(context.Background())
	if err != nil {
		t.Fatalf("ResolveDispatchTarget: %v", err)
	}
	if dispatchPeer != nil {
		t.Fatalf("expected no dispatch peer without effective assignment, got %#v", dispatchPeer)
	}
}

func TestReplicationPeerResolverUsesEffectiveAssignmentForStandby(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-b"
	cfg.Node.Role = "standby"

	now := time.Date(2026, 3, 13, 8, 0, 0, 0, time.UTC)
	nodes := &fakeClusterNodeRepository{
		nodesByID: map[string]*cluster.Node{
			"node-a": {
				NodeID:          "node-a",
				Role:            "active",
				AdvertiseURL:    "http://127.0.0.1:6065",
				LastHeartbeatAt: now,
			},
		},
	}
	assignments := &fakeClusterAssignmentRepository{
		byStandby: map[string]*cluster.ReplicationAssignment{
			"node-b": {
				ActiveNodeID:   "node-a",
				StandbyNodeID:  "node-b",
				State:          cluster.AssignmentStateReplicating,
				LeaseExpiresAt: timePointer(now.Add(20 * time.Second)),
			},
		},
	}

	resolver := NewReplicationPeerResolver(cfg, nodes, assignments).(*ClusterReplicationPeerResolver)
	resolver.now = func() time.Time { return now }

	peer, err := resolver.ResolveTarget(context.Background())
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if peer == nil || peer.NodeID != "node-a" || peer.BaseURL != "http://127.0.0.1:6065" {
		t.Fatalf("unexpected resolved peer: %#v", peer)
	}
	if peer.Source != "assignment+registry" {
		t.Fatalf("unexpected peer source: %#v", peer)
	}
}

func timePointer(value time.Time) *time.Time {
	v := value
	return &v
}
