package webdavfs

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenFileAtomicWriteKeepsOldContentUntilClose(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fsys := NewUnicodeFileSystem(root)
	target := filepath.Join(root, "docs", "note.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte("old-data"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	f, err := fsys.OpenFile(context.Background(), "/docs/note.txt", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatalf("open atomic file: %v", err)
	}
	defer f.Close()

	if _, err := f.Write([]byte("new-data")); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	beforeClose, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target before close: %v", err)
	}
	if string(beforeClose) != "old-data" {
		t.Fatalf("expected old content before close, got %q", string(beforeClose))
	}

	if err := f.Close(); err != nil {
		t.Fatalf("close atomic file: %v", err)
	}

	afterClose, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target after close: %v", err)
	}
	if string(afterClose) != "new-data" {
		t.Fatalf("expected new content after close, got %q", string(afterClose))
	}
}

func TestOpenFileAtomicWriteHidesNewFileUntilClose(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fsys := NewUnicodeFileSystem(root)
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}

	f, err := fsys.OpenFile(context.Background(), "/docs/new.txt", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatalf("open atomic file: %v", err)
	}
	defer f.Close()

	if _, err := f.Write([]byte("payload")); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	if _, err := fsys.Stat(context.Background(), "/docs/new.txt"); !os.IsNotExist(err) {
		t.Fatalf("expected file to stay hidden before close, got err=%v", err)
	}

	entries, err := fsys.ReadDir(context.Background(), "/docs")
	if err != nil {
		t.Fatalf("read dir before close: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no visible entries before close, got %d", len(entries))
	}

	if err := f.Close(); err != nil {
		t.Fatalf("close atomic file: %v", err)
	}

	visible, err := fsys.Stat(context.Background(), "/docs/new.txt")
	if err != nil {
		t.Fatalf("stat after close: %v", err)
	}
	if visible.Size() != int64(len("payload")) {
		t.Fatalf("expected size %d after close, got %d", len("payload"), visible.Size())
	}
}

func TestAtomicWriteFileRemovesTempOnCloseWithoutCommit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fsys := NewUnicodeFileSystem(root)
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}

	f, err := fsys.OpenFile(context.Background(), "/docs/abort.txt", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatalf("open atomic file: %v", err)
	}

	af, ok := f.(*atomicWriteFile)
	if !ok {
		t.Fatalf("expected atomicWriteFile, got %T", f)
	}

	if _, err := af.Write([]byte("payload")); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	if err := af.File.Close(); err != nil {
		t.Fatalf("close underlying file: %v", err)
	}
	if err := af.Close(); err == nil {
		t.Fatal("expected close to fail after underlying file was closed")
	}

	if _, err := os.Stat(af.tempPath); !os.IsNotExist(err) {
		t.Fatalf("expected temp file removed after failure, got err=%v", err)
	}
	if _, err := fsys.Stat(context.Background(), "/docs/abort.txt"); !os.IsNotExist(err) {
		t.Fatalf("expected target file absent after failed close, got err=%v", err)
	}
}

func TestAtomicWriteFileSupportsReadBeforeClose(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fsys := NewUnicodeFileSystem(root)
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}

	f, err := fsys.OpenFile(context.Background(), "/docs/readback.txt", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatalf("open atomic file: %v", err)
	}
	defer f.Close()

	if _, err := f.Write([]byte("roundtrip")); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek temp file: %v", err)
	}
	content, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read temp file: %v", err)
	}
	if string(content) != "roundtrip" {
		t.Fatalf("expected roundtrip content, got %q", string(content))
	}
}
