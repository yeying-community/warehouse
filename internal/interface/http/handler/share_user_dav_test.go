package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestClearShareDAVDeadlines(t *testing.T) {
	t.Parallel()

	handler := newShareDAVTestHandler()
	rec := &deadlineResponseWriter{header: make(http.Header)}

	handler.clearShareDAVDeadlines(rec)

	if !rec.readDeadlineSet || !rec.readDeadline.IsZero() {
		t.Fatalf("expected cleared read deadline, got set=%v deadline=%v", rec.readDeadlineSet, rec.readDeadline)
	}
	if !rec.writeDeadlineSet || !rec.writeDeadline.IsZero() {
		t.Fatalf("expected cleared write deadline, got set=%v deadline=%v", rec.writeDeadlineSet, rec.writeDeadline)
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

type deadlineResponseWriter struct {
	header           http.Header
	readDeadline     time.Time
	writeDeadline    time.Time
	readDeadlineSet  bool
	writeDeadlineSet bool
}

func (w *deadlineResponseWriter) Header() http.Header {
	return w.header
}

func (w *deadlineResponseWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func (w *deadlineResponseWriter) WriteHeader(statusCode int) {
}

func (w *deadlineResponseWriter) SetReadDeadline(deadline time.Time) error {
	w.readDeadline = deadline
	w.readDeadlineSet = true
	return nil
}

func (w *deadlineResponseWriter) SetWriteDeadline(deadline time.Time) error {
	w.writeDeadline = deadline
	w.writeDeadlineSet = true
	return nil
}
