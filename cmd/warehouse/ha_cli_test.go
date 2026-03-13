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

func TestResolveAssignmentMutationTargetDefaultsToCurrentActiveNode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-a"
	cfg.Node.Role = "active"

	target, err := resolveAssignmentMutationTarget(cfg, "", "node-b")
	if err != nil {
		t.Fatalf("resolveAssignmentMutationTarget returned error: %v", err)
	}
	if target.ActiveNodeID != "node-a" || target.StandbyNodeID != "node-b" {
		t.Fatalf("unexpected target: %#v", target)
	}
}

func TestResolveAssignmentMutationTargetDefaultsToCurrentStandbyNode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Node.ID = "node-b"
	cfg.Node.Role = "standby"

	target, err := resolveAssignmentMutationTarget(cfg, "node-a", "")
	if err != nil {
		t.Fatalf("resolveAssignmentMutationTarget returned error: %v", err)
	}
	if target.ActiveNodeID != "node-a" || target.StandbyNodeID != "node-b" {
		t.Fatalf("unexpected target: %#v", target)
	}
}

func TestResolveAssignmentMutationTargetRequiresBothSides(t *testing.T) {
	if _, err := resolveAssignmentMutationTarget(nil, "", "node-b"); err == nil {
		t.Fatalf("expected missing active node id error")
	}
	if _, err := resolveAssignmentMutationTarget(nil, "node-a", ""); err == nil {
		t.Fatalf("expected missing standby node id error")
	}
}

func TestPlanAssignmentStateMutationDrain(t *testing.T) {
	assignment := &cluster.ReplicationAssignment{
		ActiveNodeID:  "node-a",
		StandbyNodeID: "node-b",
		State:         cluster.AssignmentStateReplicating,
		Generation:    4,
	}

	updated, changed, notes, err := planAssignmentStateMutation("drain", assignment, true, false, time.Now().UTC())
	if err != nil {
		t.Fatalf("planAssignmentStateMutation returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected state mutation to change assignment")
	}
	if updated.State != cluster.AssignmentStateDraining {
		t.Fatalf("expected draining state, got %#v", updated)
	}
	if len(notes) == 0 {
		t.Fatalf("expected operator note")
	}
}

func TestPlanAssignmentStateMutationReleaseRequiresDraining(t *testing.T) {
	assignment := &cluster.ReplicationAssignment{
		ActiveNodeID:  "node-a",
		StandbyNodeID: "node-b",
		State:         cluster.AssignmentStateReplicating,
		Generation:    4,
	}

	if _, _, _, err := planAssignmentStateMutation("release", assignment, false, false, time.Now().UTC()); err == nil {
		t.Fatalf("expected release to require draining state")
	}
}

func TestPlanAssignmentStateMutationReleaseRejectsHealthyStandbyWithoutForce(t *testing.T) {
	assignment := &cluster.ReplicationAssignment{
		ActiveNodeID:  "node-a",
		StandbyNodeID: "node-b",
		State:         cluster.AssignmentStateDraining,
		Generation:    4,
	}

	if _, _, _, err := planAssignmentStateMutation("release", assignment, true, false, time.Now().UTC()); err == nil {
		t.Fatalf("expected healthy standby release to be rejected without force")
	}
}

func TestPlanAssignmentStateMutationReleaseForced(t *testing.T) {
	assignment := &cluster.ReplicationAssignment{
		ActiveNodeID:  "node-a",
		StandbyNodeID: "node-b",
		State:         cluster.AssignmentStateDraining,
		Generation:    4,
	}
	now := time.Now().UTC()

	updated, changed, notes, err := planAssignmentStateMutation("release", assignment, true, true, now)
	if err != nil {
		t.Fatalf("planAssignmentStateMutation returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected forced release to change assignment")
	}
	if updated.State != cluster.AssignmentStateReleased {
		t.Fatalf("expected released state, got %#v", updated)
	}
	if updated.LeaseExpiresAt == nil || !updated.LeaseExpiresAt.Equal(now) {
		t.Fatalf("expected lease expiry to be set to now, got %#v", updated.LeaseExpiresAt)
	}
	if len(notes) == 0 {
		t.Fatalf("expected forced release note")
	}
}

func TestPlanAssignmentStateMutationRetry(t *testing.T) {
	assignment := &cluster.ReplicationAssignment{
		ActiveNodeID:  "node-a",
		StandbyNodeID: "node-b",
		State:         cluster.AssignmentStateError,
		Generation:    4,
		LastError:     stringPointer("dispatch failed"),
	}

	updated, changed, notes, err := planAssignmentStateMutation("retry", assignment, true, false, time.Now().UTC())
	if err != nil {
		t.Fatalf("planAssignmentStateMutation returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected retry to change assignment")
	}
	if updated.State != cluster.AssignmentStatePending {
		t.Fatalf("expected pending state, got %#v", updated)
	}
	if updated.LastError != nil {
		t.Fatalf("expected last error to be cleared, got %#v", updated.LastError)
	}
	if len(notes) == 0 {
		t.Fatalf("expected retry note")
	}
}

func stringPointer(value string) *string {
	v := value
	return &v
}
