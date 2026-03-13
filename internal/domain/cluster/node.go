package cluster

import (
	"errors"
	"strings"
	"time"
)

var ErrNodeNotFound = errors.New("cluster node not found")

// Node describes one registered warehouse instance in the shared control plane.
type Node struct {
	NodeID          string
	Role            string
	AdvertiseURL    string
	LastHeartbeatAt time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Healthy reports whether the node heartbeat is still considered fresh.
func (n *Node) Healthy(now time.Time, maxStaleness time.Duration) bool {
	if n == nil || maxStaleness <= 0 || n.LastHeartbeatAt.IsZero() {
		return false
	}
	return n.LastHeartbeatAt.After(now.Add(-maxStaleness))
}

// Normalize trims user-provided fields before persistence/comparison.
func (n *Node) Normalize() {
	if n == nil {
		return
	}
	n.NodeID = strings.TrimSpace(n.NodeID)
	n.Role = strings.TrimSpace(n.Role)
	n.AdvertiseURL = strings.TrimSpace(n.AdvertiseURL)
}
