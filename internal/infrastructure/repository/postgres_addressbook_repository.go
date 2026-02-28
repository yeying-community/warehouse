package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"
	"github.com/yeying-community/warehouse/internal/domain/addressbook"
)

type AddressBookRepository interface {
	CreateGroup(ctx context.Context, group *addressbook.Group) error
	GetGroupByID(ctx context.Context, userID, groupID string) (*addressbook.Group, error)
	ListGroupsByUser(ctx context.Context, userID string) ([]*addressbook.Group, error)
	UpdateGroupName(ctx context.Context, userID, groupID, name string) error
	DeleteGroup(ctx context.Context, userID, groupID string) error

	CreateContact(ctx context.Context, contact *addressbook.Contact) error
	GetContactByID(ctx context.Context, userID, contactID string) (*addressbook.Contact, error)
	ListContactsByUser(ctx context.Context, userID string) ([]*addressbook.Contact, error)
	UpdateContact(ctx context.Context, contact *addressbook.Contact) error
	DeleteContact(ctx context.Context, userID, contactID string) error
}

type PostgresAddressBookRepository struct {
	db *sql.DB
}

func NewPostgresAddressBookRepository(db *sql.DB) *PostgresAddressBookRepository {
	return &PostgresAddressBookRepository{db: db}
}

func (r *PostgresAddressBookRepository) CreateGroup(ctx context.Context, group *addressbook.Group) error {
	query := `
		INSERT INTO address_groups (id, user_id, name, created_at)
		VALUES ($1, $2, $3, $4)
	`
	_, err := r.db.ExecContext(ctx, query, group.ID, group.UserID, group.Name, group.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "idx_address_groups_user_name") {
			return addressbook.ErrDuplicateGroupName
		}
		return fmt.Errorf("failed to create group: %w", err)
	}
	return nil
}

func (r *PostgresAddressBookRepository) GetGroupByID(ctx context.Context, userID, groupID string) (*addressbook.Group, error) {
	query := `
		SELECT id, user_id, name, created_at
		FROM address_groups
		WHERE id = $1 AND user_id = $2
	`
	group := &addressbook.Group{}
	err := r.db.QueryRowContext(ctx, query, groupID, userID).Scan(
		&group.ID,
		&group.UserID,
		&group.Name,
		&group.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, addressbook.ErrGroupNotFound
		}
		return nil, fmt.Errorf("failed to get group: %w", err)
	}
	return group, nil
}

func (r *PostgresAddressBookRepository) ListGroupsByUser(ctx context.Context, userID string) ([]*addressbook.Group, error) {
	query := `
		SELECT id, user_id, name, created_at
		FROM address_groups
		WHERE user_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query groups: %w", err)
	}
	defer rows.Close()

	var groups []*addressbook.Group
	for rows.Next() {
		group := &addressbook.Group{}
		if err := rows.Scan(&group.ID, &group.UserID, &group.Name, &group.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan group: %w", err)
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate groups: %w", err)
	}
	return groups, nil
}

func (r *PostgresAddressBookRepository) UpdateGroupName(ctx context.Context, userID, groupID, name string) error {
	query := `UPDATE address_groups SET name = $1 WHERE id = $2 AND user_id = $3`
	result, err := r.db.ExecContext(ctx, query, name, groupID, userID)
	if err != nil {
		if strings.Contains(err.Error(), "idx_address_groups_user_name") {
			return addressbook.ErrDuplicateGroupName
		}
		return fmt.Errorf("failed to update group: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if rows == 0 {
		return addressbook.ErrGroupNotFound
	}
	return nil
}

func (r *PostgresAddressBookRepository) DeleteGroup(ctx context.Context, userID, groupID string) error {
	query := `DELETE FROM address_groups WHERE id = $1 AND user_id = $2`
	result, err := r.db.ExecContext(ctx, query, groupID, userID)
	if err != nil {
		return fmt.Errorf("failed to delete group: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if rows == 0 {
		return addressbook.ErrGroupNotFound
	}
	return nil
}

func (r *PostgresAddressBookRepository) CreateContact(ctx context.Context, contact *addressbook.Contact) error {
	query := `
		INSERT INTO address_contacts (id, user_id, group_id, name, wallet_address, tags, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	var groupID interface{}
	if strings.TrimSpace(contact.GroupID) != "" {
		groupID = contact.GroupID
	} else {
		groupID = nil
	}
	_, err := r.db.ExecContext(ctx, query,
		contact.ID,
		contact.UserID,
		groupID,
		contact.Name,
		contact.WalletAddress,
		pq.Array(contact.Tags),
		contact.CreatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "idx_address_contacts_user_wallet") {
			return addressbook.ErrDuplicateWallet
		}
		return fmt.Errorf("failed to create contact: %w", err)
	}
	return nil
}

