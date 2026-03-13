package service

import (
	"context"
	"testing"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/cluster"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
	"go.uber.org/zap"
)

type fakeAllocatorNodeRepository struct {
	nodesByRole map[string][]*cluster.Node
}

func (r *fakeAllocatorNodeRepository) UpsertHeartbeat(context.Context, *cluster.Node) error {
	return nil
}

func (r *fakeAllocatorNodeRepository) Get(context.Context, string) (*cluster.Node, error) {
	return nil, cluster.ErrNodeNotFound
}

func (r *fakeAllocatorNodeRepository) ListByRole(_ context.Context, role string) ([]*cluster.Node, error) {
	nodes := r.nodesByRole[role]
	result := make([]*cluster.Node, 0, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		copied := *node
		result = append(result, &copied)
	}
	return result, nil
}

type fakeAllocatorAssignmentRepository struct {
	listEffectiveByActiveID string
	getByPairActiveNodeID   string
	getByPairStandbyNodeID  string
	effectiveAssignments    []*cluster.ReplicationAssignment
	assignmentsByPair       map[string]*cluster.ReplicationAssignment
	upserted                []*cluster.ReplicationAssignment
	releaseActiveNodeID     string
	releaseKeepStandbyIDs   []string
}

func (r *fakeAllocatorAssignmentRepository) List(context.Context, repository.ClusterReplicationAssignmentFilter) ([]*cluster.ReplicationAssignment, error) {
	return nil, nil
}

func (r *fakeAllocatorAssignmentRepository) ListEffectiveByActive(_ context.Context, activeNodeID string) ([]*cluster.ReplicationAssignment, error) {
	r.listEffectiveByActiveID = activeNodeID
	result := make([]*cluster.ReplicationAssignment, 0, len(r.effectiveAssignments))
	for _, assignment := range r.effectiveAssignments {
		if assignment == nil {
			continue
		}
		copied := *assignment
		result = append(result, &copied)
	}
	return result, nil
}

func (r *fakeAllocatorAssignmentRepository) GetEffectiveByStandby(context.Context, string) (*cluster.ReplicationAssignment, error) {
	return nil, nil
}

func (r *fakeAllocatorAssignmentRepository) GetByPair(_ context.Context, activeNodeID, standbyNodeID string) (*cluster.ReplicationAssignment, error) {
	r.getByPairActiveNodeID = activeNodeID
	r.getByPairStandbyNodeID = standbyNodeID
	if assignment := r.assignmentsByPair[activeNodeID+"->"+standbyNodeID]; assignment != nil {
		copied := *assignment
		return &copied, nil
	}
	for _, assignment := range r.effectiveAssignments {
		if assignment == nil {
			continue
		}
		if assignment.ActiveNodeID == activeNodeID && assignment.StandbyNodeID == standbyNodeID {
			copied := *assignment
			return &copied, nil
		}
	}
	return nil, nil
}

func (r *fakeAllocatorAssignmentRepository) UpsertLease(_ context.Context, assignment *cluster.ReplicationAssignment) error {
	copied := *assignment
	copied.ID = int64(len(r.upserted) + 1)
	copied.Generation = 1
	if existing := r.assignmentsByPair[copied.ActiveNodeID+"->"+copied.StandbyNodeID]; existing != nil {
		copied.Generation = existing.Generation
		if cluster.AdvancesAssignmentGeneration(existing.State, copied.State) {
			copied.Generation++
		}
	}
	r.upserted = append(r.upserted, &copied)
	if r.assignmentsByPair == nil {
		r.assignmentsByPair = make(map[string]*cluster.ReplicationAssignment)
	}
	r.assignmentsByPair[copied.ActiveNodeID+"->"+copied.StandbyNodeID] = &copied
	if assignment != nil {
		assignment.ID = copied.ID
		assignment.Generation = copied.Generation
	}
	return nil
}

