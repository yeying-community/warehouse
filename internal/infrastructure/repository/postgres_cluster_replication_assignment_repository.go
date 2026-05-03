package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/yeying-community/warehouse/internal/domain/cluster"
)

const defaultClusterAssignmentLimit = 20

// ClusterReplicationAssignmentFilter is used to query assignment status rows.
type ClusterReplicationAssignmentFilter struct {
	ActiveNodeID  string
	StandbyNodeID string
	State         string
	Limit         int
}

// ClusterReplicationAssignmentRepository reads control-plane assignment rows.
type ClusterReplicationAssignmentRepository interface {
	List(ctx context.Context, filter ClusterReplicationAssignmentFilter) ([]*cluster.ReplicationAssignment, error)
	ListEffectiveByActive(ctx context.Context, activeNodeID string) ([]*cluster.ReplicationAssignment, error)
	GetEffectiveByStandby(ctx context.Context, standbyNodeID string) (*cluster.ReplicationAssignment, error)
	GetByPair(ctx context.Context, activeNodeID, standbyNodeID string) (*cluster.ReplicationAssignment, error)
	UpsertLease(ctx context.Context, assignment *cluster.ReplicationAssignment) error
	UpdateState(ctx context.Context, assignment *cluster.ReplicationAssignment) error
	ReleaseByActiveExcept(ctx context.Context, activeNodeID string, keepStandbyIDs []string) error
}

// PostgresClusterReplicationAssignmentRepository is the PostgreSQL implementation.
type PostgresClusterReplicationAssignmentRepository struct {
	db *sql.DB
}

// NewPostgresClusterReplicationAssignmentRepository creates an assignment repository.
func NewPostgresClusterReplicationAssignmentRepository(db *sql.DB) *PostgresClusterReplicationAssignmentRepository {
	return &PostgresClusterReplicationAssignmentRepository{db: db}
}

// List returns assignment rows ordered by freshest updates first.
func (r *PostgresClusterReplicationAssignmentRepository) List(ctx context.Context, filter ClusterReplicationAssignmentFilter) ([]*cluster.ReplicationAssignment, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultClusterAssignmentLimit
	}

	query := `
		SELECT id, active_node_id, standby_node_id, state, generation,
		       lease_expires_at, last_reconcile_job_id, last_error, failure_count, next_retry_at,
		       created_at, updated_at
		FROM cluster_replication_assignments
	`
	clauses := make([]string, 0, 3)
	args := make([]any, 0, 4)

	if activeNodeID := strings.TrimSpace(filter.ActiveNodeID); activeNodeID != "" {
		args = append(args, activeNodeID)
		clauses = append(clauses, fmt.Sprintf("active_node_id = $%d", len(args)))
	}
	if standbyNodeID := strings.TrimSpace(filter.StandbyNodeID); standbyNodeID != "" {
		args = append(args, standbyNodeID)
		clauses = append(clauses, fmt.Sprintf("standby_node_id = $%d", len(args)))
	}
	if state := strings.TrimSpace(filter.State); state != "" {
		args = append(args, state)
		clauses = append(clauses, fmt.Sprintf("state = $%d", len(args)))
	}
	if len(clauses) > 0 {
		query += "\nWHERE " + strings.Join(clauses, "\n  AND ")
	}

	args = append(args, limit)
	query += fmt.Sprintf("\nORDER BY updated_at DESC, id DESC\nLIMIT $%d", len(args))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list cluster replication assignments: %w", err)
	}
	defer rows.Close()

	assignments := make([]*cluster.ReplicationAssignment, 0, limit)
	for rows.Next() {
		assignment, err := scanClusterReplicationAssignment(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan cluster replication assignment: %w", err)
		}
		assignments = append(assignments, assignment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate cluster replication assignments: %w", err)
	}
	return assignments, nil
}

