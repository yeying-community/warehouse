package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/yeying-community/warehouse/internal/domain/replication"
)

func TestPostgresReplicationOutboxRepositoryAppend(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewPostgresReplicationOutboxRepository(db)
	path := "/apps/demo/readme.txt"
	sha := "abc123"
	size := int64(128)
	nextRetryAt := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	createdAt := nextRetryAt.Add(5 * time.Second)

	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO replication_outbox (
			source_node_id, target_node_id, op, path, from_path, to_path,
			is_dir, content_sha256, file_size, assignment_generation, status, next_retry_at, last_error
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id, status, attempt_count, next_retry_at, created_at, dispatched_at
	`)).
		WithArgs(
			"node-a",
			"node-b",
			replication.OpUpsertFile,
			path,
			nil,
			nil,
			false,
			sha,
			size,
			nil,
			replication.StatusPending,
			nextRetryAt,
			nil,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status", "attempt_count", "next_retry_at", "created_at", "dispatched_at"}).
			AddRow(int64(7), replication.StatusPending, 0, nextRetryAt, createdAt, nil))

	event := &replication.OutboxEvent{
		SourceNodeID:  "node-a",
		TargetNodeID:  "node-b",
		Op:            replication.OpUpsertFile,
		Path:          &path,
		ContentSHA256: &sha,
		FileSize:      &size,
		NextRetryAt:   nextRetryAt,
	}
	if err := repo.Append(context.Background(), event); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if event.ID != 7 || event.Status != replication.StatusPending || event.AttemptCount != 0 {
		t.Fatalf("unexpected event after append: %#v", event)
	}
	if !event.CreatedAt.Equal(createdAt) {
		t.Fatalf("expected created_at %v, got %v", createdAt, event.CreatedAt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPostgresReplicationOutboxRepositoryListPending(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewPostgresReplicationOutboxRepository(db)
	nextRetryAt := time.Date(2026, 3, 8, 11, 0, 0, 0, time.UTC)
	createdAt := nextRetryAt.Add(-time.Minute)
	dispatchedAt := nextRetryAt.Add(2 * time.Minute)
	assignmentGeneration := int64(4)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, source_node_id, target_node_id, op, path, from_path, to_path,
		       is_dir, content_sha256, file_size, assignment_generation, status, attempt_count,
		       next_retry_at, last_error, created_at, dispatched_at
		FROM replication_outbox
		WHERE source_node_id = $1
		  AND target_node_id = $2
		  AND assignment_generation = $3
		  AND status IN ($4, $5)
		  AND next_retry_at <= NOW()
		ORDER BY id ASC
		LIMIT $6
	`)).
		WithArgs("node-a", "node-b", assignmentGeneration, replication.StatusPending, replication.StatusFailed, 50).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_node_id", "target_node_id", "op", "path", "from_path", "to_path",
			"is_dir", "content_sha256", "file_size", "assignment_generation", "status", "attempt_count",
			"next_retry_at", "last_error", "created_at", "dispatched_at",
		}).AddRow(
			int64(9), "node-a", "node-b", replication.OpMovePath, "/dst", "/src", "/dst",
			false, nil, nil, assignmentGeneration, replication.StatusFailed, 2,
			nextRetryAt, "boom", createdAt, dispatchedAt,
		))

	events, err := repo.ListPending(context.Background(), "node-a", "node-b", &assignmentGeneration, 50)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].FromPath == nil || *events[0].FromPath != "/src" {
		t.Fatalf("unexpected from_path: %#v", events[0].FromPath)
	}
	if events[0].ToPath == nil || *events[0].ToPath != "/dst" {
		t.Fatalf("unexpected to_path: %#v", events[0].ToPath)
	}
	if events[0].LastError == nil || *events[0].LastError != "boom" {
		t.Fatalf("unexpected last_error: %#v", events[0].LastError)
	}
	if events[0].DispatchedAt == nil || !events[0].DispatchedAt.Equal(dispatchedAt) {
		t.Fatalf("unexpected dispatched_at: %#v", events[0].DispatchedAt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPostgresReplicationOutboxRepositoryMarkFailed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewPostgresReplicationOutboxRepository(db)
	nextRetryAt := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE replication_outbox
		SET status = $2,
		    attempt_count = attempt_count + 1,
		    next_retry_at = $3,
		    last_error = $4
		WHERE id = $1
	`)).
		WithArgs(int64(9), replication.StatusFailed, nextRetryAt, "network timeout").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.MarkFailed(context.Background(), 9, "network timeout", nextRetryAt); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPostgresReplicationOutboxRepositoryGetStatusSummary(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewPostgresReplicationOutboxRepository(db)
	lastOutboxID := int64(12)
	lastDispatchedID := int64(11)
	oldestPending := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	lastFailedID := int64(10)
	nextRetryAt := oldestPending.Add(15 * time.Second)

	mock.ExpectQuery(regexp.QuoteMeta(`
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
	`)).
		WithArgs("node-a", "node-b", replication.StatusFailed, replication.StatusPending, replication.StatusDispatched).
		WillReturnRows(sqlmock.NewRows([]string{
			"pending_events",
			"failed_events",
			"last_outbox_id",
			"last_dispatched_outbox_id",
			"oldest_pending_created_at",
			"last_failed_outbox_id",
			"last_failure_attempt",
			"next_retry_at",
			"last_error",
		}).AddRow(
			int64(3),
			int64(1),
			lastOutboxID,
			lastDispatchedID,
			oldestPending,
			lastFailedID,
			int64(4),
			nextRetryAt,
			"network timeout",
		))

	summary, err := repo.GetStatusSummary(context.Background(), "node-a", "node-b")
	if err != nil {
		t.Fatalf("GetStatusSummary: %v", err)
	}
	if summary.PendingEvents != 3 || summary.FailedEvents != 1 {
		t.Fatalf("unexpected status counts: %#v", summary)
	}
	if summary.LastOutboxID == nil || *summary.LastOutboxID != lastOutboxID {
		t.Fatalf("unexpected last outbox id: %#v", summary.LastOutboxID)
	}
	if summary.LastDispatchedOutboxID == nil || *summary.LastDispatchedOutboxID != lastDispatchedID {
		t.Fatalf("unexpected last dispatched id: %#v", summary.LastDispatchedOutboxID)
	}
	if summary.OldestPendingCreatedAt == nil || !summary.OldestPendingCreatedAt.Equal(oldestPending) {
		t.Fatalf("unexpected oldest pending created_at: %#v", summary.OldestPendingCreatedAt)
	}
	if summary.LastFailedOutboxID == nil || *summary.LastFailedOutboxID != lastFailedID {
		t.Fatalf("unexpected last failed id: %#v", summary.LastFailedOutboxID)
	}
	if summary.LastFailureAttempt == nil || *summary.LastFailureAttempt != 4 {
		t.Fatalf("unexpected last failure attempt: %#v", summary.LastFailureAttempt)
	}
	if summary.NextRetryAt == nil || !summary.NextRetryAt.Equal(nextRetryAt) {
		t.Fatalf("unexpected next retry at: %#v", summary.NextRetryAt)
	}
	if summary.LastError == nil || *summary.LastError != "network timeout" {
		t.Fatalf("unexpected last error: %#v", summary.LastError)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPostgresReplicationOffsetRepositoryUpsertAndGet(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewPostgresReplicationOffsetRepository(db)
	lastAppliedAt := time.Date(2026, 3, 8, 13, 0, 0, 0, time.UTC)
	updatedAt := lastAppliedAt.Add(10 * time.Second)

	mock.ExpectExec(regexp.QuoteMeta(`
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
	`)).
		WithArgs("node-a", "node-b", nil, int64(17), lastAppliedAt, updatedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))

	offset := &replication.Offset{
		SourceNodeID:        "node-a",
		TargetNodeID:        "node-b",
		LastAppliedOutboxID: 17,
		LastAppliedAt:       lastAppliedAt,
		UpdatedAt:           updatedAt,
	}
	if err := repo.Upsert(context.Background(), offset); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT source_node_id, target_node_id, assignment_generation, last_applied_outbox_id, last_applied_at, updated_at
		FROM replication_offsets
		WHERE source_node_id = $1 AND target_node_id = $2
	`)).
		WithArgs("node-a", "node-b").
		WillReturnRows(sqlmock.NewRows([]string{"source_node_id", "target_node_id", "assignment_generation", "last_applied_outbox_id", "last_applied_at", "updated_at"}).
			AddRow("node-a", "node-b", nil, int64(17), lastAppliedAt, updatedAt))

	loaded, err := repo.Get(context.Background(), "node-a", "node-b")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if loaded.LastAppliedOutboxID != 17 || !loaded.LastAppliedAt.Equal(lastAppliedAt) || !loaded.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("unexpected offset: %#v", loaded)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