func (r *fakeAllocatorAssignmentRepository) UpdateState(context.Context, *cluster.ReplicationAssignment) error {
	return nil
}

func (r *fakeAllocatorAssignmentRepository) ReleaseByActiveExcept(_ context.Context, activeNodeID string, keepStandbyIDs []string) error {
	r.releaseActiveNodeID = activeNodeID
	r.releaseKeepStandbyIDs = append([]string(nil), keepStandbyIDs...)
	return nil
}

func TestReplicationAssignmentAllocatorSyncOnceConfiguredPeer(t *testing.T) {
	now := time.Date(2026, 3, 13, 10, 0, 0, 0, time.UTC)
	cfg := config.DefaultConfig()
	cfg.Node.ID = "active-a"
	cfg.Node.Role = "active"
	cfg.Internal.Replication.Enabled = true
	cfg.Internal.Replication.PeerNodeID = "standby-b"

	nodes := &fakeAllocatorNodeRepository{
		nodesByRole: map[string][]*cluster.Node{
			"standby": {
				{NodeID: "standby-a", Role: "standby", AdvertiseURL: "http://standby-a", LastHeartbeatAt: now},
				{NodeID: "standby-b", Role: "standby", AdvertiseURL: "http://standby-b", LastHeartbeatAt: now},
			},
		},
	}
	assignments := &fakeAllocatorAssignmentRepository{
		effectiveAssignments: []*cluster.ReplicationAssignment{
			{ActiveNodeID: "active-a", StandbyNodeID: "standby-a", State: cluster.AssignmentStateReplicating},
		},
	}

	allocator := NewReplicationAssignmentAllocator(cfg, nodes, assignments, zap.NewNop())
	allocator.now = func() time.Time { return now }

	if err := allocator.SyncOnce(context.Background()); err != nil {
		t.Fatalf("SyncOnce: %v", err)
	}
	if assignments.listEffectiveByActiveID != "" {
		t.Fatalf("expected configured peer path to avoid current assignment lookup, got %q", assignments.listEffectiveByActiveID)
	}
	if len(assignments.upserted) != 1 {
		t.Fatalf("expected 1 upsert, got %d", len(assignments.upserted))
	}
	if assignments.upserted[0].StandbyNodeID != "standby-b" {
		t.Fatalf("unexpected upserted standby: %#v", assignments.upserted[0])
	}
	if assignments.upserted[0].State != cluster.AssignmentStatePending {
		t.Fatalf("expected new configured assignment to start pending, got %#v", assignments.upserted[0])
	}
	if len(assignments.releaseKeepStandbyIDs) != 1 || assignments.releaseKeepStandbyIDs[0] != "standby-b" {
		t.Fatalf("unexpected release keep set: %#v", assignments.releaseKeepStandbyIDs)
	}
	if assignments.releaseActiveNodeID != "active-a" {
		t.Fatalf("unexpected release active node id: %q", assignments.releaseActiveNodeID)
	}
}

