package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/replication"
)

const defaultReplicationPendingLimit = 100

// ReplicationOutboxRepository stores durable replication events.
type ReplicationOutboxRepository interface {
	Append(ctx context.Context, event *replication.OutboxEvent) error
	AppendBatch(ctx context.Context, events []*replication.OutboxEvent) error
	ListPending(ctx context.Context, sourceNodeID, targetNodeID string, assignmentGeneration *int64, limit int) ([]*replication.OutboxEvent, error)
	MarkDispatched(ctx context.Context, id int64, dispatchedAt time.Time) error
	MarkFailed(ctx context.Context, id int64, lastError string, nextRetryAt time.Time) error
	GetStatusSummary(ctx context.Context, sourceNodeID, targetNodeID string) (*replication.OutboxStatus, error)
}

// ReplicationOffsetRepository stores apply progress for a source->target pair.
type ReplicationOffsetRepository interface {
	Upsert(ctx context.Context, offset *replication.Offset) error
	Get(ctx context.Context, sourceNodeID, targetNodeID string) (*replication.Offset, error)
}

// ReplicationReconcileRepository stores historical reconcile jobs and pending items.
type ReplicationReconcileRepository interface {
	CreateJob(ctx context.Context, job *replication.ReconcileJob) error
	ReplaceItems(ctx context.Context, jobID int64, items []*replication.ReconcileItem) error
	UpdateJobResult(ctx context.Context, jobID int64, status string, scannedItems, pendingItems int64, completedAt *time.Time, lastError *string) error
	GetLatestJob(ctx context.Context, sourceNodeID, targetNodeID string) (*replication.ReconcileJob, error)
	ListPendingItems(ctx context.Context, jobID int64, limit int) ([]*replication.ReconcileItem, error)
	UpdateItemsState(ctx context.Context, itemIDs []int64, state string) error
	CountPendingItems(ctx context.Context, jobID int64) (int64, error)
}

// PostgresReplicationOutboxRepository is the PostgreSQL implementation.
type PostgresReplicationOutboxRepository struct {
	db *sql.DB
}

// PostgresReplicationOffsetRepository is the PostgreSQL implementation.
type PostgresReplicationOffsetRepository struct {
	db *sql.DB
}

// PostgresReplicationReconcileRepository is the PostgreSQL implementation.
type PostgresReplicationReconcileRepository struct {
	db *sql.DB
}

// NewPostgresReplicationOutboxRepository creates an outbox repository.
func NewPostgresReplicationOutboxRepository(db *sql.DB) *PostgresReplicationOutboxRepository {
	return &PostgresReplicationOutboxRepository{db: db}
}

// NewPostgresReplicationOffsetRepository creates an offset repository.
func NewPostgresReplicationOffsetRepository(db *sql.DB) *PostgresReplicationOffsetRepository {
	return &PostgresReplicationOffsetRepository{db: db}
}

// NewPostgresReplicationReconcileRepository creates a reconcile repository.
func NewPostgresReplicationReconcileRepository(db *sql.DB) *PostgresReplicationReconcileRepository {
	return &PostgresReplicationReconcileRepository{db: db}
}

// Append inserts a new durable replication event.
func (r *PostgresReplicationOutboxRepository) Append(ctx context.Context, event *replication.OutboxEvent) error {
	return r.AppendBatch(ctx, []*replication.OutboxEvent{event})
}

// AppendBatch inserts multiple durable replication events atomically.
func (r *PostgresReplicationOutboxRepository) AppendBatch(ctx context.Context, events []*replication.OutboxEvent) error {
	if len(events) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin replication outbox batch transaction: %w", err)
	}
	defer tx.Rollback()

	query := `
		INSERT INTO replication_outbox (
			source_node_id, target_node_id, op, path, from_path, to_path,
			is_dir, content_sha256, file_size, assignment_generation, status, next_retry_at, last_error
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id, status, attempt_count, next_retry_at, created_at, dispatched_at
	`

	for _, event := range events {
		if event == nil {
			return fmt.Errorf("replication outbox event is required")
		}

		status := strings.TrimSpace(event.Status)
		if status == "" {
			status = replication.StatusPending
		}
		if event.NextRetryAt.IsZero() {
			event.NextRetryAt = time.Now()
		}

		var dispatchedAt sql.NullTime
		err := tx.QueryRowContext(ctx, query,
			event.SourceNodeID,
			event.TargetNodeID,
			event.Op,
			event.Path,
			event.FromPath,
			event.ToPath,
			event.IsDir,
			event.ContentSHA256,
			event.FileSize,
			event.AssignmentGeneration,
			status,
			event.NextRetryAt,
			event.LastError,
		).Scan(
			&event.ID,
			&event.Status,
			&event.AttemptCount,
			&event.NextRetryAt,
			&event.CreatedAt,
			&dispatchedAt,
		)
		if err != nil {
			return fmt.Errorf("failed to append replication outbox event: %w", err)
		}
		if dispatchedAt.Valid {
			event.DispatchedAt = &dispatchedAt.Time
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit replication outbox batch transaction: %w", err)
	}

	return nil
}

