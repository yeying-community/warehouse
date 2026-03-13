package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"go.uber.org/zap"
)

func TestInternalReplicationHandleReconcileApplyBatchPreservesMtime(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-b"
	cfg.Node.Role = "standby"
	cfg.WebDAV.Directory = root
	cfg.Internal.Replication.Enabled = true

	handler := NewInternalReplicationHandler(cfg, zap.NewNop(), nil, newMemoryReplicationOffsetStore(), nil, nil, nil)
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
	req.Header.Set("X-Warehouse-Node-Id", "node-a")
	req.Header.Set("X-Warehouse-Assignment-Generation", "1")
	recorder := httptest.NewRecorder()
	handler.HandleReconcileApplyBatch(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	fullPath := filepath.Join(root, "history", "a.txt")
	info, err := os.Stat(fullPath)
	if err != nil {
		t.Fatalf("stat applied file: %v", err)
	}
	if !info.ModTime().UTC().Equal(modifiedAt) {
		t.Fatalf("unexpected modtime: got=%s want=%s", info.ModTime().UTC(), modifiedAt)
	}
}