// ListEffectiveByActive loads effective assignments for one active node.
func (r *PostgresClusterReplicationAssignmentRepository) ListEffectiveByActive(ctx context.Context, activeNodeID string) ([]*cluster.ReplicationAssignment, error) {
	query := `
		SELECT id, active_node_id, standby_node_id, state, generation,
		       lease_expires_at, last_reconcile_job_id, last_error, failure_count, next_retry_at,
		       created_at, updated_at
		FROM cluster_replication_assignments
		WHERE active_node_id = $1
		  AND state IN ('pending', 'reconciling', 'replicating', 'draining')
		ORDER BY updated_at DESC, id DESC
	`
	rows, err := r.db.QueryContext(ctx, query, strings.TrimSpace(activeNodeID))
	if err != nil {
		return nil, fmt.Errorf("failed to list effective cluster replication assignments: %w", err)
	}
	defer rows.Close()

	assignments := make([]*cluster.ReplicationAssignment, 0, 4)
	for rows.Next() {
		assignment, err := scanClusterReplicationAssignment(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan effective cluster replication assignment: %w", err)
		}
		assignments = append(assignments, assignment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate effective cluster replication assignments: %w", err)
	}
	return assignments, nil
}

// GetEffectiveByStandby loads the current effective assignment for one standby node.
func (r *PostgresClusterReplicationAssignmentRepository) GetEffectiveByStandby(ctx context.Context, standbyNodeID string) (*cluster.ReplicationAssignment, error) {
	query := `
		SELECT id, active_node_id, standby_node_id, state, generation,
		       lease_expires_at, last_reconcile_job_id, last_error, failure_count, next_retry_at,
		       created_at, updated_at
		FROM cluster_replication_assignments
		WHERE standby_node_id = $1
		  AND state IN ('pending', 'reconciling', 'replicating', 'draining')
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
	`
	row := r.db.QueryRowContext(ctx, query, strings.TrimSpace(standbyNodeID))
	assignment, err := scanClusterReplicationAssignment(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get effective cluster replication assignment by standby: %w", err)
	}
	return assignment, nil
}

// GetByPair loads one assignment row by active/standby node id pair.
func (r *PostgresClusterReplicationAssignmentRepository) GetByPair(ctx context.Context, activeNodeID, standbyNodeID string) (*cluster.ReplicationAssignment, error) {
	query := `
		SELECT id, active_node_id, standby_node_id, state, generation,
		       lease_expires_at, last_reconcile_job_id, last_error, failure_count, next_retry_at,
		       created_at, updated_at
		FROM cluster_replication_assignments
		WHERE active_node_id = $1
		  AND standby_node_id = $2
		LIMIT 1
	`
	row := r.db.QueryRowContext(ctx, query, strings.TrimSpace(activeNodeID), strings.TrimSpace(standbyNodeID))
	assignment, err := scanClusterReplicationAssignment(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster replication assignment by pair: %w", err)
	}
	return assignment, nil
}

// UpsertLease inserts or renews one assignment lease row.
func (r *PostgresClusterReplicationAssignmentRepository) UpsertLease(ctx context.Context, assignment *cluster.ReplicationAssignment) error {
	if assignment == nil {
		return fmt.Errorf("cluster replication assignment is required")
	}

	assignment.ActiveNodeID = strings.TrimSpace(assignment.ActiveNodeID)
	assignment.StandbyNodeID = strings.TrimSpace(assignment.StandbyNodeID)
	assignment.State = strings.TrimSpace(assignment.State)
	if assignment.ActiveNodeID == "" {
		return fmt.Errorf("cluster replication assignment active node id is required")
	}
	if assignment.StandbyNodeID == "" {
		return fmt.Errorf("cluster replication assignment standby node id is required")
	}
	if assignment.State == "" {
		return fmt.Errorf("cluster replication assignment state is required")
	}
	if assignment.LeaseExpiresAt == nil || assignment.LeaseExpiresAt.IsZero() {
		return fmt.Errorf("cluster replication assignment lease expiry is required")
	}

	query := `
		INSERT INTO cluster_replication_assignments (
			active_node_id, standby_node_id, state, lease_expires_at
		)
			VALUES ($1, $2, $3, $4)
		ON CONFLICT (active_node_id, standby_node_id)
		DO UPDATE SET
			state = EXCLUDED.state,
			generation = CASE
				WHEN cluster_replication_assignments.state IN ('released', 'error', 'paused')
				 AND EXCLUDED.state = 'pending'
				THEN cluster_replication_assignments.generation + 1
				ELSE cluster_replication_assignments.generation
			END,
			lease_expires_at = EXCLUDED.lease_expires_at,
			last_error = CASE
				WHEN EXCLUDED.state = 'pending' THEN NULL
				ELSE cluster_replication_assignments.last_error
			END,
			failure_count = CASE
				WHEN EXCLUDED.state = 'pending' THEN 0
				ELSE cluster_replication_assignments.failure_count
			END,
			next_retry_at = CASE
				WHEN EXCLUDED.state = 'pending' THEN NULL
				ELSE cluster_replication_assignments.next_retry_at
			END,
			updated_at = TIMEZONE('UTC', NOW())
		RETURNING id, generation, last_reconcile_job_id, last_error, failure_count, next_retry_at, created_at, updated_at
	`
	var lastReconcileJobID sql.NullInt64
	var lastError sql.NullString
	var nextRetryAt sql.NullTime
	if err := r.db.QueryRowContext(
		ctx,
		query,
		assignment.ActiveNodeID,
		assignment.StandbyNodeID,
		assignment.State,
		assignment.LeaseExpiresAt.UTC(),
	).Scan(
		&assignment.ID,
		&assignment.Generation,
		&lastReconcileJobID,
		&lastError,
		&assignment.FailureCount,
		&nextRetryAt,
		&assignment.CreatedAt,
		&assignment.UpdatedAt,
	); err != nil {
		return fmt.Errorf("failed to upsert cluster replication assignment lease: %w", err)
	}
	if lastReconcileJobID.Valid {
		value := lastReconcileJobID.Int64
		assignment.LastReconcileJobID = &value
	} else {
		assignment.LastReconcileJobID = nil
	}
	if lastError.Valid {
		value := lastError.String
		assignment.LastError = &value
	} else {
		assignment.LastError = nil
	}
	if nextRetryAt.Valid {
		value := nextRetryAt.Time.UTC()
		assignment.NextRetryAt = &value
	} else {
		assignment.NextRetryAt = nil
	}
	return nil
}

// UpdateState persists assignment lifecycle fields without changing generation.
func (r *PostgresClusterReplicationAssignmentRepository) UpdateState(ctx context.Context, assignment *cluster.ReplicationAssignment) error {
	if assignment == nil {
		return fmt.Errorf("cluster replication assignment is required")
	}

	assignment.Normalize()
	if assignment.ActiveNodeID == "" {
		return fmt.Errorf("cluster replication assignment active node id is required")
	}
	if assignment.StandbyNodeID == "" {
		return fmt.Errorf("cluster replication assignment standby node id is required")
	}
	if assignment.Generation <= 0 {
		return fmt.Errorf("cluster replication assignment generation is required")
	}
	if assignment.State == "" {
		return fmt.Errorf("cluster replication assignment state is required")
	}
	var leaseExpiresAt any
	if assignment.LeaseExpiresAt != nil && !assignment.LeaseExpiresAt.IsZero() {
		leaseExpiresAt = assignment.LeaseExpiresAt.UTC()
	}
	var nextRetryAt any
	if assignment.NextRetryAt != nil && !assignment.NextRetryAt.IsZero() {
		nextRetryAt = assignment.NextRetryAt.UTC()
	}

	query := `
		UPDATE cluster_replication_assignments
		SET state = $4,
		    generation = CASE
		    	WHEN state IN ('released', 'error', 'paused')
		    	 AND $4 = 'pending'
		    	THEN generation + 1
		    	ELSE generation
		    END,
		    lease_expires_at = $5,
		    last_reconcile_job_id = $6,
		    last_error = $7,
		    failure_count = $8,
		    next_retry_at = $9,
		    updated_at = TIMEZONE('UTC', NOW())
		WHERE active_node_id = $1
		  AND standby_node_id = $2
		  AND generation = $3
		RETURNING id, generation, created_at, updated_at
	`
	if err := r.db.QueryRowContext(
		ctx,
		query,
		assignment.ActiveNodeID,
		assignment.StandbyNodeID,
		assignment.Generation,
		assignment.State,
		leaseExpiresAt,
		assignment.LastReconcileJobID,
		assignment.LastError,
		assignment.FailureCount,
		nextRetryAt,
	).Scan(
		&assignment.ID,
		&assignment.Generation,
		&assignment.CreatedAt,
		&assignment.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			existing, lookupErr := r.GetByPair(ctx, assignment.ActiveNodeID, assignment.StandbyNodeID)
			if lookupErr != nil {
				return fmt.Errorf("failed to verify cluster replication assignment state update: %w", lookupErr)
			}
			if existing == nil {
				return cluster.ErrReplicationAssignmentNotFound
			}
			return cluster.ErrReplicationAssignmentGenerationMismatch
		}
		return fmt.Errorf("failed to update cluster replication assignment state: %w", err)
	}
	return nil
}

