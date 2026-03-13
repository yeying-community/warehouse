package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/replication"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

type fakeWorkerOutboxRepository struct {
	mu               sync.Mutex
	events           []*replication.OutboxEvent
	listSourceNodeID string
	listTargetNodeID string
	listGeneration   *int64
	listLimit        int
	dispatched       []int64
	failed           []failedEvent
}

type fakeWorkerResolver struct {
	peer *ResolvedReplicationPeer
}

type failedEvent struct {
	id          int64
	lastError   string
	nextRetryAt time.Time
}

func (r *fakeWorkerOutboxRepository) Append(context.Context, *replication.OutboxEvent) error {
	return nil
}

func (r *fakeWorkerOutboxRepository) ListPending(_ context.Context, sourceNodeID, targetNodeID string, assignmentGeneration *int64, limit int) ([]*replication.OutboxEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listSourceNodeID = sourceNodeID
	r.listTargetNodeID = targetNodeID
	r.listGeneration = assignmentGeneration
	r.listLimit = limit
	items := make([]*replication.OutboxEvent, len(r.events))
	for i, event := range r.events {
		copied := *event
		items[i] = &copied
	}
	return items, nil
}

func (r *fakeWorkerOutboxRepository) MarkDispatched(_ context.Context, id int64, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dispatched = append(r.dispatched, id)
	return nil
}

func (r *fakeWorkerOutboxRepository) MarkFailed(_ context.Context, id int64, lastError string, nextRetryAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed = append(r.failed, failedEvent{id: id, lastError: lastError, nextRetryAt: nextRetryAt})
	return nil
}

func (r *fakeWorkerOutboxRepository) GetStatusSummary(context.Context, string, string) (*replication.OutboxStatus, error) {
	return nil, nil
}

func (r fakeWorkerResolver) ResolveTarget(context.Context) (*ResolvedReplicationPeer, error) {
	return r.peer, nil
}

func (r fakeWorkerResolver) ResolveDispatchTarget(context.Context) (*ResolvedReplicationPeer, error) {
	return r.peer, nil
}

func (r fakeWorkerResolver) ResolveByNodeID(context.Context, string, bool) (*ResolvedReplicationPeer, error) {
	return r.peer, nil
}

type fsApplyRequest struct {
	OutboxID int64  `json:"outboxId"`
	Op       string `json:"op"`
	Path     string `json:"path,omitempty"`
	FromPath string `json:"fromPath,omitempty"`
	ToPath   string `json:"toPath,omitempty"`
	IsDir    bool   `json:"isDir"`
}

