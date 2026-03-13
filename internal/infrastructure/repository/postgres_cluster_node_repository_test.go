package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/yeying-community/warehouse/internal/domain/cluster"
)

func TestPostgresClusterNodeRepositoryUpsertHeartbeat(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewPostgresClusterNodeRepository(db)
	lastHeartbeatAt := time.Date(2026, 3, 13, 8, 0, 0, 0, time.UTC)
	createdAt := lastHeartbeatAt.Add(-time.Minute)
	updatedAt := lastHeartbeatAt.Add(time.Second)

	mock.ExpectQuery(regexp.QuoteMeta(`
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
	`)).
		WithArgs("node-b", "standby", "http://127.0.0.1:6066", lastHeartbeatAt).
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(createdAt, updatedAt))

	node := &cluster.Node{
		NodeID:          "node-b",
		Role:            "standby",
		AdvertiseURL:    "http://127.0.0.1:6066",
		LastHeartbeatAt: lastHeartbeatAt,
	}
	if err := repo.UpsertHeartbeat(context.Background(), node); err != nil {
		t.Fatalf("UpsertHeartbeat: %v", err)
	}
	if !node.CreatedAt.Equal(createdAt) || !node.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("unexpected persisted node timestamps: %#v", node)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPostgresClusterNodeRepositoryListByRole(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewPostgresClusterNodeRepository(db)
	lastHeartbeatAt := time.Date(2026, 3, 13, 8, 0, 0, 0, time.UTC)
	createdAt := lastHeartbeatAt.Add(-time.Minute)
	updatedAt := lastHeartbeatAt.Add(time.Second)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT node_id, role, advertise_url, last_heartbeat_at, created_at, updated_at
		FROM cluster_nodes
		WHERE role = $1
		ORDER BY last_heartbeat_at DESC, node_id ASC
	`)).
		WithArgs("standby").
		WillReturnRows(sqlmock.NewRows([]string{
			"node_id", "role", "advertise_url", "last_heartbeat_at", "created_at", "updated_at",
		}).AddRow("node-b", "standby", "http://127.0.0.1:6066", lastHeartbeatAt, createdAt, updatedAt))

	nodes, err := repo.ListByRole(context.Background(), "standby")
	if err != nil {
		t.Fatalf("ListByRole: %v", err)
	}
	if len(nodes) != 1 || nodes[0].NodeID != "node-b" {
		t.Fatalf("unexpected nodes: %#v", nodes)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