func TestReplicationAssignmentAllocatorSyncOnceKeepsCurrentHealthyStandby(t *testing.T) {
	now := time.Date(2026, 3, 13, 10, 0, 0, 0, time.UTC)
	cfg := config.DefaultConfig()
	cfg.Node.ID = "active-a"
	cfg.Node.Role = "active"
	cfg.Internal.Replication.Enabled = true

	nodes := &fakeAllocatorNodeRepository{
		nodesByRole: map[string][]*cluster.Node{
			"standby": {
				{NodeID: "standby-b", Role: "standby", AdvertiseURL: "http://standby-b", LastHeartbeatAt: now},
				{NodeID: "standby-a", Role: "standby", AdvertiseURL: "http://standby-a", LastHeartbeatAt: now.Add(-time.Second)},
			},
		},
	}
	assignments := &fakeAllocatorAssignmentRepository{
		effectiveAssignments: []*cluster.ReplicationAssignment{
			{ActiveNodeID: "active-a", StandbyNodeID: "standby-a", State: cluster.AssignmentStateReplicating},
		},
	}

	allocator := NewReplicationAssignmentAllocator(cfg, nodes, assignments, zap.NewNop())
	allocator.now = func() time.Time { return now }

	if err := allocator.SyncOnce(context.Background()); err != nil {
		t.Fatalf("SyncOnce: %v", err)
	}
	if assignments.listEffectiveByActiveID != "active-a" {
		t.Fatalf("unexpected current assignment lookup scope: %q", assignments.listEffectiveByActiveID)
	}
	if len(assignments.upserted) != 1 {
		t.Fatalf("expected 1 upsert, got %d", len(assignments.upserted))
	}
	if assignments.upserted[0].StandbyNodeID != "standby-a" {
		t.Fatalf("expected current healthy standby to be kept, got %#v", assignments.upserted[0])
	}
	if assignments.upserted[0].State != cluster.AssignmentStateReplicating {
		t.Fatalf("expected current assignment state to be preserved, got %#v", assignments.upserted[0])
	}
}

func TestReplicationAssignmentAllocatorSyncOnceSelectsFreshHealthyStandby(t *testing.T) {
	now := time.Date(2026, 3, 13, 10, 0, 0, 0, time.UTC)
	cfg := config.DefaultConfig()
	cfg.Node.ID = "active-a"
	cfg.Node.Role = "active"
	cfg.Internal.Replication.Enabled = true

	nodes := &fakeAllocatorNodeRepository{
		nodesByRole: map[string][]*cluster.Node{
			"standby": {
				{NodeID: "standby-b", Role: "standby", AdvertiseURL: "http://standby-b", LastHeartbeatAt: now},
				{NodeID: "standby-a", Role: "standby", AdvertiseURL: "http://standby-a", LastHeartbeatAt: now.Add(-time.Second)},
			},
		},
	}
	assignments := &fakeAllocatorAssignmentRepository{}

	allocator := NewReplicationAssignmentAllocator(cfg, nodes, assignments, zap.NewNop())
	allocator.now = func() time.Time { return now }

	if err := allocator.SyncOnce(context.Background()); err != nil {
		t.Fatalf("SyncOnce: %v", err)
	}
	if len(assignments.upserted) != 1 {
		t.Fatalf("expected 1 upsert, got %d", len(assignments.upserted))
	}
	if assignments.upserted[0].StandbyNodeID != "standby-b" {
		t.Fatalf("expected freshest standby to be selected, got %#v", assignments.upserted[0])
	}
	if assignments.upserted[0].State != cluster.AssignmentStatePending {
		t.Fatalf("expected brand new assignment to start pending, got %#v", assignments.upserted[0])
	}
}

func TestReplicationAssignmentAllocatorSyncOnceRecoversErrorStateOnStartup(t *testing.T) {
	now := time.Date(2026, 3, 13, 10, 0, 0, 0, time.UTC)
	cfg := config.DefaultConfig()
	cfg.Node.ID = "active-a"
	cfg.Node.Role = "active"
	cfg.Internal.Replication.Enabled = true

	nodes := &fakeAllocatorNodeRepository{
		nodesByRole: map[string][]*cluster.Node{
			"standby": {
				{NodeID: "standby-a", Role: "standby", AdvertiseURL: "http://standby-a", LastHeartbeatAt: now},
			},
		},
	}
	assignments := &fakeAllocatorAssignmentRepository{
		assignmentsByPair: map[string]*cluster.ReplicationAssignment{
			"active-a->standby-a": {
				ActiveNodeID:  "active-a",
				StandbyNodeID: "standby-a",
				State:         cluster.AssignmentStateError,
				Generation:    3,
			},
		},
	}

	allocator := NewReplicationAssignmentAllocator(cfg, nodes, assignments, zap.NewNop())
	allocator.now = func() time.Time { return now }

	if err := allocator.SyncOnce(context.Background()); err != nil {
		t.Fatalf("SyncOnce: %v", err)
	}
	if len(assignments.upserted) != 1 {
		t.Fatalf("expected 1 upsert, got %d", len(assignments.upserted))
	}
	if assignments.upserted[0].State != cluster.AssignmentStatePending {
		t.Fatalf("expected startup recovery to move error to pending, got %#v", assignments.upserted[0])
	}
	if allocator.recoverError {
		t.Fatalf("expected startup error recovery flag to be consumed")
	}
}

