package handler

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicallyOverwritesOnCommit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "shared", "note.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte("old-data"), 0o644); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	if err := writeFileAtomically(target, bytes.NewReader([]byte("new-data")), 0o644); err != nil {
		t.Fatalf("writeFileAtomically: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != "new-data" {
		t.Fatalf("expected new-data, got %q", string(got))
	}
}

func TestWriteFileAtomicallyDoesNotLeaveVisibleTempFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "shared", "upload.bin")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := writeFileAtomically(target, bytes.NewReader([]byte("payload")), 0o644); err != nil {
		t.Fatalf("writeFileAtomically: %v", err)
	}

	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one visible entry, got %d", len(entries))
	}
	if entries[0].Name() != "upload.bin" {
		t.Fatalf("expected committed filename, got %q", entries[0].Name())
	}
}
