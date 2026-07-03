package atomicfile

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAllOverwritesOnCommit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "docs", "note.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte("old-data"), 0o644); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	if err := WriteAll(target, bytes.NewReader([]byte("new-data")), 0o644); err != nil {
		t.Fatalf("WriteAll: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != "new-data" {
		t.Fatalf("expected new-data, got %q", string(got))
	}
}

func TestOpenKeepsOldContentUntilClose(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "docs", "note.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte("old-data"), 0o644); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	f, err := Open(target, 0o644)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Abort()

	if _, err := f.Write([]byte("new-data")); err != nil {
		t.Fatalf("write: %v", err)
	}

	beforeClose, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read before close: %v", err)
	}
	if string(beforeClose) != "old-data" {
		t.Fatalf("expected old-data before close, got %q", string(beforeClose))
	}

	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	afterClose, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read after close: %v", err)
	}
	if string(afterClose) != "new-data" {
		t.Fatalf("expected new-data after close, got %q", string(afterClose))
	}
}

func TestOpenSupportsReadBeforeClose(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "docs", "roundtrip.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	f, err := Open(target, 0o644)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Abort()

	if _, err := f.Write([]byte("roundtrip")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek: %v", err)
	}
	content, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(content) != "roundtrip" {
		t.Fatalf("expected roundtrip, got %q", string(content))
	}
}

func TestAbortRemovesTempAndSkipsCommit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "docs", "abort.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	f, err := Open(target, 0o644)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := f.Write([]byte("payload")); err != nil {
		t.Fatalf("write: %v", err)
	}
	tempPath := f.tempPath
	f.Abort()

	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Fatalf("expected temp removed, got err=%v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected target absent, got err=%v", err)
	}
}