// ReleaseByActiveExcept releases all effective assignments except the retained standby set.
func (r *PostgresClusterReplicationAssignmentRepository) ReleaseByActiveExcept(ctx context.Context, activeNodeID string, keepStandbyIDs []string) error {
	activeNodeID = strings.TrimSpace(activeNodeID)
	if activeNodeID == "" {
		return fmt.Errorf("cluster replication assignment active node id is required")
	}

	query := `
		UPDATE cluster_replication_assignments
		SET state = $2,
		    lease_expires_at = CASE
		    	WHEN lease_expires_at IS NULL OR lease_expires_at > TIMEZONE('UTC', NOW()) THEN TIMEZONE('UTC', NOW())
		    	ELSE lease_expires_at
		    END,
		    updated_at = TIMEZONE('UTC', NOW())
		WHERE active_node_id = $1
		  AND state IN ('pending', 'reconciling', 'replicating', 'draining')
	`
	args := []any{activeNodeID, cluster.AssignmentStateReleased}
	keep := normalizeStringSet(keepStandbyIDs)
	if len(keep) > 0 {
		placeholders := make([]string, 0, len(keep))
		for _, standbyNodeID := range keep {
			args = append(args, standbyNodeID)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		query += "\n  AND standby_node_id NOT IN (" + strings.Join(placeholders, ", ") + ")"
	}

	if _, err := r.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("failed to release stale cluster replication assignments: %w", err)
	}
	return nil
}

