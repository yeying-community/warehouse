package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"
	"github.com/yeying-community/warehouse/internal/domain/group"
)

type GroupRepository interface {
	CreateGroup(ctx context.Context, grp *group.Group, ownerMember *group.Member) error
	GetGroupByID(ctx context.Context, userID, groupID string) (*group.Group, error)
	GetVisibleGroupByID(ctx context.Context, userID, walletAddress, groupID string) (*group.Group, error)
	ListVisibleGroups(ctx context.Context, userID, walletAddress string) ([]*group.Group, error)
	UpdateGroupName(ctx context.Context, userID, groupID, name string) error
	DeleteGroup(ctx context.Context, userID, groupID string) error

	CreateMember(ctx context.Context, member *group.Member) error
	GetMemberByID(ctx context.Context, userID, memberID string) (*group.Member, error)
	ListVisibleMembers(ctx context.Context, userID, walletAddress string) ([]*group.Member, error)
	UpdateMember(ctx context.Context, member *group.Member) error
	UpdateMemberStatusByWallet(ctx context.Context, walletAddress, memberID, status string) error
	DeleteMember(ctx context.Context, userID, memberID string) error
	DeleteMemberByWallet(ctx context.Context, walletAddress, memberID string) error
}

type PostgresGroupRepository struct {
	db *sql.DB
}

func NewPostgresGroupRepository(db *sql.DB) *PostgresGroupRepository {
	return &PostgresGroupRepository{db: db}
}

func (r *PostgresGroupRepository) CreateGroup(ctx context.Context, grp *group.Group, ownerMember *group.Member) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin group transaction: %w", err)
	}
	defer tx.Rollback()

	groupQuery := `
		INSERT INTO address_groups (id, user_id, name, created_at)
		VALUES ($1, $2, $3, $4)
	`
	if _, err := tx.ExecContext(ctx, groupQuery, grp.ID, grp.UserID, grp.Name, grp.CreatedAt); err != nil {
		if strings.Contains(err.Error(), "idx_address_groups_user_name") {
			return group.ErrDuplicateGroupName
		}
		return fmt.Errorf("failed to create group: %w", err)
	}
	if ownerMember != nil {
		memberQuery := `
			INSERT INTO group_members (id, user_id, group_id, name, wallet_address, tags, status, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`
		if _, err := tx.ExecContext(ctx, memberQuery,
			ownerMember.ID,
			ownerMember.UserID,
			ownerMember.GroupID,
			ownerMember.Name,
			ownerMember.WalletAddress,
			pq.Array(ownerMember.Tags),
			group.NormalizeMemberStatus(ownerMember.Status),
			ownerMember.CreatedAt,
		); err != nil {
			if isDuplicateMemberError(err) {
				return group.ErrDuplicateMember
			}
			return fmt.Errorf("failed to create owner group member: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit group transaction: %w", err)
	}
	return nil
}

func (r *PostgresGroupRepository) GetGroupByID(ctx context.Context, userID, groupID string) (*group.Group, error) {
	query := `
		SELECT id, user_id, name, created_at
		FROM address_groups
		WHERE id = $1 AND user_id = $2
	`
	grp := &group.Group{}
	err := r.db.QueryRowContext(ctx, query, groupID, userID).Scan(
		&grp.ID,
		&grp.UserID,
		&grp.Name,
		&grp.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, group.ErrGroupNotFound
		}
		return nil, fmt.Errorf("failed to get group: %w", err)
	}
	grp.CanInvite = true
	return grp, nil
}

func (r *PostgresGroupRepository) GetVisibleGroupByID(ctx context.Context, userID, walletAddress, groupID string) (*group.Group, error) {
	query := `
		SELECT DISTINCT
			g.id,
			g.user_id,
			g.name,
			(g.user_id = $2 OR active_member.id IS NOT NULL),
			g.created_at
		FROM address_groups g
		LEFT JOIN group_members member
			ON member.group_id = g.id
			AND LOWER(member.wallet_address) = LOWER($3)
		LEFT JOIN group_members active_member
			ON active_member.group_id = g.id
			AND LOWER(active_member.wallet_address) = LOWER($3)
			AND active_member.status = $4
		WHERE g.id = $1
			AND (
				g.user_id = $2
				OR ($3 <> '' AND member.id IS NOT NULL)
			)
	`
	grp := &group.Group{}
	err := r.db.QueryRowContext(
		ctx,
		query,
		groupID,
		userID,
		strings.TrimSpace(walletAddress),
		group.MemberStatusActive,
	).Scan(
		&grp.ID,
		&grp.UserID,
		&grp.Name,
		&grp.CanInvite,
		&grp.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, group.ErrGroupNotFound
		}
		return nil, fmt.Errorf("failed to get visible group: %w", err)
	}
	return grp, nil
}

