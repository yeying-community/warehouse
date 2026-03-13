package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidateWebDAVAutoCreateDirectory(t *testing.T) {
	loader := NewLoader()
	root := t.TempDir()
	target := filepath.Join(root, "webdav")

	cfg := DefaultConfig()
	cfg.WebDAV.Directory = target
	cfg.WebDAV.AutoCreateDirectory = true

	if err := loader.validateWebDAV(cfg); err != nil {
		t.Fatalf("expected directory to be auto-created, got error: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected directory to exist, got error: %v", err)
	}
}

func TestValidateWebDAVRequireExistingDirectory(t *testing.T) {
	loader := NewLoader()
	cfg := DefaultConfig()
	cfg.WebDAV.Directory = filepath.Join(t.TempDir(), "missing")
	cfg.WebDAV.AutoCreateDirectory = false

	if err := loader.validateWebDAV(cfg); err == nil {
		t.Fatalf("expected error when directory is missing and auto creation is disabled")
	}
}

func TestValidateInternalReplicationRequiresNodeIDAndSecret(t *testing.T) {
	loader := NewLoader()
	cfg := DefaultConfig()
	cfg.Internal.Replication.Enabled = true

	if err := loader.validateNode(cfg); err != nil {
		t.Fatalf("validateNode failed: %v", err)
	}
	if err := loader.validateInternal(cfg); err == nil {
		t.Fatalf("expected error when internal replication is enabled without node id and shared secret")
	}
}

func TestValidateInternalReplicationAcceptsStandbyRole(t *testing.T) {
	loader := NewLoader()
	cfg := DefaultConfig()
	cfg.Node.ID = "node-b"
	cfg.Node.Role = "standby"
	cfg.Internal.Replication.Enabled = true
	cfg.Internal.Replication.PeerNodeID = "node-a"
	cfg.Internal.Replication.SharedSecret = "secret"
	cfg.Internal.Replication.AllowedClockSkew = time.Minute
	cfg.Internal.Replication.PeerBaseURL = "https://peer.internal"

	if err := loader.validateNode(cfg); err != nil {
		t.Fatalf("expected standby role to be accepted, got: %v", err)
	}
	if err := loader.validateInternal(cfg); err != nil {
		t.Fatalf("expected internal replication config to be valid, got: %v", err)
	}
}

func TestValidateInternalReplicationActiveAllowsDynamicPeerDiscovery(t *testing.T) {
	loader := NewLoader()
	cfg := DefaultConfig()
	cfg.Node.ID = "node-a"
	cfg.Node.Role = "active"
	cfg.Internal.Replication.Enabled = true
	cfg.Internal.Replication.SharedSecret = "secret"
	cfg.Internal.Replication.AllowedClockSkew = time.Minute

	if err := loader.validateNode(cfg); err != nil {
		t.Fatalf("validateNode failed: %v", err)
	}
	if err := loader.validateInternal(cfg); err != nil {
		t.Fatalf("expected dynamic peer discovery config to be valid, got: %v", err)
	}
}

func TestValidateInternalReplicationRejectsInvalidWorkerSettings(t *testing.T) {
	loader := NewLoader()
	cfg := DefaultConfig()
	cfg.Node.ID = "node-a"
	cfg.Node.Role = "active"
	cfg.Internal.Replication.Enabled = true
	cfg.Internal.Replication.PeerNodeID = "node-b"
	cfg.Internal.Replication.PeerBaseURL = "https://peer.internal"
	cfg.Internal.Replication.SharedSecret = "secret"
	cfg.Internal.Replication.AllowedClockSkew = time.Minute
	cfg.Internal.Replication.BatchSize = 0

	if err := loader.validateNode(cfg); err != nil {
		t.Fatalf("validateNode failed: %v", err)
	}
	if err := loader.validateInternal(cfg); err == nil {
		t.Fatalf("expected invalid worker setting to be rejected")
	}
}
