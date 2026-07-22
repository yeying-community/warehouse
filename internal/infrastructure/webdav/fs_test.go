package webdavfs

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	xwebdav "golang.org/x/net/webdav"
)

func TestVirtualFileIsVisibleReadableAndReadOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "personal"), 0o755); err != nil {
		t.Fatalf("mkdir personal: %v", err)
	}

	fsys := NewUnicodeFileSystemWithVirtualFiles(root, []VirtualFile{{
		Path:    "/personal/Warehouse 用户使用指南.md",
		Content: []byte("guide content"),
		ModTime: time.Unix(10, 0).UTC(),
	}})

	info, err := fsys.Stat(ctx, "/personal/Warehouse 用户使用指南.md")
	if err != nil {
		t.Fatalf("stat virtual file: %v", err)
	}
	if info.Name() != "Warehouse 用户使用指南.md" || info.Size() != int64(len("guide content")) {
		t.Fatalf("unexpected virtual file info: name=%q size=%d", info.Name(), info.Size())
	}

	dir, err := fsys.OpenFile(ctx, "/personal", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("open personal dir: %v", err)
	}
	entries, err := dir.Readdir(0)
	if err != nil {
		t.Fatalf("readdir personal dir: %v", err)
	}
	if !hasFileInfo(entries, "Warehouse 用户使用指南.md") {
		t.Fatalf("expected virtual guide in directory listing, got %#v", entries)
	}

	file, err := fsys.OpenFile(ctx, "/personal/Warehouse 用户使用指南.md", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("open virtual file: %v", err)
	}
	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read virtual file: %v", err)
	}
	if string(content) != "guide content" {
		t.Fatalf("unexpected virtual content: %q", string(content))
	}
	if _, err := file.Write([]byte("x")); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("expected write to virtual file denied, got %v", err)
	}

	if _, err := fsys.OpenFile(ctx, "/personal/Warehouse 用户使用指南.md", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("expected open for write denied, got %v", err)
	}
}

func TestVirtualFileAppearsInPropfind(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "personal"), 0o755); err != nil {
		t.Fatalf("mkdir personal: %v", err)
	}

	handler := &xwebdav.Handler{
		Prefix: "/dav",
		FileSystem: NewUnicodeFileSystemWithVirtualFiles(root, []VirtualFile{{
			Path:    "/personal/Warehouse 用户使用指南.md",
			Content: []byte("guide content"),
		}}),
		LockSystem: xwebdav.NewMemLS(),
	}
	body := strings.NewReader(`<?xml version="1.0" encoding="utf-8"?><D:propfind xmlns:D="DAV:"><D:allprop/></D:propfind>`)
	req := httptest.NewRequest("PROPFIND", "/dav/personal/", body)
	req.Header.Set("Depth", "1")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != xwebdav.StatusMulti {
		t.Fatalf("expected 207 Multi-Status, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Warehouse 用户使用指南.md") {
		t.Fatalf("expected guide name in PROPFIND response, got %s", rec.Body.String())
	}
}

func TestVirtualFileDoesNotMaskRealFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	target := filepath.Join(root, "personal", "Warehouse 用户使用指南.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir personal: %v", err)
	}
	if err := os.WriteFile(target, []byte("real content"), 0o644); err != nil {
		t.Fatalf("write real guide: %v", err)
	}

	fsys := NewUnicodeFileSystemWithVirtualFiles(root, []VirtualFile{{
		Path:    "/personal/Warehouse 用户使用指南.md",
		Content: []byte("virtual content"),
	}})

	file, err := fsys.OpenFile(ctx, "/personal/Warehouse 用户使用指南.md", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("open real file: %v", err)
	}
	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read real file: %v", err)
	}
	if string(content) != "real content" {
		t.Fatalf("expected real file content, got %q", string(content))
	}

	dir, err := fsys.OpenFile(ctx, "/personal", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("open personal dir: %v", err)
	}
	entries, err := dir.Readdir(0)
	if err != nil {
		t.Fatalf("readdir personal dir: %v", err)
	}
	count := 0
	for _, entry := range entries {
		if entry.Name() == "Warehouse 用户使用指南.md" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one guide entry, got %d", count)
	}
}

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

func hasFileInfo(entries []os.FileInfo, name string) bool {
	for _, entry := range entries {
		if entry.Name() == name {
			return true
		}
	}
	return false
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

	tempPath := af.File.TempPath()
	if err := af.File.File.Close(); err != nil {
		t.Fatalf("close underlying file: %v", err)
	}
	if err := af.Close(); err == nil {
		t.Fatal("expected close to fail after underlying file was closed")
	}

	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
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