// ListPending lists ready-to-dispatch events ordered by durable sequence id.
func (r *PostgresReplicationOutboxRepository) ListPending(
	ctx context.Context,
	sourceNodeID, targetNodeID string,
	assignmentGeneration *int64,
	limit int,
) ([]*replication.OutboxEvent, error) {
	if assignmentGeneration == nil || *assignmentGeneration <= 0 {
		return nil, fmt.Errorf("assignment generation is required")
	}
	if limit <= 0 {
		limit = defaultReplicationPendingLimit
	}

	query := `
		SELECT id, source_node_id, target_node_id, op, path, from_path, to_path,
		       is_dir, content_sha256, file_size, assignment_generation, status, attempt_count,
		       next_retry_at, last_error, created_at, dispatched_at
		FROM replication_outbox
		WHERE source_node_id = $1
		  AND target_node_id = $2
		  AND assignment_generation = $3
		  AND status IN ($4, $5)
		  AND next_retry_at <= NOW()
	`
	args := []any{
		sourceNodeID,
		targetNodeID,
		*assignmentGeneration,
		replication.StatusPending,
		replication.StatusFailed,
	}
	args = append(args, limit)
	query += fmt.Sprintf("\nORDER BY id ASC\nLIMIT $%d", len(args))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list pending replication events: %w", err)
	}
	defer rows.Close()

	var events []*replication.OutboxEvent
	for rows.Next() {
		event, err := scanOutboxEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate pending replication events: %w", err)
	}

	return events, nil
}

// MarkDispatched records that an event was delivered successfully.
func (r *PostgresReplicationOutboxRepository) MarkDispatched(ctx context.Context, id int64, dispatchedAt time.Time) error {
	if dispatchedAt.IsZero() {
		dispatchedAt = time.Now()
	}

	query := `
		UPDATE replication_outbox
		SET status = $2,
		    dispatched_at = $3,
		    last_error = NULL
		WHERE id = $1
	`

	result, err := r.db.ExecContext(ctx, query, id, replication.StatusDispatched, dispatchedAt)
	if err != nil {
		return fmt.Errorf("failed to mark replication event as dispatched: %w", err)
	}
	if err := ensureAffectedRows(result, replication.ErrOutboxEventNotFound); err != nil {
		return err
	}

	return nil
}

// MarkFailed records an attempt failure and schedules the next retry.
func (r *PostgresReplicationOutboxRepository) MarkFailed(ctx context.Context, id int64, lastError string, nextRetryAt time.Time) error {
	if nextRetryAt.IsZero() {
		nextRetryAt = time.Now()
	}

	query := `
		UPDATE replication_outbox
		SET status = $2,
		    attempt_count = attempt_count + 1,
		    next_retry_at = $3,
		    last_error = $4
		WHERE id = $1
	`

	result, err := r.db.ExecContext(ctx, query, id, replication.StatusFailed, nextRetryAt, strings.TrimSpace(lastError))
	if err != nil {
		return fmt.Errorf("failed to mark replication event as failed: %w", err)
	}
	if err := ensureAffectedRows(result, replication.ErrOutboxEventNotFound); err != nil {
		return err
	}

	return nil
}

