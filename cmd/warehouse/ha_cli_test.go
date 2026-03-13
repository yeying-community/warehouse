package main

import (
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/yeying-community/warehouse/internal/domain/cluster"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
)

func TestResolveHABaseURLDefaultsToLocalhost(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Address = "0.0.0.0"
	cfg.Server.Port = 6065

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("base-url", "", "")
	flags.Bool("peer", false, "")

	baseURL, err := resolveHABaseURL(cfg, flags)
	if err != nil {
		t.Fatalf("resolveHABaseURL returned error: %v", err)
	}
	if baseURL != "http://127.0.0.1:6065" {
		t.Fatalf("unexpected baseURL: %q", baseURL)
	}
}

func TestResolveHABaseURLUsesPeerWhenRequested(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Internal.Replication.PeerBaseURL = "http://127.0.0.1:6066"

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("base-url", "", "")
	flags.Bool("peer", false, "")
	if err := flags.Set("peer", "true"); err != nil {
		t.Fatalf("set peer flag: %v", err)
	}

	baseURL, err := resolveHABaseURL(cfg, flags)
	if err != nil {
		t.Fatalf("resolveHABaseURL returned error: %v", err)
	}
	if baseURL != "http://127.0.0.1:6066" {
		t.Fatalf("unexpected baseURL: %q", baseURL)
	}
}

func TestResolveHABaseURLRejectsInvalidOverride(t *testing.T) {
	cfg := config.DefaultConfig()

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("base-url", "", "")
	flags.Bool("peer", false, "")
	if err := flags.Set("base-url", "127.0.0.1:6065"); err != nil {
		t.Fatalf("set base-url flag: %v", err)
	}

	if _, err := resolveHABaseURL(cfg, flags); err == nil {
		t.Fatalf("expected invalid base-url error")
	}
}

func TestResolveAssignmentStatusFilterDefaultsToCurrentActiveNode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-a"
	cfg.Node.Role = "active"

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.Bool("all", false, "")
	flags.String("active-node-id", "", "")
	flags.String("standby-node-id", "", "")
	flags.String("state", "", "")
	flags.Int("limit", 20, "")

	filter, scope, err := resolveAssignmentStatusFilter(cfg, flags)
	if err != nil {
		t.Fatalf("resolveAssignmentStatusFilter returned error: %v", err)
	}
	if scope != "current_active" {
		t.Fatalf("unexpected scope: %q", scope)
	}
	if filter.ActiveNodeID != "node-a" || filter.StandbyNodeID != "" {
		t.Fatalf("unexpected filter: %#v", filter)
	}
}

func TestResolveAssignmentStatusFilterDefaultsToCurrentStandbyNode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-b"
	cfg.Node.Role = "standby"

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.Bool("all", false, "")
	flags.String("active-node-id", "", "")
	flags.String("standby-node-id", "", "")
	flags.String("state", "", "")
	flags.Int("limit", 20, "")

	filter, scope, err := resolveAssignmentStatusFilter(cfg, flags)
	if err != nil {
		t.Fatalf("resolveAssignmentStatusFilter returned error: %v", err)
	}
	if scope != "current_standby" {
		t.Fatalf("unexpected scope: %q", scope)
	}
	if filter.StandbyNodeID != "node-b" || filter.ActiveNodeID != "" {
		t.Fatalf("unexpected filter: %#v", filter)
	}
}

func TestResolveAssignmentStatusFilterAllDisablesDefaultCurrentNodeScope(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-a"
	cfg.Node.Role = "active"

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.Bool("all", false, "")
	flags.String("active-node-id", "", "")
	flags.String("standby-node-id", "", "")
	flags.String("state", "", "")
	flags.Int("limit", 20, "")
	if err := flags.Set("all", "true"); err != nil {
		t.Fatalf("set all flag: %v", err)
	}

	filter, scope, err := resolveAssignmentStatusFilter(cfg, flags)
	if err != nil {
		t.Fatalf("resolveAssignmentStatusFilter returned error: %v", err)
	}
	if scope != "all" {
		t.Fatalf("unexpected scope: %q", scope)
	}
	if filter.ActiveNodeID != "" || filter.StandbyNodeID != "" {
		t.Fatalf("unexpected filter: %#v", filter)
	}
}

func TestBuildAssignmentStatusResponseIncludesLeaseState(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-a"
	cfg.Node.Role = "active"

	expired := time.Now().UTC().Add(-time.Minute)
	response := buildAssignmentStatusResponse(
		cfg,
		repository.ClusterReplicationAssignmentFilter{ActiveNodeID: "node-a", Limit: 20},
		"current_active",
		[]*cluster.ReplicationAssignment{
			{
				ID:             7,
				ActiveNodeID:   "node-a",
				StandbyNodeID:  "node-b",
				State:          cluster.AssignmentStateReplicating,
				Generation:     3,
				LeaseExpiresAt: &expired,
				CreatedAt:      expired.Add(-time.Minute),
				UpdatedAt:      expired,
			},
		},
	)

	if response.Count != 1 || len(response.Assignments) != 1 {
		t.Fatalf("unexpected response count: %#v", response)
	}
	if !response.Assignments[0].LeaseExpired {
		t.Fatalf("expected lease to be expired: %#v", response.Assignments[0])
	}
	if len(response.Notes) == 0 {
		t.Fatalf("expected notes in response")
	}
}
