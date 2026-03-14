package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/cluster"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
	"go.uber.org/zap"
)

const (
	defaultAssignmentRenewInterval  = 5 * time.Second
	defaultAssignmentLeaseDuration  = 20 * time.Second
	defaultAssignmentErrorRetryBase = 10 * time.Second
	defaultAssignmentErrorRetryMax  = 5 * time.Minute
)

type assignmentErrorRecoveryState struct {
	attempt     int
	nextRetryAt time.Time
}

// ReplicationAssignmentAllocator maintains the effective standby assignment for one active node.
type ReplicationAssignmentAllocator struct {
	config             *config.Config
	nodes              repository.ClusterNodeRepository
	assignments        repository.ClusterReplicationAssignmentRepository
	logger             *zap.Logger
	now                func() time.Time
	renewInterval      time.Duration
	leaseDuration      time.Duration
	maxStaleness       time.Duration
	errorRetryBase     time.Duration
	errorRetryMax      time.Duration
	errorRecoveryPairs map[string]assignmentErrorRecoveryState
}

// NewReplicationAssignmentAllocator creates an active-side assignment allocator/renewer.
func NewReplicationAssignmentAllocator(
	cfg *config.Config,
	nodes repository.ClusterNodeRepository,
	assignments repository.ClusterReplicationAssignmentRepository,
	logger *zap.Logger,
) *ReplicationAssignmentAllocator {
	if cfg == nil || nodes == nil || assignments == nil {
		return nil
	}
	return &ReplicationAssignmentAllocator{
		config:             cfg,
		nodes:              nodes,
		assignments:        assignments,
		logger:             logger,
		now:                time.Now,
		renewInterval:      defaultAssignmentRenewInterval,
		leaseDuration:      defaultAssignmentLeaseDuration,
		maxStaleness:       defaultClusterNodeStaleness,
		errorRetryBase:     defaultAssignmentErrorRetryBase,
		errorRetryMax:      defaultAssignmentErrorRetryMax,
		errorRecoveryPairs: make(map[string]assignmentErrorRecoveryState),
	}
}

// Enabled reports whether the current node should allocate/renew assignments.
func (a *ReplicationAssignmentAllocator) Enabled() bool {
	if a == nil || a.config == nil {
		return false
	}
	return a.config.Replication.Enabled &&
		strings.EqualFold(strings.TrimSpace(a.config.Node.Role), "active") &&
		strings.TrimSpace(a.config.Node.ID) != ""
}

// Run starts the periodic renew loop until ctx is canceled.
func (a *ReplicationAssignmentAllocator) Run(ctx context.Context) {
	if !a.Enabled() {
		return
	}

	ticker := time.NewTicker(a.renewInterval)
	defer ticker.Stop()

	if a.logger != nil {
		a.logger.Info("replication assignment allocator started",
			zap.String("active_node_id", a.config.Node.ID),
			zap.Duration("renew_interval", a.renewInterval),
			zap.Duration("lease_duration", a.leaseDuration))
		defer a.logger.Info("replication assignment allocator stopped",
			zap.String("active_node_id", a.config.Node.ID))
	}

	if err := a.SyncOnce(ctx); err != nil && !errors.Is(err, context.Canceled) && a.logger != nil {
		a.logger.Warn("replication assignment allocator sync failed", zap.Error(err))
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.SyncOnce(ctx); err != nil && !errors.Is(err, context.Canceled) && a.logger != nil {
				a.logger.Warn("replication assignment allocator sync failed", zap.Error(err))
			}
		}
	}
}

// SyncOnce writes or releases effective assignments based on healthy standby discovery.
func (a *ReplicationAssignmentAllocator) SyncOnce(ctx context.Context) error {
	if !a.Enabled() {
		return nil
	}

	targets, err := a.selectTargetStandbys(ctx)
	if err != nil {
		return err
	}
	activeNodeID := strings.TrimSpace(a.config.Node.ID)
	if len(targets) == 0 {
		if err := a.assignments.ReleaseByActiveExcept(ctx, activeNodeID, nil); err != nil {
			return fmt.Errorf("release assignments without target: %w", err)
		}
		return nil
	}

	keepStandbyIDs := make([]string, 0, len(targets))
	for _, target := range targets {
		if target == nil {
			continue
		}
		keepStandbyIDs = append(keepStandbyIDs, target.NodeID)

		leaseExpiresAt := a.now().UTC().Add(a.leaseDuration)
		state := cluster.AssignmentStatePending
		currentAssignment, err := a.assignments.GetByPair(ctx, activeNodeID, target.NodeID)
		if err != nil {
			return fmt.Errorf("load assignment for standby %q: %w", target.NodeID, err)
		}
		if currentAssignment != nil {
			switch {
			case currentAssignment.Effective():
				state = currentAssignment.State
				a.clearErrorRecoveryPair(activeNodeID, target.NodeID)
			case currentAssignment.State == cluster.AssignmentStateError && a.shouldRecoverErrorPair(activeNodeID, target.NodeID, a.now().UTC()):
				if a.logger != nil {
					recovery := a.errorRecoveryPairs[a.errorRecoveryKey(activeNodeID, target.NodeID)]
					a.logger.Info("recovering error assignment to pending",
						zap.String("active_node_id", activeNodeID),
						zap.String("standby_node_id", target.NodeID),
						zap.Int64("generation", currentAssignment.Generation),
						zap.Int("attempt", recovery.attempt),
						zap.Time("next_retry_at", recovery.nextRetryAt))
				}
			case currentAssignment.State == cluster.AssignmentStateError:
				state = currentAssignment.State
			}
		} else {
			a.clearErrorRecoveryPair(activeNodeID, target.NodeID)
		}
		assignment := &cluster.ReplicationAssignment{
			ActiveNodeID:   activeNodeID,
			StandbyNodeID:  target.NodeID,
			State:          state,
			LeaseExpiresAt: &leaseExpiresAt,
		}
		if err := a.assignments.UpsertLease(ctx, assignment); err != nil {
			return fmt.Errorf("upsert assignment lease for standby %q: %w", target.NodeID, err)
		}
		if a.logger != nil {
			a.logger.Debug("replication assignment renewed",
				zap.String("active_node_id", activeNodeID),
				zap.String("standby_node_id", target.NodeID),
				zap.Int64("generation", assignment.Generation),
				zap.Time("lease_expires_at", leaseExpiresAt))
		}
	}
	if err := a.assignments.ReleaseByActiveExcept(ctx, activeNodeID, keepStandbyIDs); err != nil {
		return fmt.Errorf("release stale assignments for active %q: %w", activeNodeID, err)
	}
	return nil
}