// GetStatusSummary returns queue depth and lag hints for one source->target pair.
func (r *PostgresReplicationOutboxRepository) GetStatusSummary(ctx context.Context, sourceNodeID, targetNodeID string) (*replication.OutboxStatus, error) {
	query := `
		WITH pair_events AS (
			SELECT id, status, attempt_count, next_retry_at, last_error, created_at
			FROM replication_outbox
			WHERE source_node_id = $1
			  AND target_node_id = $2
		),
		last_failed AS (
			SELECT id, attempt_count, next_retry_at, last_error
			FROM pair_events
			WHERE status = $3
			ORDER BY id DESC
			LIMIT 1
		)
		SELECT
			COUNT(*) FILTER (WHERE status IN ($4, $3)) AS pending_events,
			COUNT(*) FILTER (WHERE status = $3) AS failed_events,
			MAX(id) AS last_outbox_id,
			MAX(id) FILTER (WHERE status = $5) AS last_dispatched_outbox_id,
			MIN(created_at) FILTER (WHERE status IN ($4, $3)) AS oldest_pending_created_at,
			(SELECT id FROM last_failed),
			(SELECT attempt_count FROM last_failed),
			(SELECT next_retry_at FROM last_failed),
			(SELECT last_error FROM last_failed)
		FROM pair_events
	`

	status := &replication.OutboxStatus{}
	var lastOutboxID sql.NullInt64
	var lastDispatchedID sql.NullInt64
	var oldestPendingCreatedAt sql.NullTime
	var lastFailedID sql.NullInt64
	var lastFailureAttempt sql.NullInt64
	var nextRetryAt sql.NullTime
	var lastError sql.NullString
	err := r.db.QueryRowContext(ctx, query,
		sourceNodeID,
		targetNodeID,
		replication.StatusFailed,
		replication.StatusPending,
		replication.StatusDispatched,
	).Scan(
		&status.PendingEvents,
		&status.FailedEvents,
		&lastOutboxID,
		&lastDispatchedID,
		&oldestPendingCreatedAt,
		&lastFailedID,
		&lastFailureAttempt,
		&nextRetryAt,
		&lastError,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get replication outbox status summary: %w", err)
	}
	if lastOutboxID.Valid {
		status.LastOutboxID = &lastOutboxID.Int64
	}
	if lastDispatchedID.Valid {
		status.LastDispatchedOutboxID = &lastDispatchedID.Int64
	}
	if oldestPendingCreatedAt.Valid {
		status.OldestPendingCreatedAt = &oldestPendingCreatedAt.Time
	}
	if lastFailedID.Valid {
		status.LastFailedOutboxID = &lastFailedID.Int64
	}
	if lastFailureAttempt.Valid {
		status.LastFailureAttempt = nullableInt(lastFailureAttempt)
	}
	if nextRetryAt.Valid {
		status.NextRetryAt = &nextRetryAt.Time
	}
	status.LastError = nullableString(lastError)

	return status, nil
}

// Upsert stores the latest apply progress for one source->target pair.
func (r *PostgresReplicationOffsetRepository) Upsert(ctx context.Context, offset *replication.Offset) error {
	updatedAt := offset.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}

	query := `
		INSERT INTO replication_offsets (
			source_node_id, target_node_id, assignment_generation, last_applied_outbox_id, last_applied_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (source_node_id, target_node_id)
		DO UPDATE SET
			assignment_generation = EXCLUDED.assignment_generation,
			last_applied_outbox_id = EXCLUDED.last_applied_outbox_id,
			last_applied_at = EXCLUDED.last_applied_at,
			updated_at = EXCLUDED.updated_at
	`

	_, err := r.db.ExecContext(ctx, query,
		offset.SourceNodeID,
		offset.TargetNodeID,
		offset.AssignmentGeneration,
		offset.LastAppliedOutboxID,
		offset.LastAppliedAt,
		updatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert replication offset: %w", err)
	}
	if offset.UpdatedAt.IsZero() {
		offset.UpdatedAt = updatedAt
	}

	return nil
}

// Get fetches the last applied sequence for one source->target pair.
func (r *PostgresReplicationOffsetRepository) Get(ctx context.Context, sourceNodeID, targetNodeID string) (*replication.Offset, error) {
	query := `
		SELECT source_node_id, target_node_id, assignment_generation, last_applied_outbox_id, last_applied_at, updated_at
		FROM replication_offsets
		WHERE source_node_id = $1 AND target_node_id = $2
	`

	offset := &replication.Offset{}
	var assignmentGeneration sql.NullInt64
	err := r.db.QueryRowContext(ctx, query, sourceNodeID, targetNodeID).Scan(
		&offset.SourceNodeID,
		&offset.TargetNodeID,
		&assignmentGeneration,
		&offset.LastAppliedOutboxID,
		&offset.LastAppliedAt,
		&offset.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, replication.ErrOffsetNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get replication offset: %w", err)
	}
	offset.AssignmentGeneration = nullableInt64(assignmentGeneration)

	return offset, nil
}

