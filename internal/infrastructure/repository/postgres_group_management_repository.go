package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"
	"github.com/yeying-community/warehouse/internal/domain/groupmanagement"
)

type GroupManagementRepository interface {
	CreateGroup(ctx context.Context, group *groupmanagement.Group) error
	GetGroupByID(ctx context.Context, userID, groupID string) (*groupmanagement.Group, error)
	GetVisibleGroupByID(ctx context.Context, userID, walletAddress, groupID string) (*groupmanagement.Group, error)
	ListVisibleGroups(ctx context.Context, userID, walletAddress string) ([]*groupmanagement.Group, error)
	UpdateGroupName(ctx context.Context, userID, groupID, name string) error
	DeleteGroup(ctx context.Context, userID, groupID string) error

	CreateMember(ctx context.Context, member *groupmanagement.Member) error
	GetMemberByID(ctx context.Context, userID, memberID string) (*groupmanagement.Member, error)
	ListVisibleMembers(ctx context.Context, userID, walletAddress string) ([]*groupmanagement.Member, error)
	UpdateMember(ctx context.Context, member *groupmanagement.Member) error
	UpdateMemberStatus(ctx context.Context, userID, memberID, status string) error
	DeleteMember(ctx context.Context, userID, memberID string) error
}

type PostgresGroupManagementRepository struct {
	db *sql.DB
}

func NewPostgresGroupManagementRepository(db *sql.DB) *PostgresGroupManagementRepository {
	return &PostgresGroupManagementRepository{db: db}
}

func (r *PostgresGroupManagementRepository) CreateGroup(ctx context.Context, group *groupmanagement.Group) error {
	query := `
		INSERT INTO address_groups (id, user_id, name, created_at)
		VALUES ($1, $2, $3, $4)
	`
	_, err := r.db.ExecContext(ctx, query, group.ID, group.UserID, group.Name, group.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "idx_address_groups_user_name") {
			return groupmanagement.ErrDuplicateGroupName
		}
		return fmt.Errorf("failed to create group: %w", err)
	}
	return nil
}

func (r *PostgresGroupManagementRepository) GetGroupByID(ctx context.Context, userID, groupID string) (*groupmanagement.Group, error) {
	query := `
		SELECT id, user_id, name, created_at
		FROM address_groups
		WHERE id = $1 AND user_id = $2
	`
	group := &groupmanagement.Group{}
	err := r.db.QueryRowContext(ctx, query, groupID, userID).Scan(
		&group.ID,
		&group.UserID,
		&group.Name,
		&group.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, groupmanagement.ErrGroupNotFound
		}
		return nil, fmt.Errorf("failed to get group: %w", err)
	}
	return group, nil
}

func (r *PostgresGroupManagementRepository) GetVisibleGroupByID(ctx context.Context, userID, walletAddress, groupID string) (*groupmanagement.Group, error) {
	query := `
		SELECT DISTINCT g.id, g.user_id, g.name, g.created_at
		FROM address_groups g
		LEFT JOIN group_members member
			ON member.group_id = g.id
			AND member.status = $4
			AND LOWER(member.wallet_address) = LOWER($3)
		WHERE g.id = $1
			AND (g.user_id = $2 OR ($3 <> '' AND member.id IS NOT NULL))
	`
	group := &groupmanagement.Group{}
	err := r.db.QueryRowContext(ctx, query, groupID, userID, strings.TrimSpace(walletAddress), groupmanagement.MemberStatusActive).Scan(
		&group.ID,
		&group.UserID,
		&group.Name,
		&group.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, groupmanagement.ErrGroupNotFound
		}
		return nil, fmt.Errorf("failed to get visible group: %w", err)
	}
	return group, nil
}