func (a *ReplicationAssignmentAllocator) selectTargetStandbys(ctx context.Context) ([]*cluster.Node, error) {
	nodes, err := a.nodes.ListByRole(ctx, "standby")
	if err != nil {
		return nil, fmt.Errorf("list standby nodes: %w", err)
	}

	now := a.now().UTC()
	healthyStandbys := make([]*cluster.Node, 0, len(nodes))
	healthyByID := make(map[string]*cluster.Node, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		node.Normalize()
		if node.NodeID == "" || node.AdvertiseURL == "" {
			continue
		}
		if !node.Healthy(now, a.maxStaleness) {
			continue
		}
		healthyStandbys = append(healthyStandbys, node)
		healthyByID[node.NodeID] = node
	}
	if len(healthyStandbys) == 0 {
		return nil, nil
	}

	currentAssignments, err := a.assignments.ListEffectiveByActive(ctx, strings.TrimSpace(a.config.Node.ID))
	if err != nil {
		return nil, fmt.Errorf("list effective assignments: %w", err)
	}
	selected := make([]*cluster.Node, 0, len(healthyStandbys))
	selectedByID := make(map[string]struct{}, len(healthyStandbys))
	for _, assignment := range currentAssignments {
		if assignment == nil {
			continue
		}
		if standby := healthyByID[strings.TrimSpace(assignment.StandbyNodeID)]; standby != nil {
			selected = append(selected, standby)
			selectedByID[standby.NodeID] = struct{}{}
		}
	}
	for _, standby := range healthyStandbys {
		if standby == nil {
			continue
		}
		if _, ok := selectedByID[standby.NodeID]; ok {
			continue
		}
		selected = append(selected, standby)
	}
	return selected, nil
}

func (a *ReplicationAssignmentAllocator) shouldRecoverErrorPair(activeNodeID, standbyNodeID string, now time.Time) bool {
	if a == nil {
		return false
	}
	key := a.errorRecoveryKey(activeNodeID, standbyNodeID)
	if recovery, ok := a.errorRecoveryPairs[key]; ok && recovery.nextRetryAt.After(now) {
		return false
	}

	attempt := 1
	if recovery, ok := a.errorRecoveryPairs[key]; ok {
		attempt = recovery.attempt + 1
	}
	a.errorRecoveryPairs[key] = assignmentErrorRecoveryState{
		attempt:     attempt,
		nextRetryAt: now.Add(a.errorRecoveryDelay(attempt)),
	}
	return true
}

func (a *ReplicationAssignmentAllocator) clearErrorRecoveryPair(activeNodeID, standbyNodeID string) {
	if a == nil {
		return
	}
	delete(a.errorRecoveryPairs, a.errorRecoveryKey(activeNodeID, standbyNodeID))
}

func (a *ReplicationAssignmentAllocator) errorRecoveryKey(activeNodeID, standbyNodeID string) string {
	return strings.TrimSpace(activeNodeID) + "->" + strings.TrimSpace(standbyNodeID)
}

func (a *ReplicationAssignmentAllocator) errorRecoveryDelay(attempt int) time.Duration {
	base := a.errorRetryBase
	maxDelay := a.errorRetryMax
	if base <= 0 {
		base = defaultAssignmentErrorRetryBase
	}
	if maxDelay < base {
		maxDelay = defaultAssignmentErrorRetryMax
	}
	if attempt <= 1 {
		return base
	}

	delay := base
	for i := 1; i < attempt; i++ {
		if delay >= maxDelay/2 {
			return maxDelay
		}
		delay *= 2
	}
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}