// CreateJob inserts one historical reconcile job.
func (r *PostgresReplicationReconcileRepository) CreateJob(ctx context.Context, job *replication.ReconcileJob) error {
	startedAt := job.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}

	query := `
		INSERT INTO replication_reconcile_jobs (
			source_node_id, target_node_id, assignment_generation, watermark_outbox_id, status,
			scanned_items, pending_items, started_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at
	`
	err := r.db.QueryRowContext(ctx, query,
		job.SourceNodeID,
		job.TargetNodeID,
		job.AssignmentGeneration,
		job.WatermarkOutboxID,
		job.Status,
		job.ScannedItems,
		job.PendingItems,
		startedAt,
	).Scan(&job.ID, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create replication reconcile job: %w", err)
	}
	if job.StartedAt.IsZero() {
		job.StartedAt = startedAt
	}

	return nil
}

// ReplaceItems replaces all reconcile items for one job.
func (r *PostgresReplicationReconcileRepository) ReplaceItems(ctx context.Context, jobID int64, items []*replication.ReconcileItem) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin reconcile items transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM replication_reconcile_items WHERE job_id = $1`, jobID); err != nil {
		return fmt.Errorf("failed to clear reconcile items: %w", err)
	}

	if len(items) > 0 {
		query := `
			INSERT INTO replication_reconcile_items (
				job_id, path, is_dir, file_size, modified_at, state
			)
			VALUES ($1, $2, $3, $4, $5, $6)
		`
		for _, item := range items {
			if _, err := tx.ExecContext(ctx, query,
				jobID,
				item.Path,
				item.IsDir,
				item.FileSize,
				item.ModifiedAt,
				item.State,
			); err != nil {
				return fmt.Errorf("failed to insert reconcile item %q: %w", item.Path, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit reconcile items: %w", err)
	}
	return nil
}

// UpdateJobResult updates one reconcile job status and counters.
func (r *PostgresReplicationReconcileRepository) UpdateJobResult(ctx context.Context, jobID int64, status string, scannedItems, pendingItems int64, completedAt *time.Time, lastError *string) error {
	query := `
		UPDATE replication_reconcile_jobs
		SET status = $2,
			scanned_items = $3,
			pending_items = $4,
			completed_at = $5,
			last_error = $6,
			updated_at = NOW()
		WHERE id = $1
	`
	result, err := r.db.ExecContext(ctx, query, jobID, status, scannedItems, pendingItems, completedAt, lastError)
	if err != nil {
		return fmt.Errorf("failed to update reconcile job result: %w", err)
	}
	if err := ensureAffectedRows(result, replication.ErrReconcileJobNotFound); err != nil {
		return err
	}
	return nil
}

// GetLatestJob loads the latest reconcile job for one source->target pair.
func (r *PostgresReplicationReconcileRepository) GetLatestJob(ctx context.Context, sourceNodeID, targetNodeID string) (*replication.ReconcileJob, error) {
	query := `
		SELECT id, source_node_id, target_node_id, assignment_generation, watermark_outbox_id, status,
		       scanned_items, pending_items, started_at, completed_at, last_error,
		       created_at, updated_at
		FROM replication_reconcile_jobs
		WHERE source_node_id = $1
		  AND target_node_id = $2
		ORDER BY id DESC
		LIMIT 1
	`

	job := &replication.ReconcileJob{}
	var assignmentGeneration sql.NullInt64
	var completedAt sql.NullTime
	var lastError sql.NullString
	err := r.db.QueryRowContext(ctx, query, sourceNodeID, targetNodeID).Scan(
		&job.ID,
		&job.SourceNodeID,
		&job.TargetNodeID,
		&assignmentGeneration,
		&job.WatermarkOutboxID,
		&job.Status,
		&job.ScannedItems,
		&job.PendingItems,
		&job.StartedAt,
		&completedAt,
		&lastError,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, replication.ErrReconcileJobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get latest reconcile job: %w", err)
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}
	job.AssignmentGeneration = nullableInt64(assignmentGeneration)
	job.LastError = nullableString(lastError)
	return job, nil
}

// ListPendingItems returns pending reconcile items for one job.
func (r *PostgresReplicationReconcileRepository) ListPendingItems(ctx context.Context, jobID int64, limit int) ([]*replication.ReconcileItem, error) {
	if limit <= 0 {
		limit = defaultReplicationPendingLimit
	}
	query := `
		SELECT id, job_id, path, is_dir, file_size, modified_at, state, created_at, updated_at
		FROM replication_reconcile_items
		WHERE job_id = $1
		  AND state = $2
		ORDER BY id ASC
		LIMIT $3
	`
	rows, err := r.db.QueryContext(ctx, query, jobID, replication.ReconcileItemStatePending, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list pending reconcile items: %w", err)
	}
	defer rows.Close()

	items := make([]*replication.ReconcileItem, 0, limit)
	for rows.Next() {
		item := &replication.ReconcileItem{}
		var fileSize sql.NullInt64
		var modifiedAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.JobID,
			&item.Path,
			&item.IsDir,
			&fileSize,
			&modifiedAt,
			&item.State,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan reconcile item: %w", err)
		}
		item.FileSize = nullableInt64(fileSize)
		if modifiedAt.Valid {
			item.ModifiedAt = &modifiedAt.Time
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate reconcile items: %w", err)
	}
	return items, nil
}

// UpdateItemsState updates reconcile items state by item IDs.
func (r *PostgresReplicationReconcileRepository) UpdateItemsState(ctx context.Context, itemIDs []int64, state string) error {
	if len(itemIDs) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin reconcile item state transaction: %w", err)
	}
	defer tx.Rollback()

	query := `
		UPDATE replication_reconcile_items
		SET state = $2, updated_at = NOW()
		WHERE id = $1
	`
	for _, itemID := range itemIDs {
		if _, err := tx.ExecContext(ctx, query, itemID, state); err != nil {
			return fmt.Errorf("failed to update reconcile item %d state: %w", itemID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit reconcile item state updates: %w", err)
	}
	return nil
}

// CountPendingItems returns pending item count for one job.
func (r *PostgresReplicationReconcileRepository) CountPendingItems(ctx context.Context, jobID int64) (int64, error) {
	query := `
		SELECT COUNT(*)
		FROM replication_reconcile_items
		WHERE job_id = $1
		  AND state = $2
	`
	var pending int64
	if err := r.db.QueryRowContext(ctx, query, jobID, replication.ReconcileItemStatePending).Scan(&pending); err != nil {
		return 0, fmt.Errorf("failed to count pending reconcile items: %w", err)
	}
	return pending, nil
}

func scanOutboxEvent(scanner interface {
	Scan(dest ...interface{}) error
}) (*replication.OutboxEvent, error) {
	event := &replication.OutboxEvent{}
	var path sql.NullString
	var fromPath sql.NullString
	var toPath sql.NullString
	var contentSHA256 sql.NullString
	var fileSize sql.NullInt64
	var assignmentGeneration sql.NullInt64
	var lastError sql.NullString
	var dispatchedAt sql.NullTime

	if err := scanner.Scan(
		&event.ID,
		&event.SourceNodeID,
		&event.TargetNodeID,
		&event.Op,
		&path,
		&fromPath,
		&toPath,
		&event.IsDir,
		&contentSHA256,
		&fileSize,
		&assignmentGeneration,
		&event.Status,
		&event.AttemptCount,
		&event.NextRetryAt,
		&lastError,
		&event.CreatedAt,
		&dispatchedAt,
	); err != nil {
		return nil, fmt.Errorf("failed to scan replication outbox event: %w", err)
	}

	event.Path = nullableString(path)
	event.FromPath = nullableString(fromPath)
	event.ToPath = nullableString(toPath)
	event.ContentSHA256 = nullableString(contentSHA256)
	event.FileSize = nullableInt64(fileSize)
	event.AssignmentGeneration = nullableInt64(assignmentGeneration)
	event.LastError = nullableString(lastError)
	if dispatchedAt.Valid {
		event.DispatchedAt = &dispatchedAt.Time
	}

	return event, nil
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	v := value.String
	return &v
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Int64
	return &v
}

func nullableInt(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	v := int(value.Int64)
	return &v
}

func ensureAffectedRows(result sql.Result, notFound error) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if rowsAffected == 0 {
		return notFound
	}
	return nil
}