func (r *PostgresGroupRepository) ListVisibleGroups(ctx context.Context, userID, walletAddress string) ([]*group.Group, error) {
	query := `
		SELECT DISTINCT
			g.id,
			g.user_id,
			g.name,
			(g.user_id = $1 OR active_member.id IS NOT NULL),
			g.created_at
		FROM address_groups g
		LEFT JOIN group_members member
			ON member.group_id = g.id
			AND LOWER(member.wallet_address) = LOWER($2)
		LEFT JOIN group_members active_member
			ON active_member.group_id = g.id
			AND LOWER(active_member.wallet_address) = LOWER($2)
			AND active_member.status = $3
		WHERE g.user_id = $1
			OR ($2 <> '' AND member.id IS NOT NULL)
		ORDER BY g.created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID, strings.TrimSpace(walletAddress), group.MemberStatusActive)
	if err != nil {
		return nil, fmt.Errorf("failed to query groups: %w", err)
	}
	defer rows.Close()

	var groups []*group.Group
	for rows.Next() {
		grp := &group.Group{}
		if err := rows.Scan(&grp.ID, &grp.UserID, &grp.Name, &grp.CanInvite, &grp.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan group: %w", err)
		}
		groups = append(groups, grp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate groups: %w", err)
	}
	return groups, nil
}

func (r *PostgresGroupRepository) UpdateGroupName(ctx context.Context, userID, groupID, name string) error {
	query := `UPDATE address_groups SET name = $1 WHERE id = $2 AND user_id = $3`
	result, err := r.db.ExecContext(ctx, query, name, groupID, userID)
	if err != nil {
		if strings.Contains(err.Error(), "idx_address_groups_user_name") {
			return group.ErrDuplicateGroupName
		}
		return fmt.Errorf("failed to update group: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if rows == 0 {
		return group.ErrGroupNotFound
	}
	return nil
}

func (r *PostgresGroupRepository) DeleteGroup(ctx context.Context, userID, groupID string) error {
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
		return group.ErrGroupNotFound
	}
	return nil
}

func (r *PostgresGroupRepository) CreateMember(ctx context.Context, member *group.Member) error {
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
		group.NormalizeMemberStatus(member.Status),
		member.CreatedAt,
	)
	if err != nil {
		if isDuplicateMemberError(err) {
			return group.ErrDuplicateMember
		}
		return fmt.Errorf("failed to create member: %w", err)
	}
	return nil
}

func (r *PostgresGroupRepository) GetMemberByID(ctx context.Context, userID, memberID string) (*group.Member, error) {
	query := `
		SELECT
			m.id,
			m.user_id,
			m.group_id,
			m.name,
			COALESCE(invited.username, ''),
			m.wallet_address,
			m.tags,
			m.status,
			LOWER(m.wallet_address) = LOWER(COALESCE(owner.wallet_address, '')),
			m.created_at
		FROM group_members m
		JOIN address_groups g ON g.id = m.group_id
		LEFT JOIN users owner ON owner.id = g.user_id
		LEFT JOIN users invited ON LOWER(invited.wallet_address) = LOWER(m.wallet_address)
		WHERE m.id = $1 AND m.user_id = $2
	`
	member := &group.Member{}
	var tags []string
	err := r.db.QueryRowContext(ctx, query, memberID, userID).Scan(
		&member.ID,
		&member.UserID,
		&member.GroupID,
		&member.Name,
		&member.Username,
		&member.WalletAddress,
		pq.Array(&tags),
		&member.Status,
		&member.IsOwner,
		&member.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, group.ErrMemberNotFound
		}
		return nil, fmt.Errorf("failed to get member: %w", err)
	}
	member.Tags = tags
	member.Status = group.NormalizeMemberStatus(member.Status)
	return member, nil
}

func (r *PostgresGroupRepository) ListVisibleMembers(ctx context.Context, userID, walletAddress string) ([]*group.Member, error) {
	query := `
		SELECT DISTINCT
			m.id,
			m.user_id,
			m.group_id,
			m.name,
			COALESCE(invited.username, ''),
			m.wallet_address,
			m.tags,
			m.status,
			LOWER(m.wallet_address) = LOWER(COALESCE(owner.wallet_address, '')),
			m.created_at
		FROM group_members m
		JOIN address_groups g ON g.id = m.group_id
		LEFT JOIN users owner ON owner.id = g.user_id
		LEFT JOIN users invited ON LOWER(invited.wallet_address) = LOWER(m.wallet_address)
		LEFT JOIN group_members current_member
			ON current_member.group_id = g.id
			AND current_member.status = $3
			AND LOWER(current_member.wallet_address) = LOWER($2)
		WHERE g.user_id = $1
			OR ($2 <> '' AND LOWER(m.wallet_address) = LOWER($2))
			OR ($2 <> '' AND current_member.id IS NOT NULL AND m.status = $3)
		ORDER BY m.created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID, strings.TrimSpace(walletAddress), group.MemberStatusActive)
	if err != nil {
		return nil, fmt.Errorf("failed to query members: %w", err)
	}
	defer rows.Close()

	var members []*group.Member
	for rows.Next() {
		member := &group.Member{}
		var tags []string
		if err := rows.Scan(
			&member.ID,
			&member.UserID,
			&member.GroupID,
			&member.Name,
			&member.Username,
			&member.WalletAddress,
			pq.Array(&tags),
			&member.Status,
			&member.IsOwner,
			&member.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan member: %w", err)
		}
		member.Tags = tags
		member.Status = group.NormalizeMemberStatus(member.Status)
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate members: %w", err)
	}
	return members, nil
}

