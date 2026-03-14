package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/replication"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"go.uber.org/zap"
)

type fakeReplicationOutboxRepository struct {
	events []*replication.OutboxEvent
}

type fakeMutationRecorderResolver struct {
	peer  *ResolvedReplicationPeer
	peers []*ResolvedReplicationPeer
}

func (r *fakeReplicationOutboxRepository) Append(_ context.Context, event *replication.OutboxEvent) error {
	return r.AppendBatch(context.Background(), []*replication.OutboxEvent{event})
}

func (r *fakeReplicationOutboxRepository) AppendBatch(_ context.Context, events []*replication.OutboxEvent) error {
	for _, event := range events {
		if event == nil {
			continue
		}
		copied := *event
		copied.ID = int64(len(r.events) + 1)
		r.events = append(r.events, &copied)
	}
	return nil
}

func (r *fakeReplicationOutboxRepository) ListPending(context.Context, string, string, *int64, int) ([]*replication.OutboxEvent, error) {
	return nil, nil
}

func (r *fakeReplicationOutboxRepository) MarkDispatched(context.Context, int64, time.Time) error {
	return nil
}

func (r *fakeReplicationOutboxRepository) MarkFailed(context.Context, int64, string, time.Time) error {
	return nil
}

func (r *fakeReplicationOutboxRepository) GetStatusSummary(context.Context, string, string) (*replication.OutboxStatus, error) {
	return nil, nil
}

func (r fakeMutationRecorderResolver) ResolveTarget(context.Context) (*ResolvedReplicationPeer, error) {
	peers, _ := r.ResolveTargets(context.Background())
	if len(peers) == 0 {
		return nil, nil
	}
	return peers[0], nil
}

func (r fakeMutationRecorderResolver) ResolveDispatchTarget(context.Context) (*ResolvedReplicationPeer, error) {
	peers, _ := r.ResolveDispatchTargets(context.Background())
	if len(peers) == 0 {
		return nil, nil
	}
	return peers[0], nil
}

func (r fakeMutationRecorderResolver) ResolveTargets(context.Context) ([]*ResolvedReplicationPeer, error) {
	if len(r.peers) > 0 {
		return append([]*ResolvedReplicationPeer(nil), r.peers...), nil
	}
	if r.peer == nil {
		return nil, nil
	}
	return []*ResolvedReplicationPeer{r.peer}, nil
}

func (r fakeMutationRecorderResolver) ResolveDispatchTargets(context.Context) ([]*ResolvedReplicationPeer, error) {
	return r.ResolveTargets(context.Background())
}

func (r fakeMutationRecorderResolver) ResolveByNodeID(context.Context, string, bool) (*ResolvedReplicationPeer, error) {
	return r.peer, nil
}

func TestMutationRecorderUpsertFile(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "alice", "docs", "hello.txt")
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	payload := []byte("hello warehouse")
	if err := os.WriteFile(filePath, payload, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	repo := &fakeReplicationOutboxRepository{}
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-a"
	cfg.Node.Role = "active"
	cfg.Replication.Enabled = true
	cfg.WebDAV.Directory = root
	generation := int64(7)

	recorder := NewMutationRecorder(cfg, repo, fakeMutationRecorderResolver{
		peer: &ResolvedReplicationPeer{
			NodeID:               "node-b",
			AssignmentGeneration: &generation,
		},
	}, zap.NewNop())
	if err := recorder.UpsertFile(context.Background(), filePath); err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}
	if len(repo.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(repo.events))
	}
	event := repo.events[0]
	if event.Op != replication.OpUpsertFile || event.Path == nil || *event.Path != "/alice/docs/hello.txt" {
		t.Fatalf("unexpected event: %#v", event)
	}
	expectedSHA := sha256.Sum256(payload)
	if event.ContentSHA256 == nil || *event.ContentSHA256 != hex.EncodeToString(expectedSHA[:]) {
		t.Fatalf("unexpected sha: %#v", event.ContentSHA256)
	}
	if event.FileSize == nil || *event.FileSize != int64(len(payload)) {
		t.Fatalf("unexpected size: %#v", event.FileSize)
	}
	if event.AssignmentGeneration == nil || *event.AssignmentGeneration != generation {
		t.Fatalf("unexpected assignment generation: %#v", event.AssignmentGeneration)
	}
}

