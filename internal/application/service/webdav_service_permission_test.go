package service

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/yeying-community/warehouse/internal/domain/permission"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
)

func TestResolvePermissionOperationPut(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	service := &WebDAVService{
		config: &config.Config{
			WebDAV: config.WebDAVConfig{
				Prefix:    "/dav",
				Directory: baseDir,
			},
		},
	}
	u := &user.User{
		Username:  "alice",
		Directory: "alice",
	}

	userDir := filepath.Join(baseDir, "alice")
	if err := os.MkdirAll(filepath.Join(userDir, "personal", "packages"), 0755); err != nil {
		t.Fatalf("mkdir user dir: %v", err)
	}

	t.Run("create when target does not exist", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/dav/personal/packages/new.tar.gz", nil)
		got := service.resolvePermissionOperation(u, req)
		if got != permission.OperationCreate {
			t.Fatalf("resolvePermissionOperation() = %s, want %s", got, permission.OperationCreate)
		}
	})

	t.Run("write when target already exists", func(t *testing.T) {
		target := filepath.Join(userDir, "personal", "packages", "exists.tar.gz")
		if err := os.WriteFile(target, []byte("hello"), 0644); err != nil {
			t.Fatalf("write target: %v", err)
		}

		req := httptest.NewRequest(http.MethodPut, "/dav/personal/packages/exists.tar.gz", nil)
		got := service.resolvePermissionOperation(u, req)
		if got != permission.OperationWrite {
			t.Fatalf("resolvePermissionOperation() = %s, want %s", got, permission.OperationWrite)
		}
	})
}