func scanClusterReplicationAssignment(scanner interface {
	Scan(dest ...any) error
}) (*cluster.ReplicationAssignment, error) {
	assignment := &cluster.ReplicationAssignment{}
	var leaseExpiresAt sql.NullTime
	var lastReconcileJobID sql.NullInt64
	var lastError sql.NullString
	var nextRetryAt sql.NullTime
	if err := scanner.Scan(
		&assignment.ID,
		&assignment.ActiveNodeID,
		&assignment.StandbyNodeID,
		&assignment.State,
		&assignment.Generation,
		&leaseExpiresAt,
		&lastReconcileJobID,
		&lastError,
		&assignment.FailureCount,
		&nextRetryAt,
		&assignment.CreatedAt,
		&assignment.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if leaseExpiresAt.Valid {
		value := leaseExpiresAt.Time.UTC()
		assignment.LeaseExpiresAt = &value
	}
	if lastReconcileJobID.Valid {
		value := lastReconcileJobID.Int64
		assignment.LastReconcileJobID = &value
	}
	if lastError.Valid {
		value := lastError.String
		assignment.LastError = &value
	}
	if nextRetryAt.Valid {
		value := nextRetryAt.Time.UTC()
		assignment.NextRetryAt = &value
	}
	return assignment, nil
}

func normalizeStringSet(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized
}
