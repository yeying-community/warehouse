package assetspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"go.uber.org/zap"
)

func TestManagerEnsureForUserCreatesDefaultSpaces(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		WebDAV: config.WebDAVConfig{Directory: tmpDir},
		Web3: config.Web3Config{
			UCAN: config.UCANConfig{
				AppScope: config.AppScopeConfig{PathPrefix: "/apps"},
			},
		},
	}

	manager := NewManager(cfg, zap.NewNop())
	u := user.NewUser("alice", "alice")

	if err := manager.EnsureForUser(u); err != nil {
		t.Fatalf("EnsureForUser returned error: %v", err)
	}

	assertDirectoryExists(t, filepath.Join(tmpDir, "alice"))
	assertDirectoryExists(t, filepath.Join(tmpDir, "alice", "personal"))
	assertDirectoryExists(t, filepath.Join(tmpDir, "alice", "apps"))
}

func TestManagerEnsureForUserRespectsCustomAppScopePrefix(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		WebDAV: config.WebDAVConfig{Directory: tmpDir},
		Web3: config.Web3Config{
			UCAN: config.UCANConfig{
				AppScope: config.AppScopeConfig{PathPrefix: "/dapps/storage"},
			},
		},
	}

	manager := NewManager(cfg, zap.NewNop())
	u := user.NewUser("bob", "bob")

	if err := manager.EnsureForUser(u); err != nil {
		t.Fatalf("EnsureForUser returned error: %v", err)
	}

	assertDirectoryExists(t, filepath.Join(tmpDir, "bob", "personal"))
	assertDirectoryExists(t, filepath.Join(tmpDir, "bob", "dapps", "storage"))

	spaces := manager.Spaces()
	if len(spaces) != 2 {
		t.Fatalf("unexpected spaces length: %d", len(spaces))
	}
	if spaces[0].Path != "/personal" {
		t.Fatalf("unexpected personal path: %s", spaces[0].Path)
	}
	if spaces[1].Path != "/dapps/storage" {
		t.Fatalf("unexpected apps path: %s", spaces[1].Path)
	}
}

func assertDirectoryExists(t *testing.T, dir string) {
	t.Helper()

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("expected directory %s to exist: %v", dir, err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %s to be a directory", dir)
	}
}