func TestMutationRecorderUpsertFileFansOutToMultipleTargets(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "alice", "docs", "hello.txt")
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	payload := []byte("hello warehouse")
	if err := os.WriteFile(filePath, payload, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	repo := &fakeReplicationOutboxRepository{}
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-a"
	cfg.Node.Role = "active"
	cfg.Replication.Enabled = true
	cfg.WebDAV.Directory = root
	generationA := int64(7)
	generationB := int64(8)

	recorder := NewMutationRecorder(cfg, repo, fakeMutationRecorderResolver{
		peers: []*ResolvedReplicationPeer{
			{
				NodeID:               "node-b",
				AssignmentGeneration: &generationA,
			},
			{
				NodeID:               "node-c",
				AssignmentGeneration: &generationB,
			},
		},
	}, zap.NewNop())
	if err := recorder.UpsertFile(context.Background(), filePath); err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}
	if len(repo.events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(repo.events))
	}
	if repo.events[0].TargetNodeID != "node-b" || repo.events[1].TargetNodeID != "node-c" {
		t.Fatalf("unexpected target fan-out: %#v", repo.events)
	}
	if repo.events[0].AssignmentGeneration == nil || *repo.events[0].AssignmentGeneration != generationA {
		t.Fatalf("unexpected generation for node-b: %#v", repo.events[0].AssignmentGeneration)
	}
	if repo.events[1].AssignmentGeneration == nil || *repo.events[1].AssignmentGeneration != generationB {
		t.Fatalf("unexpected generation for node-c: %#v", repo.events[1].AssignmentGeneration)
	}
}

func TestMutationRecorderMoveCopyRemoveAndEnsureDir(t *testing.T) {
	root := t.TempDir()
	fromPath := filepath.Join(root, "alice", "src.txt")
	toPath := filepath.Join(root, "alice", "nested", "dst.txt")
	if err := os.MkdirAll(filepath.Dir(fromPath), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(fromPath, []byte("payload"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	repo := &fakeReplicationOutboxRepository{}
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-a"
	cfg.Node.Role = "active"
	cfg.Replication.Enabled = true
	cfg.WebDAV.Directory = root
	generation := int64(9)

	recorder := NewMutationRecorder(cfg, repo, fakeMutationRecorderResolver{
		peer: &ResolvedReplicationPeer{
			NodeID:               "node-b",
			AssignmentGeneration: &generation,
		},
	}, zap.NewNop())
	if err := recorder.EnsureDir(context.Background(), filepath.Join(root, "alice", "nested")); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := recorder.MovePath(context.Background(), fromPath, toPath, false); err != nil {
		t.Fatalf("MovePath: %v", err)
	}
	if err := recorder.CopyPath(context.Background(), toPath, filepath.Join(root, "alice", "copy.txt"), false); err != nil {
		t.Fatalf("CopyPath: %v", err)
	}
	if err := recorder.RemovePath(context.Background(), toPath, false); err != nil {
		t.Fatalf("RemovePath: %v", err)
	}
	if len(repo.events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(repo.events))
	}
	if repo.events[0].Op != replication.OpEnsureDir || repo.events[0].Path == nil || *repo.events[0].Path != "/alice/nested" {
		t.Fatalf("unexpected ensure_dir event: %#v", repo.events[0])
	}
	if repo.events[1].Op != replication.OpMovePath || repo.events[1].FromPath == nil || *repo.events[1].FromPath != "/alice/src.txt" || repo.events[1].ToPath == nil || *repo.events[1].ToPath != "/alice/nested/dst.txt" {
		t.Fatalf("unexpected move event: %#v", repo.events[1])
	}
	if repo.events[2].Op != replication.OpCopyPath {
		t.Fatalf("unexpected copy event: %#v", repo.events[2])
	}
	if repo.events[3].Op != replication.OpRemovePath || repo.events[3].Path == nil || *repo.events[3].Path != "/alice/nested/dst.txt" {
		t.Fatalf("unexpected remove event: %#v", repo.events[3])
	}
}

func TestMutationRecorderRejectsPathOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("x"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	repo := &fakeReplicationOutboxRepository{}
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-a"
	cfg.Node.Role = "active"
	cfg.Replication.Enabled = true
	cfg.WebDAV.Directory = root

	recorder := NewMutationRecorder(cfg, repo, fakeMutationRecorderResolver{
		peer: &ResolvedReplicationPeer{
			NodeID:               "node-b",
			AssignmentGeneration: int64Pointer(11),
		},
	}, zap.NewNop())
	if err := recorder.UpsertFile(context.Background(), outside); err == nil {
		t.Fatalf("expected error for path outside root")
	}
	if len(repo.events) != 0 {
		t.Fatalf("expected no events, got %d", len(repo.events))
	}
}