func TestReplicationWorkerDispatchOnce(t *testing.T) {
	activeRoot := t.TempDir()
	standbyRoot := t.TempDir()
	activeFile := filepath.Join(activeRoot, "alice", "docs", "hello.txt")
	if err := os.MkdirAll(filepath.Dir(activeFile), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	payload := []byte("replicated payload")
	if err := os.WriteFile(activeFile, payload, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	digest := sha256.Sum256(payload)
	hashHex := hex.EncodeToString(digest[:])

	standbyCfg := config.DefaultConfig()
	standbyCfg.Node.ID = "node-b"
	standbyCfg.Node.Role = "standby"
	standbyCfg.Internal.Replication.Enabled = true
	standbyCfg.Internal.Replication.PeerNodeID = "node-a"
	standbyCfg.Internal.Replication.SharedSecret = "shared-secret"
	standbyCfg.Internal.Replication.AllowedClockSkew = time.Minute
	standbyCfg.WebDAV.Directory = standbyRoot
	server := newStandbyTestServer(t, standbyCfg)
	defer server.Close()
	generation := int64(3)

	outbox := &fakeWorkerOutboxRepository{
		events: []*replication.OutboxEvent{
			{
				ID:                   1,
				SourceNodeID:         "node-a",
				TargetNodeID:         "node-b",
				AssignmentGeneration: &generation,
				Op:                   replication.OpEnsureDir,
				Path:                 stringPointer("/alice/docs"),
				IsDir:                true,
			},
			{
				ID:                   2,
				SourceNodeID:         "node-a",
				TargetNodeID:         "node-b",
				AssignmentGeneration: &generation,
				Op:                   replication.OpUpsertFile,
				Path:                 stringPointer("/alice/docs/hello.txt"),
				ContentSHA256:        stringPointer(hashHex),
				FileSize:             int64Pointer(int64(len(payload))),
				IsDir:                false,
			},
		},
	}

	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-a"
	cfg.Node.Role = "active"
	cfg.Internal.Replication.Enabled = true
	cfg.Internal.Replication.PeerNodeID = "node-b"
	cfg.Internal.Replication.PeerBaseURL = server.URL
	cfg.Internal.Replication.SharedSecret = "shared-secret"
	cfg.Internal.Replication.AllowedClockSkew = time.Minute
	cfg.Internal.Replication.DispatchInterval = time.Second
	cfg.Internal.Replication.RequestTimeout = 5 * time.Second
	cfg.Internal.Replication.BatchSize = 10
	cfg.Internal.Replication.RetryBackoffBase = time.Second
	cfg.Internal.Replication.MaxRetryBackoff = time.Minute
	cfg.WebDAV.Directory = activeRoot

	worker := NewReplicationWorker(cfg, outbox, fakeWorkerResolver{
		peer: &ResolvedReplicationPeer{
			NodeID:               "node-b",
			BaseURL:              server.URL,
			AssignmentGeneration: &generation,
		},
	}, zap.NewNop())

	if err := worker.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if outbox.listSourceNodeID != "node-a" || outbox.listTargetNodeID != "node-b" || outbox.listLimit != 10 {
		t.Fatalf("unexpected list args: %#v", outbox)
	}
	if outbox.listGeneration == nil || *outbox.listGeneration != generation {
		t.Fatalf("unexpected list generation: %#v", outbox.listGeneration)
	}
	if len(outbox.dispatched) != 2 || outbox.dispatched[0] != 1 || outbox.dispatched[1] != 2 {
		t.Fatalf("unexpected dispatched ids: %#v", outbox.dispatched)
	}
	if len(outbox.failed) != 0 {
		t.Fatalf("expected no failed events, got %#v", outbox.failed)
	}
	content, err := os.ReadFile(filepath.Join(standbyRoot, "alice", "docs", "hello.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != string(payload) {
		t.Fatalf("unexpected replicated file contents: %q", string(content))
	}
}

func TestReplicationWorkerMarksFailedAndStopsBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()
	generation := int64(5)

	outbox := &fakeWorkerOutboxRepository{
		events: []*replication.OutboxEvent{
			{ID: 1, SourceNodeID: "node-a", TargetNodeID: "node-b", AssignmentGeneration: &generation, Op: replication.OpEnsureDir, Path: stringPointer("/alice/docs"), IsDir: true, AttemptCount: 1},
			{ID: 2, SourceNodeID: "node-a", TargetNodeID: "node-b", AssignmentGeneration: &generation, Op: replication.OpEnsureDir, Path: stringPointer("/alice/skip"), IsDir: true},
		},
	}

	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-a"
	cfg.Node.Role = "active"
	cfg.Internal.Replication.Enabled = true
	cfg.Internal.Replication.PeerNodeID = "node-b"
	cfg.Internal.Replication.PeerBaseURL = server.URL
	cfg.Internal.Replication.SharedSecret = "shared-secret"
	cfg.Internal.Replication.AllowedClockSkew = time.Minute
	cfg.Internal.Replication.DispatchInterval = time.Second
	cfg.Internal.Replication.RequestTimeout = 5 * time.Second
	cfg.Internal.Replication.BatchSize = 10
	cfg.Internal.Replication.RetryBackoffBase = time.Second
	cfg.Internal.Replication.MaxRetryBackoff = time.Minute
	cfg.WebDAV.Directory = t.TempDir()

	worker := NewReplicationWorker(cfg, outbox, fakeWorkerResolver{
		peer: &ResolvedReplicationPeer{
			NodeID:               "node-b",
			BaseURL:              server.URL,
			AssignmentGeneration: &generation,
		},
	}, zap.NewNop())
	now := time.Date(2026, 3, 8, 13, 0, 0, 0, time.UTC)
	worker.now = func() time.Time { return now }

	if err := worker.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if len(outbox.dispatched) != 0 {
		t.Fatalf("expected no dispatched events, got %#v", outbox.dispatched)
	}
	if len(outbox.failed) != 1 {
		t.Fatalf("expected one failed event, got %#v", outbox.failed)
	}
	if outbox.failed[0].id != 1 {
		t.Fatalf("unexpected failed id: %#v", outbox.failed[0])
	}
	expectedRetryAt := now.Add(2 * time.Second)
	if !outbox.failed[0].nextRetryAt.Equal(expectedRetryAt) {
		t.Fatalf("unexpected nextRetryAt: got=%v want=%v", outbox.failed[0].nextRetryAt, expectedRetryAt)
	}
	if outbox.failed[0].lastError == "" {
		t.Fatalf("expected failure reason to be captured")
	}
}

func TestReplicationWorkerRetryDelayCapsAtMax(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-a"
	cfg.Node.Role = "active"
	cfg.Internal.Replication.Enabled = true
	cfg.Internal.Replication.PeerNodeID = "node-b"
	cfg.Internal.Replication.PeerBaseURL = "http://example.internal"
	cfg.Internal.Replication.SharedSecret = "shared-secret"
	cfg.Internal.Replication.AllowedClockSkew = time.Minute
	cfg.Internal.Replication.DispatchInterval = time.Second
	cfg.Internal.Replication.RequestTimeout = 5 * time.Second
	cfg.Internal.Replication.BatchSize = 10
	cfg.Internal.Replication.RetryBackoffBase = time.Second
	cfg.Internal.Replication.MaxRetryBackoff = 8 * time.Second

	worker := NewReplicationWorker(cfg, &fakeWorkerOutboxRepository{}, fakeWorkerResolver{
		peer: &ResolvedReplicationPeer{
			NodeID:               "node-b",
			BaseURL:              "http://example.internal",
			AssignmentGeneration: int64Pointer(1),
		},
	}, zap.NewNop())
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: time.Second},
		{attempt: 1, want: 2 * time.Second},
		{attempt: 2, want: 4 * time.Second},
		{attempt: 3, want: 8 * time.Second},
		{attempt: 4, want: 8 * time.Second},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("attempt_%d", tc.attempt), func(t *testing.T) {
			if got := worker.retryDelay(tc.attempt); got != tc.want {
				t.Fatalf("retryDelay(%d) = %v, want %v", tc.attempt, got, tc.want)
			}
		})
	}
}

