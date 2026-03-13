package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/cluster"
)

// ClusterNodeRepository stores node registration and discovery state.
type ClusterNodeRepository interface {
	UpsertHeartbeat(ctx context.Context, node *cluster.Node) error
	Get(ctx context.Context, nodeID string) (*cluster.Node, error)
	ListByRole(ctx context.Context, role string) ([]*cluster.Node, error)
}

// PostgresClusterNodeRepository is the PostgreSQL implementation.
type PostgresClusterNodeRepository struct {
	db *sql.DB
}

// NewPostgresClusterNodeRepository creates a cluster node repository.
func NewPostgresClusterNodeRepository(db *sql.DB) *PostgresClusterNodeRepository {
	return &PostgresClusterNodeRepository{db: db}
}

// UpsertHeartbeat refreshes one node registration row.
func (r *PostgresClusterNodeRepository) UpsertHeartbeat(ctx context.Context, node *cluster.Node) error {
	if node == nil {
		return fmt.Errorf("cluster node is required")
	}
	node.Normalize()
	if node.NodeID == "" {
		return fmt.Errorf("cluster node id is required")
	}
	if node.Role == "" {
		return fmt.Errorf("cluster node role is required")
	}
	if node.AdvertiseURL == "" {
		return fmt.Errorf("cluster node advertise url is required")
	}

	lastHeartbeatAt := node.LastHeartbeatAt
	if lastHeartbeatAt.IsZero() {
		lastHeartbeatAt = time.Now().UTC()
	}

	query := `
		INSERT INTO cluster_nodes (
			node_id, role, advertise_url, last_heartbeat_at
		)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (node_id)
		DO UPDATE SET
			role = EXCLUDED.role,
			advertise_url = EXCLUDED.advertise_url,
			last_heartbeat_at = EXCLUDED.last_heartbeat_at,
			updated_at = TIMEZONE('UTC', NOW())
		RETURNING created_at, updated_at
	`
	if err := r.db.QueryRowContext(ctx, query,
		node.NodeID,
		node.Role,
		node.AdvertiseURL,
		lastHeartbeatAt,
	).Scan(&node.CreatedAt, &node.UpdatedAt); err != nil {
		return fmt.Errorf("failed to upsert cluster node heartbeat: %w", err)
	}
	node.LastHeartbeatAt = lastHeartbeatAt
	return nil
}

// Get loads one registered node.
func (r *PostgresClusterNodeRepository) Get(ctx context.Context, nodeID string) (*cluster.Node, error) {
	query := `
		SELECT node_id, role, advertise_url, last_heartbeat_at, created_at, updated_at
		FROM cluster_nodes
		WHERE node_id = $1
	`
	node := &cluster.Node{}
	err := r.db.QueryRowContext(ctx, query, strings.TrimSpace(nodeID)).Scan(
		&node.NodeID,
		&node.Role,
		&node.AdvertiseURL,
		&node.LastHeartbeatAt,
		&node.CreatedAt,
		&node.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, cluster.ErrNodeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster node: %w", err)
	}
	return node, nil
}

// ListByRole lists nodes ordered by freshest heartbeat first.
func (r *PostgresClusterNodeRepository) ListByRole(ctx context.Context, role string) ([]*cluster.Node, error) {
	query := `
		SELECT node_id, role, advertise_url, last_heartbeat_at, created_at, updated_at
		FROM cluster_nodes
		WHERE role = $1
		ORDER BY last_heartbeat_at DESC, node_id ASC
	`
	rows, err := r.db.QueryContext(ctx, query, strings.TrimSpace(role))
	if err != nil {
		return nil, fmt.Errorf("failed to list cluster nodes by role: %w", err)
	}
	defer rows.Close()

	nodes := make([]*cluster.Node, 0, 4)
	for rows.Next() {
		node := &cluster.Node{}
		if err := rows.Scan(
			&node.NodeID,
			&node.Role,
			&node.AdvertiseURL,
			&node.LastHeartbeatAt,
			&node.CreatedAt,
			&node.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan cluster node: %w", err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate cluster nodes: %w", err)
	}
	return nodes, nil
}
