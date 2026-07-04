package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"go.uber.org/zap"
)

func TestParseDAVSharePath(t *testing.T) {
	t.Parallel()

	handler := newShareDAVTestHandler()

	shareID, relPath, davPrefix, err := handler.parseDAVSharePath("/dav/share/share-1/nested/file.txt")
	if err != nil {
		t.Fatalf("parseDAVSharePath: %v", err)
	}
	if shareID != "share-1" {
		t.Fatalf("expected share-1, got %q", shareID)
	}
	if relPath != "nested/file.txt" {
		t.Fatalf("expected nested/file.txt, got %q", relPath)
	}
	if davPrefix != "/dav/share/share-1" {
		t.Fatalf("expected /dav/share/share-1, got %q", davPrefix)
	}
}

func TestEnsureSameDAVShareDestinationRejectsCrossShareMove(t *testing.T) {
	t.Parallel()

	handler := newShareDAVTestHandler()
	req := httptest.NewRequest("MOVE", "/dav/share/share-1/source.txt", nil)
	req.Header.Set("Destination", "/dav/share/share-2/target.txt")

	if err := handler.ensureSameDAVShareDestination(req, "share-1"); err == nil {
		t.Fatal("expected cross-share destination to be rejected")
	}
}

func newShareDAVTestHandler() *ShareUserHandler {
	cfg := config.DefaultConfig()
	cfg.WebDAV.Prefix = "/dav"
	return NewShareUserHandler(
		service.NewShareUserService(nil, nil, nil, nil, cfg, zap.NewNop()),
		nil,
		nil,
		zap.NewNop(),
	)
}
