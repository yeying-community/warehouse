package object

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestResolvePath(t *testing.T) {
	root := filepath.Join("/srv", "warehouse")
	got, err := ResolvePath(root, "alice", "personal", "folder/中文.txt")
	if err != nil {
		t.Fatalf("resolve path: %v", err)
	}
	want := filepath.Join(root, "alice", "personal", "folder", "中文.txt")
	if got != want {
		t.Fatalf("unexpected path: got=%q want=%q", got, want)
	}
}

func TestResolvePathRejectsEscape(t *testing.T) {
	tests := []struct {
		name      string
		userDir   string
		bucket    string
		key       string
		wantError error
	}{
		{name: "key traversal", userDir: "alice", bucket: "personal", key: "../../secret", wantError: ErrPathEscape},
		{name: "user traversal", userDir: "../alice", bucket: "personal", key: "file.txt", wantError: ErrPathEscape},
		{name: "unsupported bucket", userDir: "alice", bucket: "other", key: "file.txt", wantError: ErrInvalidBucket},
		{name: "nul key", userDir: "alice", bucket: "personal", key: "bad\x00.txt", wantError: ErrInvalidKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolvePath("/srv/warehouse", tt.userDir, tt.bucket, tt.key)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("unexpected error: got=%v want=%v", err, tt.wantError)
			}
		})
	}
}

func TestResolvePathAllowsEmptyKeyForBucketOperations(t *testing.T) {
	got, err := ResolvePath("/srv/warehouse", "alice", "apps", "")
	if err != nil {
		t.Fatalf("resolve bucket path: %v", err)
	}
	want := filepath.Join("/srv/warehouse", "alice", "apps")
	if got != want {
		t.Fatalf("unexpected path: got=%q want=%q", got, want)
	}
}