func (r *PostgresGroupManagementRepository) ListVisibleGroups(ctx context.Context, userID, walletAddress string) ([]*groupmanagement.Group, error) {
	query := `
		SELECT DISTINCT g.id, g.user_id, g.name, g.created_at
		FROM address_groups g
		LEFT JOIN group_members member
			ON member.group_id = g.id
			AND member.status = $3
			AND LOWER(member.wallet_address) = LOWER($2)
		WHERE g.user_id = $1
			OR ($2 <> '' AND member.id IS NOT NULL)
		ORDER BY g.created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID, strings.TrimSpace(walletAddress), groupmanagement.MemberStatusActive)
	if err != nil {
		return nil, fmt.Errorf("failed to query groups: %w", err)
	}
	defer rows.Close()

	var groups []*groupmanagement.Group
	for rows.Next() {
		group := &groupmanagement.Group{}
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

func (r *PostgresGroupManagementRepository) UpdateGroupName(ctx context.Context, userID, groupID, name string) error {
	query := `UPDATE address_groups SET name = $1 WHERE id = $2 AND user_id = $3`
	result, err := r.db.ExecContext(ctx, query, name, groupID, userID)
	if err != nil {
		if strings.Contains(err.Error(), "idx_address_groups_user_name") {
			return groupmanagement.ErrDuplicateGroupName
		}
		return fmt.Errorf("failed to update group: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if rows == 0 {
		return groupmanagement.ErrGroupNotFound
	}
	return nil
}

func (r *PostgresGroupManagementRepository) DeleteGroup(ctx context.Context, userID, groupID string) error {
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
		return groupmanagement.ErrGroupNotFound
	}
	return nil
}

func (r *PostgresGroupManagementRepository) CreateMember(ctx context.Context, member *groupmanagement.Member) error {
	query := `
		INSERT INTO group_members (id, user_id, group_id, name, wallet_address, tags, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := r.db.ExecContext(ctx, query,
		member.ID,
		member.UserID,
		member.GroupID,
		member.Name,
		member.WalletAddress,
		pq.Array(member.Tags),
		groupmanagement.NormalizeMemberStatus(member.Status),
		member.CreatedAt,
	)
	if err != nil {
		if isDuplicateMemberError(err) {
			return groupmanagement.ErrDuplicateMember
		}
		return fmt.Errorf("failed to create member: %w", err)
	}
	return nil
}

func (r *PostgresGroupManagementRepository) GetMemberByID(ctx context.Context, userID, memberID string) (*groupmanagement.Member, error) {
	query := `
		SELECT id, user_id, group_id, name, wallet_address, tags, status, created_at
		FROM group_members
		WHERE id = $1 AND user_id = $2
	`
	member := &groupmanagement.Member{}
	var tags []string
	err := r.db.QueryRowContext(ctx, query, memberID, userID).Scan(
		&member.ID,
		&member.UserID,
		&member.GroupID,
		&member.Name,
		&member.WalletAddress,
		pq.Array(&tags),
		&member.Status,
		&member.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, groupmanagement.ErrMemberNotFound
		}
		return nil, fmt.Errorf("failed to get member: %w", err)
	}
	member.Tags = tags
	member.Status = groupmanagement.NormalizeMemberStatus(member.Status)
	return member, nil
}

func (r *PostgresGroupManagementRepository) ListVisibleMembers(ctx context.Context, userID, walletAddress string) ([]*groupmanagement.Member, error) {
	query := `
		SELECT DISTINCT m.id, m.user_id, m.group_id, m.name, m.wallet_address, m.tags, m.status, m.created_at
		FROM group_members m
		JOIN address_groups g ON g.id = m.group_id
		LEFT JOIN group_members current_member
			ON current_member.group_id = g.id
			AND current_member.status = $3
			AND LOWER(current_member.wallet_address) = LOWER($2)
		WHERE g.user_id = $1
			OR ($2 <> '' AND current_member.id IS NOT NULL AND m.status = $3)
		ORDER BY m.created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID, strings.TrimSpace(walletAddress), groupmanagement.MemberStatusActive)
	if err != nil {
		return nil, fmt.Errorf("failed to query members: %w", err)
	}
	defer rows.Close()

	var members []*groupmanagement.Member
	for rows.Next() {
		member := &groupmanagement.Member{}
		var tags []string
		if err := rows.Scan(
			&member.ID,
			&member.UserID,
			&member.GroupID,
			&member.Name,
			&member.WalletAddress,
			pq.Array(&tags),
			&member.Status,
			&member.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan member: %w", err)
		}
		member.Tags = tags
		member.Status = groupmanagement.NormalizeMemberStatus(member.Status)
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate members: %w", err)
	}
	return members, nil
}

func (r *PostgresGroupManagementRepository) UpdateMember(ctx context.Context, member *groupmanagement.Member) error {
	query := `
		UPDATE group_members
		SET group_id = $1, name = $2, wallet_address = $3, tags = $4, status = $5
		WHERE id = $6 AND user_id = $7
	`
	result, err := r.db.ExecContext(ctx, query, member.GroupID, member.Name, member.WalletAddress, pq.Array(member.Tags), groupmanagement.NormalizeMemberStatus(member.Status), member.ID, member.UserID)
	if err != nil {
		if isDuplicateMemberError(err) {
			return groupmanagement.ErrDuplicateMember
		}
		return fmt.Errorf("failed to update member: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if rows == 0 {
		return groupmanagement.ErrMemberNotFound
	}
	return nil
}

func (r *PostgresGroupManagementRepository) UpdateMemberStatus(ctx context.Context, userID, memberID, status string) error {
	query := `UPDATE group_members SET status = $1 WHERE id = $2 AND user_id = $3`
	result, err := r.db.ExecContext(ctx, query, groupmanagement.NormalizeMemberStatus(status), memberID, userID)
	if err != nil {
		return fmt.Errorf("failed to update member status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if rows == 0 {
		return groupmanagement.ErrMemberNotFound
	}
	return nil
}

func (r *PostgresGroupManagementRepository) DeleteMember(ctx context.Context, userID, memberID string) error {
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
		return groupmanagement.ErrMemberNotFound
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