func newStandbyTestServer(t *testing.T, cfg *config.Config) *httptest.Server {
	t.Helper()
	internalAuth := middleware.NewInternalAuthMiddleware(cfg.Internal.Replication, zap.NewNop())
	mux := http.NewServeMux()
	mux.Handle("/api/v1/internal/replication/fs/apply", internalAuth.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req fsApplyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Op != replication.OpEnsureDir {
			http.Error(w, "unsupported op", http.StatusBadRequest)
			return
		}
		fullPath, err := resolveStandbyPath(cfg.WebDAV.Directory, req.Path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})))
	mux.Handle("/api/v1/internal/replication/file", internalAuth.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		storagePath := r.URL.Query().Get("path")
		fullPath, err := resolveStandbyPath(cfg.WebDAV.Directory, storagePath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		digest := sha256.Sum256(payload)
		if got, want := hex.EncodeToString(digest[:]), strings.ToLower(r.Header.Get(middleware.InternalContentSHA256Header)); got != want {
			http.Error(w, "hash mismatch", http.StatusBadRequest)
			return
		}
		if err := os.WriteFile(fullPath, payload, 0644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})))
	return httptest.NewServer(mux)
}

func resolveStandbyPath(root, storagePath string) (string, error) {
	cleaned := path.Clean("/" + strings.TrimSpace(storagePath))
	if cleaned == "/" || strings.HasPrefix(cleaned, "/..") {
		return "", fmt.Errorf("invalid storage path %q", storagePath)
	}
	decoded, err := url.QueryUnescape(cleaned)
	if err == nil && decoded != "" {
		cleaned = path.Clean(decoded)
	}
	return filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(cleaned, "/"))), nil
}
