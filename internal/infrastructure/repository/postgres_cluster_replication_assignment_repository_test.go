package repository

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/yeying-community/warehouse/internal/domain/cluster"
)

func TestPostgresClusterReplicationAssignmentRepositoryListWithFilters(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewPostgresClusterReplicationAssignmentRepository(db)
	leaseExpiresAt := time.Date(2026, 3, 13, 9, 0, 0, 0, time.UTC)
	createdAt := leaseExpiresAt.Add(-time.Minute)
	updatedAt := leaseExpiresAt.Add(-time.Second)
	lastReconcileJobID := int64(12)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, active_node_id, standby_node_id, state, generation,
		       lease_expires_at, last_reconcile_job_id, last_error,
		       created_at, updated_at
		FROM cluster_replication_assignments
		WHERE active_node_id = $1
		  AND standby_node_id = $2
		  AND state = $3
		ORDER BY updated_at DESC, id DESC
		LIMIT $4`)).
		WithArgs("node-a", "node-b", "replicating", 50).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "active_node_id", "standby_node_id", "state", "generation",
			"lease_expires_at", "last_reconcile_job_id", "last_error",
			"created_at", "updated_at",
		}).AddRow(
			int64(7), "node-a", "node-b", "replicating", int64(3),
			leaseExpiresAt, lastReconcileJobID, "none", createdAt, updatedAt,
		))

	assignments, err := repo.List(context.Background(), ClusterReplicationAssignmentFilter{
		ActiveNodeID:  "node-a",
		StandbyNodeID: "node-b",
		State:         "replicating",
		Limit:         50,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(assignments) != 1 {
		t.Fatalf("expected 1 assignment, got %d", len(assignments))
	}
	if assignments[0].ID != 7 || assignments[0].Generation != 3 {
		t.Fatalf("unexpected assignment: %#v", assignments[0])
	}
	if assignments[0].LeaseExpiresAt == nil || !assignments[0].LeaseExpiresAt.Equal(leaseExpiresAt) {
		t.Fatalf("unexpected lease_expires_at: %#v", assignments[0].LeaseExpiresAt)
	}
	if assignments[0].LastReconcileJobID == nil || *assignments[0].LastReconcileJobID != lastReconcileJobID {
		t.Fatalf("unexpected last_reconcile_job_id: %#v", assignments[0].LastReconcileJobID)
	}
	if assignments[0].LastError == nil || *assignments[0].LastError != "none" {
		t.Fatalf("unexpected last_error: %#v", assignments[0].LastError)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPostgresClusterReplicationAssignmentRepositoryListUsesDefaultLimit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewPostgresClusterReplicationAssignmentRepository(db)
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, active_node_id, standby_node_id, state, generation,
		       lease_expires_at, last_reconcile_job_id, last_error,
		       created_at, updated_at
		FROM cluster_replication_assignments
		ORDER BY updated_at DESC, id DESC
		LIMIT $1`)).
		WithArgs(defaultClusterAssignmentLimit).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "active_node_id", "standby_node_id", "state", "generation",
			"lease_expires_at", "last_reconcile_job_id", "last_error",
			"created_at", "updated_at",
		}))

	assignments, err := repo.List(context.Background(), ClusterReplicationAssignmentFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(assignments) != 0 {
		t.Fatalf("expected 0 assignments, got %d", len(assignments))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPostgresClusterReplicationAssignmentRepositoryListEffectiveByActive(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewPostgresClusterReplicationAssignmentRepository(db)
	leaseExpiresAt := time.Date(2026, 3, 13, 9, 0, 0, 0, time.UTC)
	createdAt := leaseExpiresAt.Add(-2 * time.Minute)
	updatedAt := leaseExpiresAt.Add(-time.Minute)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, active_node_id, standby_node_id, state, generation,
		       lease_expires_at, last_reconcile_job_id, last_error,
		       created_at, updated_at
		FROM cluster_replication_assignments
		WHERE active_node_id = $1
		  AND state IN ('pending', 'reconciling', 'replicating', 'draining')
		ORDER BY updated_at DESC, id DESC
	`)).
		WithArgs("node-a").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "active_node_id", "standby_node_id", "state", "generation",
			"lease_expires_at", "last_reconcile_job_id", "last_error",
			"created_at", "updated_at",
		}).AddRow(
			int64(11), "node-a", "node-b", "replicating", int64(2),
			leaseExpiresAt, nil, nil, createdAt, updatedAt,
		))

	assignments, err := repo.ListEffectiveByActive(context.Background(), "node-a")
	if err != nil {
		t.Fatalf("ListEffectiveByActive: %v", err)
	}
	if len(assignments) != 1 {
		t.Fatalf("expected 1 assignment, got %d", len(assignments))
	}
	if assignments[0].StandbyNodeID != "node-b" || assignments[0].State != cluster.AssignmentStateReplicating {
		t.Fatalf("unexpected assignment: %#v", assignments[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPostgresClusterReplicationAssignmentRepositoryUpsertLease(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewPostgresClusterReplicationAssignmentRepository(db)
	leaseExpiresAt := time.Date(2026, 3, 13, 9, 0, 30, 0, time.UTC)
	createdAt := leaseExpiresAt.Add(-time.Minute)
	updatedAt := leaseExpiresAt.Add(-time.Second)
	lastReconcileJobID := int64(9)

	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO cluster_replication_assignments (
			active_node_id, standby_node_id, state, lease_expires_at
		)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (active_node_id, standby_node_id)
		DO UPDATE SET
			state = EXCLUDED.state,
			generation = CASE
				WHEN cluster_replication_assignments.state IN ('released', 'error')
				 AND EXCLUDED.state = 'pending'
				THEN cluster_replication_assignments.generation + 1
				ELSE cluster_replication_assignments.generation
			END,
			lease_expires_at = EXCLUDED.lease_expires_at,
			last_error = NULL,
			updated_at = TIMEZONE('UTC', NOW())
		RETURNING id, generation, last_reconcile_job_id, last_error, created_at, updated_at
	`)).
		WithArgs("node-a", "node-b", cluster.AssignmentStateReplicating, leaseExpiresAt).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "generation", "last_reconcile_job_id", "last_error", "created_at", "updated_at",
		}).AddRow(
			int64(15), int64(4), lastReconcileJobID, nil, createdAt, updatedAt,
		))

	assignment := &cluster.ReplicationAssignment{
		ActiveNodeID:   "node-a",
		StandbyNodeID:  "node-b",
		State:          cluster.AssignmentStateReplicating,
		LeaseExpiresAt: &leaseExpiresAt,
	}
	if err := repo.UpsertLease(context.Background(), assignment); err != nil {
		t.Fatalf("UpsertLease: %v", err)
	}
	if assignment.ID != 15 || assignment.Generation != 4 {
		t.Fatalf("unexpected assignment identity: %#v", assignment)
	}
	if assignment.LastReconcileJobID == nil || *assignment.LastReconcileJobID != lastReconcileJobID {
		t.Fatalf("unexpected last_reconcile_job_id: %#v", assignment.LastReconcileJobID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPostgresClusterReplicationAssignmentRepositoryGetEffectiveByStandby(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewPostgresClusterReplicationAssignmentRepository(db)
	leaseExpiresAt := time.Date(2026, 3, 13, 9, 0, 0, 0, time.UTC)
	createdAt := leaseExpiresAt.Add(-2 * time.Minute)
	updatedAt := leaseExpiresAt.Add(-time.Second)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, active_node_id, standby_node_id, state, generation,
		       lease_expires_at, last_reconcile_job_id, last_error,
		       created_at, updated_at
		FROM cluster_replication_assignments
		WHERE standby_node_id = $1
		  AND state IN ('pending', 'reconciling', 'replicating', 'draining')
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
	`)).
		WithArgs("node-b").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "active_node_id", "standby_node_id", "state", "generation",
			"lease_expires_at", "last_reconcile_job_id", "last_error",
			"created_at", "updated_at",
		}).AddRow(
			int64(31), "node-a", "node-b", cluster.AssignmentStateReplicating, int64(5),
			leaseExpiresAt, nil, nil, createdAt, updatedAt,
		))

	assignment, err := repo.GetEffectiveByStandby(context.Background(), "node-b")
	if err != nil {
		t.Fatalf("GetEffectiveByStandby: %v", err)
	}
	if assignment == nil {
		t.Fatalf("expected assignment")
	}
	if assignment.ActiveNodeID != "node-a" || assignment.StandbyNodeID != "node-b" {
		t.Fatalf("unexpected assignment: %#v", assignment)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPostgresClusterReplicationAssignmentRepositoryGetByPair(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewPostgresClusterReplicationAssignmentRepository(db)
	leaseExpiresAt := time.Date(2026, 3, 13, 9, 0, 0, 0, time.UTC)
	createdAt := leaseExpiresAt.Add(-2 * time.Minute)
	updatedAt := leaseExpiresAt.Add(-time.Second)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, active_node_id, standby_node_id, state, generation,
		       lease_expires_at, last_reconcile_job_id, last_error,
		       created_at, updated_at
		FROM cluster_replication_assignments
		WHERE active_node_id = $1
		  AND standby_node_id = $2
		LIMIT 1
	`)).
		WithArgs("node-a", "node-b").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "active_node_id", "standby_node_id", "state", "generation",
			"lease_expires_at", "last_reconcile_job_id", "last_error",
			"created_at", "updated_at",
		}).AddRow(
			int64(31), "node-a", "node-b", cluster.AssignmentStatePending, int64(5),
			leaseExpiresAt, nil, nil, createdAt, updatedAt,
		))

	assignment, err := repo.GetByPair(context.Background(), "node-a", "node-b")
	if err != nil {
		t.Fatalf("GetByPair: %v", err)
	}
	if assignment == nil {
		t.Fatalf("expected assignment")
	}
	if assignment.Generation != 5 || assignment.State != cluster.AssignmentStatePending {
		t.Fatalf("unexpected assignment: %#v", assignment)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPostgresClusterReplicationAssignmentRepositoryUpdateState(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewPostgresClusterReplicationAssignmentRepository(db)
	leaseExpiresAt := time.Date(2026, 3, 13, 9, 1, 0, 0, time.UTC)
	createdAt := leaseExpiresAt.Add(-3 * time.Minute)
	updatedAt := leaseExpiresAt.Add(-time.Second)
	jobID := int64(21)

	mock.ExpectQuery(regexp.QuoteMeta(`
		UPDATE cluster_replication_assignments
		SET state = $4,
		    generation = CASE
		    	WHEN state IN ('released', 'error')
		    	 AND $4 = 'pending'
		    	THEN generation + 1
		    	ELSE generation
		    END,
		    lease_expires_at = $5,
		    last_reconcile_job_id = $6,
		    last_error = $7,
		    updated_at = TIMEZONE('UTC', NOW())
		WHERE active_node_id = $1
		  AND standby_node_id = $2
		  AND generation = $3
		RETURNING id, generation, created_at, updated_at
	`)).
		WithArgs("node-a", "node-b", int64(6), cluster.AssignmentStateReconciling, leaseExpiresAt, jobID, nil).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "generation", "created_at", "updated_at",
		}).AddRow(int64(44), int64(6), createdAt, updatedAt))

	assignment := &cluster.ReplicationAssignment{
		ActiveNodeID:       "node-a",
		StandbyNodeID:      "node-b",
		State:              cluster.AssignmentStateReconciling,
		Generation:         6,
		LeaseExpiresAt:     &leaseExpiresAt,
		LastReconcileJobID: &jobID,
	}
	if err := repo.UpdateState(context.Background(), assignment); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	if assignment.ID != 44 {
		t.Fatalf("unexpected assignment id: %#v", assignment)
	}
	if !assignment.CreatedAt.Equal(createdAt) || !assignment.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("unexpected timestamps: %#v", assignment)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPostgresClusterReplicationAssignmentRepositoryUpdateStateAdvancesGenerationOnRetry(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewPostgresClusterReplicationAssignmentRepository(db)
	leaseExpiresAt := time.Date(2026, 3, 13, 9, 1, 0, 0, time.UTC)
	createdAt := leaseExpiresAt.Add(-3 * time.Minute)
	updatedAt := leaseExpiresAt.Add(-time.Second)

	mock.ExpectQuery(regexp.QuoteMeta(`
		UPDATE cluster_replication_assignments
		SET state = $4,
		    generation = CASE
		    	WHEN state IN ('released', 'error')
		    	 AND $4 = 'pending'
		    	THEN generation + 1
		    	ELSE generation
		    END,
		    lease_expires_at = $5,
		    last_reconcile_job_id = $6,
		    last_error = $7,
		    updated_at = TIMEZONE('UTC', NOW())
		WHERE active_node_id = $1
		  AND standby_node_id = $2
		  AND generation = $3
		RETURNING id, generation, created_at, updated_at
	`)).
		WithArgs("node-a", "node-b", int64(6), cluster.AssignmentStatePending, leaseExpiresAt, nil, nil).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "generation", "created_at", "updated_at",
		}).AddRow(int64(44), int64(7), createdAt, updatedAt))

	assignment := &cluster.ReplicationAssignment{
		ActiveNodeID:   "node-a",
		StandbyNodeID:  "node-b",
		State:          cluster.AssignmentStatePending,
		Generation:     6,
		LeaseExpiresAt: &leaseExpiresAt,
	}
	if err := repo.UpdateState(context.Background(), assignment); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	if assignment.Generation != 7 {
		t.Fatalf("expected generation to advance on retry, got %#v", assignment)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPostgresClusterReplicationAssignmentRepositoryUpdateStateDetectsGenerationMismatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewPostgresClusterReplicationAssignmentRepository(db)
	leaseExpiresAt := time.Date(2026, 3, 13, 9, 1, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
		UPDATE cluster_replication_assignments
		SET state = $4,
		    generation = CASE
		    	WHEN state IN ('released', 'error')
		    	 AND $4 = 'pending'
		    	THEN generation + 1
		    	ELSE generation
		    END,
		    lease_expires_at = $5,
		    last_reconcile_job_id = $6,
		    last_error = $7,
		    updated_at = TIMEZONE('UTC', NOW())
		WHERE active_node_id = $1
		  AND standby_node_id = $2
		  AND generation = $3
		RETURNING id, generation, created_at, updated_at
	`)).
		WithArgs("node-a", "node-b", int64(6), cluster.AssignmentStateReplicating, leaseExpiresAt, nil, nil).
		WillReturnError(sql.ErrNoRows)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, active_node_id, standby_node_id, state, generation,
		       lease_expires_at, last_reconcile_job_id, last_error,
		       created_at, updated_at
		FROM cluster_replication_assignments
		WHERE active_node_id = $1
		  AND standby_node_id = $2
		LIMIT 1
	`)).
		WithArgs("node-a", "node-b").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "active_node_id", "standby_node_id", "state", "generation",
			"lease_expires_at", "last_reconcile_job_id", "last_error",
			"created_at", "updated_at",
		}).AddRow(
			int64(31), "node-a", "node-b", cluster.AssignmentStateReconciling, int64(7),
			leaseExpiresAt, nil, nil, leaseExpiresAt.Add(-time.Minute), leaseExpiresAt,
		))

	assignment := &cluster.ReplicationAssignment{
		ActiveNodeID:   "node-a",
		StandbyNodeID:  "node-b",
		State:          cluster.AssignmentStateReplicating,
		Generation:     6,
		LeaseExpiresAt: &leaseExpiresAt,
	}
	err = repo.UpdateState(context.Background(), assignment)
	if !errors.Is(err, cluster.ErrReplicationAssignmentGenerationMismatch) {
		t.Fatalf("expected generation mismatch, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPostgresClusterReplicationAssignmentRepositoryReleaseByActiveExcept(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewPostgresClusterReplicationAssignmentRepository(db)
	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE cluster_replication_assignments
		SET state = $2,
		    lease_expires_at = CASE
		    	WHEN lease_expires_at IS NULL OR lease_expires_at > TIMEZONE('UTC', NOW()) THEN TIMEZONE('UTC', NOW())
		    	ELSE lease_expires_at
		    END,
		    updated_at = TIMEZONE('UTC', NOW())
		WHERE active_node_id = $1
		  AND state IN ('pending', 'reconciling', 'replicating', 'draining')
		  AND standby_node_id NOT IN ($3, $4)`)).
		WithArgs("node-a", cluster.AssignmentStateReleased, "node-b", "node-c").
		WillReturnResult(sqlmock.NewResult(0, 2))

	if err := repo.ReleaseByActiveExcept(context.Background(), "node-a", []string{"node-b", "node-c"}); err != nil {
		t.Fatalf("ReleaseByActiveExcept: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
