package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/yeying-community/warehouse/internal/domain/shareuser"
)

func TestDeleteByIDDeletesGrantBeforeShareInTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM internal_share_grants WHERE legacy_share_id = \\$1").
		WithArgs("share-1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM internal_share_items WHERE id = \\$1").
		WithArgs("share-1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := NewPostgresUserShareRepository(db).DeleteByID(context.Background(), "share-1"); err != nil {
		t.Fatalf("DeleteByID returned error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteByIDMissingShareRollsBack(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM internal_share_grants WHERE legacy_share_id = \\$1").
		WithArgs("missing").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM internal_share_items WHERE id = \\$1").
		WithArgs("missing").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	if err := NewPostgresUserShareRepository(db).DeleteByID(context.Background(), "missing"); err != shareuser.ErrShareNotFound {
		t.Fatalf("expected ErrShareNotFound, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
