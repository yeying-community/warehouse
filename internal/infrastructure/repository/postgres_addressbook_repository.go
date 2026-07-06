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
	ListVisibleGroups(ctx context.Context, userID, walletAddress string) ([]*addressbook.Group, error)
	UpdateGroupName(ctx context.Context, userID, groupID, name string) error
	DeleteGroup(ctx context.Context, userID, groupID string) error

	CreateMember(ctx context.Context, member *addressbook.Member) error
	GetMemberByID(ctx context.Context, userID, memberID string) (*addressbook.Member, error)
	ListVisibleMembers(ctx context.Context, userID, walletAddress string) ([]*addressbook.Member, error)
	UpdateMember(ctx context.Context, member *addressbook.Member) error
	DeleteMember(ctx context.Context, userID, memberID string) error
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

func (r *PostgresAddressBookRepository) ListVisibleGroups(ctx context.Context, userID, walletAddress string) ([]*addressbook.Group, error) {
	query := `
		SELECT DISTINCT g.id, g.user_id, g.name, g.created_at
		FROM address_groups g
		LEFT JOIN group_members member
			ON member.group_id = g.id
			AND LOWER(member.wallet_address) = LOWER($2)
		WHERE g.user_id = $1
			OR ($2 <> '' AND member.id IS NOT NULL)
		ORDER BY g.created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID, strings.TrimSpace(walletAddress))
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

func (r *PostgresAddressBookRepository) CreateMember(ctx context.Context, member *addressbook.Member) error {
	query := `
		INSERT INTO group_members (id, user_id, group_id, name, wallet_address, tags, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := r.db.ExecContext(ctx, query,
		member.ID,
		member.UserID,
		member.GroupID,
		member.Name,
		member.WalletAddress,
		pq.Array(member.Tags),
		member.CreatedAt,
	)
	if err != nil {
		if isDuplicateMemberError(err) {
			return addressbook.ErrDuplicateMember
		}
		return fmt.Errorf("failed to create member: %w", err)
	}
	return nil
}

func (r *PostgresAddressBookRepository) GetMemberByID(ctx context.Context, userID, memberID string) (*addressbook.Member, error) {
	query := `
		SELECT id, user_id, group_id, name, wallet_address, tags, created_at
		FROM group_members
		WHERE id = $1 AND user_id = $2
	`
	member := &addressbook.Member{}
	var tags []string
	err := r.db.QueryRowContext(ctx, query, memberID, userID).Scan(
		&member.ID,
		&member.UserID,
		&member.GroupID,
		&member.Name,
		&member.WalletAddress,
		pq.Array(&tags),
		&member.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, addressbook.ErrMemberNotFound
		}
		return nil, fmt.Errorf("failed to get member: %w", err)
	}
	member.Tags = tags
	return member, nil
}

func (r *PostgresAddressBookRepository) ListVisibleMembers(ctx context.Context, userID, walletAddress string) ([]*addressbook.Member, error) {
	query := `
		SELECT DISTINCT m.id, m.user_id, m.group_id, m.name, m.wallet_address, m.tags, m.created_at
		FROM group_members m
		JOIN address_groups g ON g.id = m.group_id
		LEFT JOIN group_members current_member
			ON current_member.group_id = g.id
			AND LOWER(current_member.wallet_address) = LOWER($2)
		WHERE g.user_id = $1
			OR ($2 <> '' AND current_member.id IS NOT NULL)
		ORDER BY m.created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID, strings.TrimSpace(walletAddress))
	if err != nil {
		return nil, fmt.Errorf("failed to query members: %w", err)
	}
	defer rows.Close()

	var members []*addressbook.Member
	for rows.Next() {
		member := &addressbook.Member{}
		var tags []string
		if err := rows.Scan(
			&member.ID,
			&member.UserID,
			&member.GroupID,
			&member.Name,
			&member.WalletAddress,
			pq.Array(&tags),
			&member.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan member: %w", err)
		}
		member.Tags = tags
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate members: %w", err)
	}
	return members, nil
}

func (r *PostgresAddressBookRepository) UpdateMember(ctx context.Context, member *addressbook.Member) error {
	query := `
		UPDATE group_members
		SET group_id = $1, name = $2, wallet_address = $3, tags = $4
		WHERE id = $5 AND user_id = $6
	`
	result, err := r.db.ExecContext(ctx, query, member.GroupID, member.Name, member.WalletAddress, pq.Array(member.Tags), member.ID, member.UserID)
	if err != nil {
		if isDuplicateMemberError(err) {
			return addressbook.ErrDuplicateMember
		}
		return fmt.Errorf("failed to update member: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if rows == 0 {
		return addressbook.ErrMemberNotFound
	}
	return nil
}

func (r *PostgresAddressBookRepository) DeleteMember(ctx context.Context, userID, memberID string) error {
	query := `DELETE FROM group_members WHERE id = $1 AND user_id = $2`
	result, err := r.db.ExecContext(ctx, query, memberID, userID)
	if err != nil {
		return fmt.Errorf("failed to delete member: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if rows == 0 {
		return addressbook.ErrMemberNotFound
	}
	return nil
}

func isDuplicateMemberError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "idx_group_members_user_group_wallet")
}
