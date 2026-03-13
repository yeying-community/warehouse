package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/cluster"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

type fakeHandlerAssignmentRepository struct {
	standbyAssignments map[string]*cluster.ReplicationAssignment
	pairAssignments    map[string]*cluster.ReplicationAssignment
	updateCalls        []*cluster.ReplicationAssignment
}

func (r *fakeHandlerAssignmentRepository) List(context.Context, repository.ClusterReplicationAssignmentFilter) ([]*cluster.ReplicationAssignment, error) {
	return nil, nil
}

func (r *fakeHandlerAssignmentRepository) ListEffectiveByActive(context.Context, string) ([]*cluster.ReplicationAssignment, error) {
	return nil, nil
}

func (r *fakeHandlerAssignmentRepository) GetEffectiveByStandby(_ context.Context, standbyNodeID string) (*cluster.ReplicationAssignment, error) {
	assignment := r.standbyAssignments[standbyNodeID]
	if assignment == nil {
		return nil, nil
	}
	copied := *assignment
	return &copied, nil
}

func (r *fakeHandlerAssignmentRepository) GetByPair(_ context.Context, activeNodeID, standbyNodeID string) (*cluster.ReplicationAssignment, error) {
	assignment := r.pairAssignments[activeNodeID+"->"+standbyNodeID]
	if assignment == nil {
		return nil, nil
	}
	copied := *assignment
	return &copied, nil
}

func (r *fakeHandlerAssignmentRepository) UpsertLease(context.Context, *cluster.ReplicationAssignment) error {
	return nil
}

func (r *fakeHandlerAssignmentRepository) UpdateState(_ context.Context, assignment *cluster.ReplicationAssignment) error {
	if assignment == nil {
		return nil
	}
	copied := *assignment
	if existing := r.pairAssignments[copied.ActiveNodeID+"->"+copied.StandbyNodeID]; existing != nil {
		if cluster.AdvancesAssignmentGeneration(existing.State, copied.State) {
			copied.Generation = existing.Generation + 1
		}
	}
	r.updateCalls = append(r.updateCalls, &copied)
	if r.pairAssignments == nil {
		r.pairAssignments = make(map[string]*cluster.ReplicationAssignment)
	}
	r.pairAssignments[copied.ActiveNodeID+"->"+copied.StandbyNodeID] = &copied
	if strings.TrimSpace(copied.StandbyNodeID) != "" {
		if r.standbyAssignments == nil {
			r.standbyAssignments = make(map[string]*cluster.ReplicationAssignment)
		}
		r.standbyAssignments[copied.StandbyNodeID] = &copied
	}
	return nil
}

func (r *fakeHandlerAssignmentRepository) ReleaseByActiveExcept(context.Context, string, []string) error {
	return nil
}

func TestInternalReplicationHandleFSApplyRejectsUnassignedSourceNode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-b"
	cfg.Node.Role = "standby"
	cfg.Internal.Replication.Enabled = true
	cfg.WebDAV.Directory = t.TempDir()

	assignments := &fakeHandlerAssignmentRepository{
		standbyAssignments: map[string]*cluster.ReplicationAssignment{
			"node-b": {
				ActiveNodeID:   "node-a",
				StandbyNodeID:  "node-b",
				State:          cluster.AssignmentStateReplicating,
				LeaseExpiresAt: timePointer(time.Now().UTC().Add(time.Minute)),
			},
		},
	}

	handler := NewInternalReplicationHandler(cfg, zap.NewNop(), nil, newMemoryReplicationOffsetStore(), nil, nil, nil, assignments)
	body := bytes.NewBufferString(`{"outboxId":1,"op":"ensure_dir","path":"/alice/docs","isDir":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/replication/fs/apply", body)
	req.Header.Set(middleware.InternalNodeIDHeader, "node-x")
	recorder := httptest.NewRecorder()

	handler.HandleFSApply(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestInternalReplicationHandleBootstrapMarkRejectsExpiredAssignment(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-b"
	cfg.Node.Role = "standby"
	cfg.Internal.Replication.Enabled = true
	cfg.WebDAV.Directory = t.TempDir()

	assignments := &fakeHandlerAssignmentRepository{
		standbyAssignments: map[string]*cluster.ReplicationAssignment{
			"node-b": {
				ActiveNodeID:   "node-a",
				StandbyNodeID:  "node-b",
				State:          cluster.AssignmentStateReplicating,
				LeaseExpiresAt: timePointer(time.Now().UTC().Add(-time.Minute)),
			},
		},
	}

	handler := NewInternalReplicationHandler(cfg, zap.NewNop(), nil, newMemoryReplicationOffsetStore(), nil, nil, nil, assignments)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/replication/bootstrap/mark", bytes.NewBufferString(`{}`))
	req.Header.Set(middleware.InternalNodeIDHeader, "node-a")
	recorder := httptest.NewRecorder()

	handler.HandleBootstrapMark(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestInternalReplicationHandleReconcileApplyBatchRejectsUnassignedSourceNode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-b"
	cfg.Node.Role = "standby"
	cfg.Internal.Replication.Enabled = true
	cfg.WebDAV.Directory = t.TempDir()

	assignments := &fakeHandlerAssignmentRepository{
		standbyAssignments: map[string]*cluster.ReplicationAssignment{
			"node-b": {
				ActiveNodeID:   "node-a",
				StandbyNodeID:  "node-b",
				State:          cluster.AssignmentStateReplicating,
				LeaseExpiresAt: timePointer(time.Now().UTC().Add(time.Minute)),
			},
		},
	}
	handler := NewInternalReplicationHandler(cfg, zap.NewNop(), nil, newMemoryReplicationOffsetStore(), nil, nil, nil, assignments)

	modifiedAt := time.Date(2025, 3, 1, 12, 30, 45, 0, time.UTC)
	payload := internalReconcileApplyBatchRequest{
		JobID: 1,
		Items: []internalReconcileApplyItemRequest{
			{
				ItemID:        10,
				Path:          "/history/a.txt",
				IsDir:         false,
				ModifiedAt:    &modifiedAt,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("hello")),
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/replication/reconcile/apply-batch", bytes.NewReader(body))
	req.Header.Set(middleware.InternalNodeIDHeader, "node-x")
	recorder := httptest.NewRecorder()
	handler.HandleReconcileApplyBatch(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func timePointer(value time.Time) *time.Time {
	v := value
	return &v
}