func TestReplicationAssignmentAllocatorSyncOncePreservesErrorStateAfterStartupRecovery(t *testing.T) {
	now := time.Date(2026, 3, 13, 10, 0, 0, 0, time.UTC)
	cfg := config.DefaultConfig()
	cfg.Node.ID = "active-a"
	cfg.Node.Role = "active"
	cfg.Internal.Replication.Enabled = true

	nodes := &fakeAllocatorNodeRepository{
		nodesByRole: map[string][]*cluster.Node{
			"standby": {
				{NodeID: "standby-a", Role: "standby", AdvertiseURL: "http://standby-a", LastHeartbeatAt: now},
			},
		},
	}
	assignments := &fakeAllocatorAssignmentRepository{
		assignmentsByPair: map[string]*cluster.ReplicationAssignment{
			"active-a->standby-a": {
				ActiveNodeID:  "active-a",
				StandbyNodeID: "standby-a",
				State:         cluster.AssignmentStateError,
				Generation:    3,
			},
		},
	}

	allocator := NewReplicationAssignmentAllocator(cfg, nodes, assignments, zap.NewNop())
	allocator.now = func() time.Time { return now }
	allocator.recoverError = false

	if err := allocator.SyncOnce(context.Background()); err != nil {
		t.Fatalf("SyncOnce: %v", err)
	}
	if len(assignments.upserted) != 1 {
		t.Fatalf("expected 1 upsert, got %d", len(assignments.upserted))
	}
	if assignments.upserted[0].State != cluster.AssignmentStateError {
		t.Fatalf("expected error state to be preserved after startup recovery window, got %#v", assignments.upserted[0])
	}
}

func TestReplicationAssignmentAllocatorSyncOnceReleasesWhenNoHealthyStandby(t *testing.T) {
	now := time.Date(2026, 3, 13, 10, 0, 0, 0, time.UTC)
	cfg := config.DefaultConfig()
	cfg.Node.ID = "active-a"
	cfg.Node.Role = "active"
	cfg.Internal.Replication.Enabled = true

	nodes := &fakeAllocatorNodeRepository{
		nodesByRole: map[string][]*cluster.Node{
			"standby": {
				{NodeID: "standby-a", Role: "standby", AdvertiseURL: "http://standby-a", LastHeartbeatAt: now.Add(-time.Minute)},
			},
		},
	}
	assignments := &fakeAllocatorAssignmentRepository{
		effectiveAssignments: []*cluster.ReplicationAssignment{
			{ActiveNodeID: "active-a", StandbyNodeID: "standby-a", State: cluster.AssignmentStateReplicating},
		},
	}

	allocator := NewReplicationAssignmentAllocator(cfg, nodes, assignments, zap.NewNop())
	allocator.now = func() time.Time { return now }

	if err := allocator.SyncOnce(context.Background()); err != nil {
		t.Fatalf("SyncOnce: %v", err)
	}
	if len(assignments.upserted) != 0 {
		t.Fatalf("expected no upsert, got %#v", assignments.upserted)
	}
	if assignments.releaseActiveNodeID != "active-a" {
		t.Fatalf("unexpected release active node id: %q", assignments.releaseActiveNodeID)
	}
	if len(assignments.releaseKeepStandbyIDs) != 0 {
		t.Fatalf("expected empty keep set, got %#v", assignments.releaseKeepStandbyIDs)
	}
}
