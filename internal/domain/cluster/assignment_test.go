package cluster

import "testing"

func TestCanTransitionAssignmentState(t *testing.T) {
	tests := []struct {
		from    string
		to      string
		allowed bool
	}{
		{from: AssignmentStatePending, to: AssignmentStateReconciling, allowed: true},
		{from: AssignmentStatePending, to: AssignmentStatePaused, allowed: true},
		{from: AssignmentStateReconciling, to: AssignmentStateReplicating, allowed: true},
		{from: AssignmentStateReconciling, to: AssignmentStatePaused, allowed: true},
		{from: AssignmentStateReplicating, to: AssignmentStateDraining, allowed: true},
		{from: AssignmentStateReplicating, to: AssignmentStatePaused, allowed: true},
		{from: AssignmentStateDraining, to: AssignmentStateReleased, allowed: true},
		{from: AssignmentStateDraining, to: AssignmentStatePaused, allowed: true},
		{from: AssignmentStateError, to: AssignmentStatePending, allowed: true},
		{from: AssignmentStateError, to: AssignmentStatePaused, allowed: true},
		{from: AssignmentStateReleased, to: AssignmentStatePending, allowed: true},
		{from: AssignmentStatePaused, to: AssignmentStatePending, allowed: true},
		{from: AssignmentStateReplicating, to: AssignmentStateReleased, allowed: false},
		{from: AssignmentStateReplicating, to: AssignmentStatePending, allowed: false},
		{from: AssignmentStateReleased, to: AssignmentStateReplicating, allowed: false},
		{from: AssignmentStateError, to: AssignmentStateReplicating, allowed: false},
		{from: AssignmentStatePaused, to: AssignmentStateReplicating, allowed: false},
	}

	for _, tc := range tests {
		allowed := CanTransitionAssignmentState(tc.from, tc.to)
		if allowed != tc.allowed {
			t.Fatalf("transition %q -> %q allowed=%v, want %v", tc.from, tc.to, allowed, tc.allowed)
		}
	}
}

func TestAdvancesAssignmentGeneration(t *testing.T) {
	tests := []struct {
		from     string
		to       string
		expected bool
	}{
		{from: AssignmentStateError, to: AssignmentStatePending, expected: true},
		{from: AssignmentStateReleased, to: AssignmentStatePending, expected: true},
		{from: AssignmentStatePaused, to: AssignmentStatePending, expected: true},
		{from: AssignmentStatePending, to: AssignmentStateReconciling, expected: false},
		{from: AssignmentStateReconciling, to: AssignmentStateReplicating, expected: false},
		{from: AssignmentStateReplicating, to: AssignmentStateDraining, expected: false},
		{from: AssignmentStateDraining, to: AssignmentStateReleased, expected: false},
		{from: AssignmentStatePending, to: AssignmentStatePaused, expected: false},
	}

	for _, tc := range tests {
		if got := AdvancesAssignmentGeneration(tc.from, tc.to); got != tc.expected {
			t.Fatalf("transition %q -> %q advances generation=%v, want %v", tc.from, tc.to, got, tc.expected)
		}
	}
}