func (r *PostgresGroupRepository) UpdateMember(ctx context.Context, member *group.Member) error {
	query := `
		UPDATE group_members
		SET group_id = $1, name = $2, wallet_address = $3, tags = $4, status = $5
		WHERE id = $6 AND user_id = $7
	`
	result, err := r.db.ExecContext(ctx, query, member.GroupID, member.Name, member.WalletAddress, pq.Array(member.Tags), group.NormalizeMemberStatus(member.Status), member.ID, member.UserID)
	if err != nil {
		if isDuplicateMemberError(err) {
			return group.ErrDuplicateMember
		}
		return fmt.Errorf("failed to update member: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if rows == 0 {
		return group.ErrMemberNotFound
	}
	return nil
}

func (r *PostgresGroupRepository) UpdateMemberStatusByWallet(ctx context.Context, walletAddress, memberID, status string) error {
	query := `UPDATE group_members SET status = $1 WHERE id = $2 AND LOWER(wallet_address) = LOWER($3) AND status = $4`
	result, err := r.db.ExecContext(ctx, query, group.NormalizeMemberStatus(status), memberID, strings.TrimSpace(walletAddress), group.MemberStatusPending)
	if err != nil {
		return fmt.Errorf("failed to update member status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if rows == 0 {
		return group.ErrMemberNotFound
	}
	return nil
}

func (r *PostgresGroupRepository) DeleteMember(ctx context.Context, userID, memberID string) error {
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
		return group.ErrMemberNotFound
	}
	return nil
}

func (r *PostgresGroupRepository) DeleteMemberByWallet(ctx context.Context, walletAddress, memberID string) error {
	query := `DELETE FROM group_members WHERE id = $1 AND LOWER(wallet_address) = LOWER($2) AND status = $3`
	result, err := r.db.ExecContext(ctx, query, memberID, strings.TrimSpace(walletAddress), group.MemberStatusPending)
	if err != nil {
		return fmt.Errorf("failed to delete member by wallet: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if rows == 0 {
		return group.ErrMemberNotFound
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
