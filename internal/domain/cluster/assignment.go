package cluster

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrReplicationAssignmentNotFound           = errors.New("cluster replication assignment not found")
	ErrReplicationAssignmentGenerationMismatch = errors.New("cluster replication assignment generation mismatch")
	ErrInvalidReplicationAssignmentTransition  = errors.New("invalid cluster replication assignment state transition")
)

const (
	AssignmentStatePending     = "pending"
	AssignmentStateReconciling = "reconciling"
	AssignmentStateReplicating = "replicating"
	AssignmentStateDraining    = "draining"
	AssignmentStateReleased    = "released"
	AssignmentStateError       = "error"
)

// ReplicationAssignment describes one control-plane assignment between active and standby.
type ReplicationAssignment struct {
	ID                 int64
	ActiveNodeID       string
	StandbyNodeID      string
	State              string
	Generation         int64
	LeaseExpiresAt     *time.Time
	LastReconcileJobID *int64
	LastError          *string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// LeaseExpired reports whether the assignment lease is expired at the given time.
func (a *ReplicationAssignment) LeaseExpired(now time.Time) bool {
	if a == nil || a.LeaseExpiresAt == nil {
		return false
	}
	return !a.LeaseExpiresAt.After(now)
}

// Normalize trims persisted assignment fields before comparison or validation.
func (a *ReplicationAssignment) Normalize() {
	if a == nil {
		return
	}
	a.ActiveNodeID = strings.TrimSpace(a.ActiveNodeID)
	a.StandbyNodeID = strings.TrimSpace(a.StandbyNodeID)
	a.State = strings.TrimSpace(a.State)
}

// Effective reports whether the assignment is part of the live replication lifecycle.
func (a *ReplicationAssignment) Effective() bool {
	if a == nil {
		return false
	}
	return IsEffectiveAssignmentState(a.State)
}

// IsEffectiveAssignmentState reports whether the given assignment state is live.
func IsEffectiveAssignmentState(state string) bool {
	switch strings.TrimSpace(state) {
	case AssignmentStatePending,
		AssignmentStateReconciling,
		AssignmentStateReplicating,
		AssignmentStateDraining:
		return true
	default:
		return false
	}
}

// CanTransitionAssignmentState reports whether the state machine allows from -> to.
func CanTransitionAssignmentState(from, to string) bool {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" || to == "" {
		return false
	}
	if from == to {
		return true
	}

	switch from {
	case AssignmentStatePending:
		return to == AssignmentStateReconciling || to == AssignmentStateDraining || to == AssignmentStateError
	case AssignmentStateReconciling:
		return to == AssignmentStateReplicating || to == AssignmentStateDraining || to == AssignmentStateError
	case AssignmentStateReplicating:
		return to == AssignmentStateDraining || to == AssignmentStateError
	case AssignmentStateDraining:
		return to == AssignmentStateReleased || to == AssignmentStateError
	case AssignmentStateError:
		return to == AssignmentStatePending || to == AssignmentStateReleased
	case AssignmentStateReleased:
		return to == AssignmentStatePending
	default:
		return false
	}
}

// AdvancesAssignmentGeneration reports whether the state change starts a new replication lifecycle.
// A new generation is only needed when a previously terminated assignment is resumed.
func AdvancesAssignmentGeneration(from, to string) bool {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	return to == AssignmentStatePending && (from == AssignmentStateReleased || from == AssignmentStateError)
}