func (r *PostgresAddressBookRepository) GetContactByID(ctx context.Context, userID, contactID string) (*addressbook.Contact, error) {
	query := `
		SELECT id, user_id, group_id, name, wallet_address, tags, created_at
		FROM address_contacts
		WHERE id = $1 AND user_id = $2
	`
	contact := &addressbook.Contact{}
	var groupID sql.NullString
	var tags []string
	err := r.db.QueryRowContext(ctx, query, contactID, userID).Scan(
		&contact.ID,
		&contact.UserID,
		&groupID,
		&contact.Name,
		&contact.WalletAddress,
		pq.Array(&tags),
		&contact.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, addressbook.ErrContactNotFound
		}
		return nil, fmt.Errorf("failed to get contact: %w", err)
	}
	if groupID.Valid {
		contact.GroupID = groupID.String
	}
	contact.Tags = tags
	return contact, nil
}

func (r *PostgresAddressBookRepository) ListContactsByUser(ctx context.Context, userID string) ([]*addressbook.Contact, error) {
	query := `
		SELECT id, user_id, group_id, name, wallet_address, tags, created_at
		FROM address_contacts
		WHERE user_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query contacts: %w", err)
	}
	defer rows.Close()

	var contacts []*addressbook.Contact
	for rows.Next() {
		contact := &addressbook.Contact{}
		var groupID sql.NullString
		var tags []string
		if err := rows.Scan(
			&contact.ID,
			&contact.UserID,
			&groupID,
			&contact.Name,
			&contact.WalletAddress,
			pq.Array(&tags),
			&contact.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan contact: %w", err)
		}
		if groupID.Valid {
			contact.GroupID = groupID.String
		}
		contact.Tags = tags
		contacts = append(contacts, contact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate contacts: %w", err)
	}
	return contacts, nil
}

func (r *PostgresAddressBookRepository) UpdateContact(ctx context.Context, contact *addressbook.Contact) error {
	query := `
		UPDATE address_contacts
		SET group_id = $1, name = $2, wallet_address = $3, tags = $4
		WHERE id = $5 AND user_id = $6
	`
	var groupID interface{}
	if strings.TrimSpace(contact.GroupID) != "" {
		groupID = contact.GroupID
	} else {
		groupID = nil
	}
	result, err := r.db.ExecContext(ctx, query, groupID, contact.Name, contact.WalletAddress, pq.Array(contact.Tags), contact.ID, contact.UserID)
	if err != nil {
		if strings.Contains(err.Error(), "idx_address_contacts_user_wallet") {
			return addressbook.ErrDuplicateWallet
		}
		return fmt.Errorf("failed to update contact: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if rows == 0 {
		return addressbook.ErrContactNotFound
	}
	return nil
}

func (r *PostgresAddressBookRepository) DeleteContact(ctx context.Context, userID, contactID string) error {
	query := `DELETE FROM address_contacts WHERE id = $1 AND user_id = $2`
	result, err := r.db.ExecContext(ctx, query, contactID, userID)
	if err != nil {
		return fmt.Errorf("failed to delete contact: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if rows == 0 {
		return addressbook.ErrContactNotFound
	}
	return nil
}
