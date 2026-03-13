package cluster

import "time"

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
