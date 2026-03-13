package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/cluster"
	"github.com/yeying-community/warehouse/internal/domain/replication"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

type memoryReplicationOffsetStore struct {
	offsets map[string]*replication.Offset
	upserts int
}

func newMemoryReplicationOffsetStore() *memoryReplicationOffsetStore {
	return &memoryReplicationOffsetStore{offsets: make(map[string]*replication.Offset)}
}

func (s *memoryReplicationOffsetStore) Get(_ context.Context, sourceNodeID, targetNodeID string) (*replication.Offset, error) {
	if offset, ok := s.offsets[sourceNodeID+"->"+targetNodeID]; ok {
		copied := *offset
		return &copied, nil
	}
	return nil, replication.ErrOffsetNotFound
}

func (s *memoryReplicationOffsetStore) Upsert(_ context.Context, offset *replication.Offset) error {
	copied := *offset
	s.offsets[offset.SourceNodeID+"->"+offset.TargetNodeID] = &copied
	s.upserts++
	return nil
}

func TestInternalReplicationHandleFSApplyEnsureDir(t *testing.T) {
	root := t.TempDir()
	offsets := newMemoryReplicationOffsetStore()
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-b"
	cfg.Node.Role = "standby"
	cfg.Internal.Replication.Enabled = true
	cfg.Internal.Replication.PeerNodeID = "node-a"
	cfg.WebDAV.Directory = root

	handler := NewInternalReplicationHandler(cfg, zap.NewNop(), nil, offsets, nil, nil, nil)
	body := bytes.NewBufferString(`{"outboxId":1,"op":"ensure_dir","path":"/alice/docs","isDir":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/replication/fs/apply", body)
	req.Header.Set(middleware.InternalNodeIDHeader, "node-a")
	req.Header.Set(middleware.InternalAssignmentGenerationHeader, "1")
	recorder := httptest.NewRecorder()

	handler.HandleFSApply(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "alice", "docs")); err != nil {
		t.Fatalf("expected directory to exist: %v", err)
	}
	offset, err := offsets.Get(context.Background(), "node-a", "node-b")
	if err != nil {
		t.Fatalf("expected offset to exist: %v", err)
	}
	if offset.LastAppliedOutboxID != 1 {
		t.Fatalf("unexpected last applied outbox id: %d", offset.LastAppliedOutboxID)
	}
}

func TestInternalReplicationHandleFSApplyRejectsSequenceGap(t *testing.T) {
	root := t.TempDir()
	offsets := newMemoryReplicationOffsetStore()
	offsets.offsets["node-a->node-b"] = &replication.Offset{
		SourceNodeID:        "node-a",
		TargetNodeID:        "node-b",
		LastAppliedOutboxID: 1,
		LastAppliedAt:       time.Now(),
		UpdatedAt:           time.Now(),
	}
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-b"
	cfg.Node.Role = "standby"
	cfg.Internal.Replication.Enabled = true
	cfg.Internal.Replication.PeerNodeID = "node-a"
	cfg.WebDAV.Directory = root

	handler := NewInternalReplicationHandler(cfg, zap.NewNop(), nil, offsets, nil, nil, nil)
	body := bytes.NewBufferString(`{"outboxId":3,"op":"ensure_dir","path":"/alice/docs","isDir":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/replication/fs/apply", body)
	req.Header.Set(middleware.InternalNodeIDHeader, "node-a")
	req.Header.Set(middleware.InternalAssignmentGenerationHeader, "1")
	recorder := httptest.NewRecorder()

	handler.HandleFSApply(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got := int64(resp["expectedNextOutboxId"].(float64)); got != 2 {
		t.Fatalf("unexpected expectedNextOutboxId: %v", resp["expectedNextOutboxId"])
	}
}

func TestInternalReplicationHandleFileApplyWritesFileAndUpdatesOffset(t *testing.T) {
	root := t.TempDir()
	offsets := newMemoryReplicationOffsetStore()
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-b"
	cfg.Node.Role = "standby"
	cfg.Internal.Replication.Enabled = true
	cfg.Internal.Replication.PeerNodeID = "node-a"
	cfg.WebDAV.Directory = root

	handler := NewInternalReplicationHandler(cfg, zap.NewNop(), nil, offsets, nil, nil, nil)
	payload := []byte("replicated payload")
	digest := sha256.Sum256(payload)
	hashHex := hex.EncodeToString(digest[:])
	req := httptest.NewRequest(http.MethodPut, "/api/v1/internal/replication/file?outboxId=1&path=/alice/file.txt&fileSize=18", bytes.NewReader(payload))
	req.Header.Set(middleware.InternalNodeIDHeader, "node-a")
	req.Header.Set(middleware.InternalContentSHA256Header, hashHex)
	req.Header.Set(middleware.InternalAssignmentGenerationHeader, "1")
	recorder := httptest.NewRecorder()

	handler.HandleFileApply(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	content, err := os.ReadFile(filepath.Join(root, "alice", "file.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != string(payload) {
		t.Fatalf("unexpected file contents: %q", string(content))
	}
	offset, err := offsets.Get(context.Background(), "node-a", "node-b")
	if err != nil {
		t.Fatalf("expected offset to exist: %v", err)
	}
	if offset.LastAppliedOutboxID != 1 {
		t.Fatalf("unexpected last applied outbox id: %d", offset.LastAppliedOutboxID)
	}
}

func TestInternalReplicationHandleFileApplyAlreadyApplied(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "alice", "file.txt")
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	payload := []byte("replicated payload")
	if err := os.WriteFile(filePath, payload, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	offsets := newMemoryReplicationOffsetStore()
	offsets.offsets["node-a->node-b"] = &replication.Offset{
		SourceNodeID:        "node-a",
		TargetNodeID:        "node-b",
		LastAppliedOutboxID: 4,
		LastAppliedAt:       time.Now(),
		UpdatedAt:           time.Now(),
	}
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-b"
	cfg.Node.Role = "standby"
	cfg.Internal.Replication.Enabled = true
	cfg.Internal.Replication.PeerNodeID = "node-a"
	cfg.WebDAV.Directory = root

	handler := NewInternalReplicationHandler(cfg, zap.NewNop(), nil, offsets, nil, nil, nil)
	digest := sha256.Sum256(payload)
	hashHex := hex.EncodeToString(digest[:])
	req := httptest.NewRequest(http.MethodPut, "/api/v1/internal/replication/file?outboxId=4&path=/alice/file.txt&fileSize=18", bytes.NewReader(payload))
	req.Header.Set(middleware.InternalNodeIDHeader, "node-a")
	req.Header.Set(middleware.InternalContentSHA256Header, hashHex)
	req.Header.Set(middleware.InternalAssignmentGenerationHeader, "1")
	recorder := httptest.NewRecorder()

	handler.HandleFileApply(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var resp internalReplicationApplyResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.AlreadyApplied {
		t.Fatalf("expected alreadyApplied response: %#v", resp)
	}
	if offsets.upserts != 0 {
		t.Fatalf("expected no offset updates, got %d", offsets.upserts)
	}
}

func TestInternalReplicationHandleFSApplyRejectsUninitializedGenerationFence(t *testing.T) {
	root := t.TempDir()
	offsetGeneration := int64(1)
	offsets := newMemoryReplicationOffsetStore()
	offsets.offsets["node-a->node-b"] = &replication.Offset{
		SourceNodeID:         "node-a",
		TargetNodeID:         "node-b",
		AssignmentGeneration: &offsetGeneration,
		LastAppliedOutboxID:  9,
		LastAppliedAt:        time.Now(),
		UpdatedAt:            time.Now(),
	}
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-b"
	cfg.Node.Role = "standby"
	cfg.Internal.Replication.Enabled = true
	cfg.WebDAV.Directory = root

	assignmentGeneration := int64(2)
	assignments := &fakeHandlerAssignmentRepository{
		standbyAssignments: map[string]*cluster.ReplicationAssignment{
			"node-b": {
				ActiveNodeID:   "node-a",
				StandbyNodeID:  "node-b",
				State:          cluster.AssignmentStateReplicating,
				Generation:     assignmentGeneration,
				LeaseExpiresAt: timePointer(time.Now().UTC().Add(time.Minute)),
			},
		},
	}

	handler := NewInternalReplicationHandler(cfg, zap.NewNop(), nil, offsets, nil, nil, nil, assignments)
	body := bytes.NewBufferString(`{"outboxId":10,"op":"ensure_dir","path":"/alice/docs","isDir":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/replication/fs/apply", body)
	req.Header.Set(middleware.InternalNodeIDHeader, "node-a")
	req.Header.Set(middleware.InternalAssignmentGenerationHeader, "2")
	recorder := httptest.NewRecorder()

	handler.HandleFSApply(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got := int64(resp["lastAppliedOutboxId"].(float64)); got != 9 {
		t.Fatalf("unexpected lastAppliedOutboxId: %v", resp["lastAppliedOutboxId"])
	}
}
