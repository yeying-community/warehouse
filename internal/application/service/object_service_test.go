package service

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestObjectServicePutListOpenDelete(t *testing.T) {
	root := t.TempDir()
	svc := NewObjectService(root)
	ctx := context.Background()

	info, err := svc.Put(ctx, "alice", "personal", "docs/note.txt", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("put object: %v", err)
	}
	if info.Size != 5 || info.ContentType != "text/plain; charset=utf-8" || info.ETag == "" {
		t.Fatalf("unexpected object info: %+v", info)
	}

	listed, err := svc.List(ctx, "alice", "personal", "docs/", '/')
	if err != nil {
		t.Fatalf("list objects: %v", err)
	}
	if len(listed.Objects) != 1 || listed.Objects[0].Key != "docs/note.txt" {
		t.Fatalf("unexpected list result: %+v", listed)
	}

	file, _, err := svc.Open(ctx, "alice", "personal", "docs/note.txt")
	if err != nil {
		t.Fatalf("open object: %v", err)
	}
	content, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil || string(content) != "hello" {
		t.Fatalf("unexpected content: content=%q err=%v", content, err)
	}

	if err := svc.Delete(ctx, "alice", "personal", "docs/note.txt"); err != nil {
		t.Fatalf("delete object: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "alice", "personal", "docs", "note.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected object to be deleted, got err=%v", err)
	}
}

func TestObjectServiceListReturnsPrefixes(t *testing.T) {
	root := t.TempDir()
	svc := NewObjectService(root)
	ctx := context.Background()
	if _, err := svc.Put(ctx, "alice", "personal", "docs/note.txt", strings.NewReader("hello")); err != nil {
		t.Fatalf("put object: %v", err)
	}
	listed, err := svc.List(ctx, "alice", "personal", "", '/')
	if err != nil {
		t.Fatalf("list objects: %v", err)
	}
	if len(listed.Prefixes) != 1 || listed.Prefixes[0] != "docs/" {
		t.Fatalf("unexpected prefixes: %+v", listed.Prefixes)
	}
}
