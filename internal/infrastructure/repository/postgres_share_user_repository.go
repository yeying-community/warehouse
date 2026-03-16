package repository

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/yeying-community/warehouse/internal/domain/shareuser"
)

type UserShareAudience struct {
	AudienceType  string
	TargetUserID  string
	TargetWallet  string
	SourceGroupID string
}

// UserShareRepository 定向分享仓储接口
type UserShareRepository interface {
	Create(ctx context.Context, item *shareuser.ShareUserItem) error
	CreateWithAudiences(ctx context.Context, item *shareuser.ShareUserItem, audiences []UserShareAudience) error
	GetByID(ctx context.Context, id string) (*shareuser.ShareUserItem, error)
	GetByOwnerID(ctx context.Context, ownerID string) ([]*shareuser.ShareUserItem, error)
	GetByTargetID(ctx context.Context, targetID string) ([]*shareuser.ShareUserItem, error)
	DeleteByID(ctx context.Context, id string) error
	ListAudiencesByShareID(ctx context.Context, shareID string) ([]UserShareAudience, error)
}

// PostgresUserShareRepository PostgreSQL 实现
type PostgresUserShareRepository struct {
	db *sql.DB
}

// NewPostgresUserShareRepository 创建 PostgreSQL 定向分享仓储
func NewPostgresUserShareRepository(db *sql.DB) *PostgresUserShareRepository {
	return &PostgresUserShareRepository{db: db}
}

func (r *PostgresUserShareRepository) Create(ctx context.Context, item *shareuser.ShareUserItem) error {
	if item == nil {
		return fmt.Errorf("share item is required")
	}
	if strings.TrimSpace(item.TargetUserID) == "" {
		return fmt.Errorf("target user id is required")
	}
	aud := UserShareAudience{
		AudienceType: shareuser.AudienceTypeUser,
		TargetUserID: item.TargetUserID,
		TargetWallet: item.TargetWalletAddress,
	}
	return r.CreateWithAudiences(ctx, item, []UserShareAudience{aud})
}

func (r *PostgresUserShareRepository) CreateWithAudiences(ctx context.Context, item *shareuser.ShareUserItem, audiences []UserShareAudience) error {
	if item == nil {
		return fmt.Errorf("share item is required")
	}
	normalizedAudiences := normalizeAudiences(audiences)
	if len(normalizedAudiences) == 0 {
		return fmt.Errorf("at least one audience is required")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	insertItemQuery := `
		INSERT INTO internal_share_items (
			id, owner_user_id, owner_username, name, path, is_dir, permissions, expires_at, status, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'active',$9,$9)
	`
	if _, err := tx.ExecContext(ctx, insertItemQuery,
		item.ID,
		item.OwnerUserID,
		item.OwnerUsername,
		item.Name,
		item.Path,
		item.IsDir,
		item.Permissions,
		item.ExpiresAt,
		item.CreatedAt,
	); err != nil {
		return fmt.Errorf("failed to create internal share item: %w", err)
	}

	insertAudienceQuery := `
		INSERT INTO internal_share_audiences (
			id, share_id, audience_type, target_user_id, target_wallet_address, source_group_id, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (id) DO NOTHING
	`
	for _, aud := range normalizedAudiences {
		audienceID := makeAudienceID(item.ID, aud)
		var targetUserID any
		var targetWallet any
		var sourceGroupID any
		if strings.TrimSpace(aud.TargetUserID) != "" {
			targetUserID = aud.TargetUserID
		}
		if strings.TrimSpace(aud.TargetWallet) != "" {
			targetWallet = aud.TargetWallet
		}
		if strings.TrimSpace(aud.SourceGroupID) != "" {
			sourceGroupID = aud.SourceGroupID
		}
		if _, err := tx.ExecContext(ctx, insertAudienceQuery,
			audienceID,
			item.ID,
			aud.AudienceType,
			targetUserID,
			targetWallet,
			sourceGroupID,
			item.CreatedAt,
		); err != nil {
			return fmt.Errorf("failed to create share audience: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

func (r *PostgresUserShareRepository) GetByID(ctx context.Context, id string) (*shareuser.ShareUserItem, error) {
	query := `
		WITH audience_stats AS (
			SELECT
				share_id,
				COUNT(*)::INT AS audience_count,
				COUNT(*) FILTER (WHERE audience_type = 'user')::INT AS target_count,
				BOOL_OR(audience_type = 'all_users') AS all_users,
				BOOL_OR(source_group_id IS NOT NULL) AS has_group_source,
				MIN(target_user_id) FILTER (WHERE audience_type = 'user') AS sample_target_user_id,
				MIN(target_wallet_address) FILTER (WHERE audience_type = 'user') AS sample_target_wallet
			FROM internal_share_audiences
			WHERE share_id = $1
			GROUP BY share_id
		)
		SELECT
			i.id, i.owner_user_id, i.owner_username, i.name, i.path, i.is_dir, i.permissions, i.expires_at, i.created_at,
			COALESCE(s.audience_count, 0),
			COALESCE(s.target_count, 0),
			COALESCE(s.all_users, FALSE),
			COALESCE(s.has_group_source, FALSE),
			COALESCE(s.sample_target_user_id, ''),
			COALESCE(s.sample_target_wallet, '')
		FROM internal_share_items i
		LEFT JOIN audience_stats s ON s.share_id = i.id
		WHERE i.id = $1 AND i.status = 'active'
	`

	item := &shareuser.ShareUserItem{}
	var expiresAt sql.NullTime
	var audienceCount int
	var targetCount int
	var allUsers bool
	var hasGroupSource bool
	var sampleTargetUserID string
	var sampleTargetWallet string
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&item.ID,
		&item.OwnerUserID,
		&item.OwnerUsername,
		&item.Name,
		&item.Path,
		&item.IsDir,
		&item.Permissions,
		&expiresAt,
		&item.CreatedAt,
		&audienceCount,
		&targetCount,
		&allUsers,
		&hasGroupSource,
		&sampleTargetUserID,
		&sampleTargetWallet,
	)
	if err == sql.ErrNoRows {
		return nil, shareuser.ErrShareNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get share item: %w", err)
	}
	if expiresAt.Valid {
		item.ExpiresAt = &expiresAt.Time
	}
	item.AudienceCount = audienceCount
	item.TargetCount = targetCount
	item.AllUsers = allUsers
	item.TargetUserID = sampleTargetUserID
	item.TargetWalletAddress = sampleTargetWallet
	item.AudienceType = resolveTargetType(allUsers, targetCount, hasGroupSource)
	return item, nil
}

func (r *PostgresUserShareRepository) GetByOwnerID(ctx context.Context, ownerID string) ([]*shareuser.ShareUserItem, error) {
	query := `
		WITH audience_stats AS (
			SELECT
				share_id,
				COUNT(*)::INT AS audience_count,
				COUNT(*) FILTER (WHERE audience_type = 'user')::INT AS target_count,
				BOOL_OR(audience_type = 'all_users') AS all_users,
				BOOL_OR(source_group_id IS NOT NULL) AS has_group_source,
				MIN(target_user_id) FILTER (WHERE audience_type = 'user') AS sample_target_user_id,
				MIN(target_wallet_address) FILTER (WHERE audience_type = 'user') AS sample_target_wallet
			FROM internal_share_audiences
			GROUP BY share_id
		)
		SELECT
			i.id, i.owner_user_id, i.owner_username, i.name, i.path, i.is_dir, i.permissions, i.expires_at, i.created_at,
			COALESCE(s.audience_count, 0),
			COALESCE(s.target_count, 0),
			COALESCE(s.all_users, FALSE),
			COALESCE(s.has_group_source, FALSE),
			COALESCE(s.sample_target_user_id, ''),
			COALESCE(s.sample_target_wallet, '')
		FROM internal_share_items i
		LEFT JOIN audience_stats s ON s.share_id = i.id
		WHERE i.owner_user_id = $1 AND i.status = 'active'
		ORDER BY i.created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, fmt.Errorf("failed to query share items: %w", err)
	}
	defer rows.Close()

	return scanShareItems(rows)
}

func (r *PostgresUserShareRepository) GetByTargetID(ctx context.Context, targetID string) ([]*shareuser.ShareUserItem, error) {
	query := `
		WITH accessible_shares AS (
			SELECT DISTINCT share_id
			FROM internal_share_audiences
			WHERE (audience_type = 'user' AND target_user_id = $1)
			   OR audience_type = 'all_users'
		),
		audience_stats AS (
			SELECT
				share_id,
				COUNT(*)::INT AS audience_count,
				COUNT(*) FILTER (WHERE audience_type = 'user')::INT AS target_count,
				BOOL_OR(audience_type = 'all_users') AS all_users,
				BOOL_OR(source_group_id IS NOT NULL) AS has_group_source,
				MIN(target_user_id) FILTER (WHERE audience_type = 'user') AS sample_target_user_id,
				MIN(target_wallet_address) FILTER (WHERE audience_type = 'user') AS sample_target_wallet
			FROM internal_share_audiences
			GROUP BY share_id
		)
		SELECT
			i.id, i.owner_user_id, i.owner_username, i.name, i.path, i.is_dir, i.permissions, i.expires_at, i.created_at,
			COALESCE(s.audience_count, 0),
			COALESCE(s.target_count, 0),
			COALESCE(s.all_users, FALSE),
			COALESCE(s.has_group_source, FALSE),
			COALESCE(s.sample_target_user_id, ''),
			COALESCE(s.sample_target_wallet, '')
		FROM internal_share_items i
		INNER JOIN accessible_shares ac ON ac.share_id = i.id
		LEFT JOIN audience_stats s ON s.share_id = i.id
		WHERE i.status = 'active'
		  AND i.owner_user_id <> $1
		ORDER BY i.created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, targetID)
	if err != nil {
		return nil, fmt.Errorf("failed to query received share items: %w", err)
	}
	defer rows.Close()

	return scanShareItems(rows)
}

func (r *PostgresUserShareRepository) DeleteByID(ctx context.Context, id string) error {
	query := `DELETE FROM internal_share_items WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete share item: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if rows == 0 {
		return shareuser.ErrShareNotFound
	}
	return nil
}

func (r *PostgresUserShareRepository) ListAudiencesByShareID(ctx context.Context, shareID string) ([]UserShareAudience, error) {
	query := `
		SELECT audience_type, target_user_id, target_wallet_address, source_group_id
		FROM internal_share_audiences
		WHERE share_id = $1
		ORDER BY created_at ASC, id ASC
	`

	rows, err := r.db.QueryContext(ctx, query, shareID)
	if err != nil {
		return nil, fmt.Errorf("failed to query share audiences: %w", err)
	}
	defer rows.Close()

	items := make([]UserShareAudience, 0)
	for rows.Next() {
		var audienceType string
		var targetUserID sql.NullString
		var targetWallet sql.NullString
		var sourceGroupID sql.NullString
		if err := rows.Scan(&audienceType, &targetUserID, &targetWallet, &sourceGroupID); err != nil {
			return nil, fmt.Errorf("failed to scan share audience: %w", err)
		}
		aud := UserShareAudience{
			AudienceType: strings.TrimSpace(audienceType),
		}
		if targetUserID.Valid {
			aud.TargetUserID = targetUserID.String
		}
		if targetWallet.Valid {
			aud.TargetWallet = targetWallet.String
		}
		if sourceGroupID.Valid {
			aud.SourceGroupID = sourceGroupID.String
		}
		items = append(items, aud)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate share audiences: %w", err)
	}
	return items, nil
}

func scanShareItems(rows *sql.Rows) ([]*shareuser.ShareUserItem, error) {
	items := make([]*shareuser.ShareUserItem, 0)
	for rows.Next() {
		item := &shareuser.ShareUserItem{}
		var expiresAt sql.NullTime
		var audienceCount int
		var targetCount int
		var allUsers bool
		var hasGroupSource bool
		var sampleTargetUserID string
		var sampleTargetWallet string
		if err := rows.Scan(
			&item.ID,
			&item.OwnerUserID,
			&item.OwnerUsername,
			&item.Name,
			&item.Path,
			&item.IsDir,
			&item.Permissions,
			&expiresAt,
			&item.CreatedAt,
			&audienceCount,
			&targetCount,
			&allUsers,
			&hasGroupSource,
			&sampleTargetUserID,
			&sampleTargetWallet,
		); err != nil {
			return nil, fmt.Errorf("failed to scan share item: %w", err)
		}
		if expiresAt.Valid {
			item.ExpiresAt = &expiresAt.Time
		}
		item.AudienceCount = audienceCount
		item.TargetCount = targetCount
		item.AllUsers = allUsers
		item.TargetUserID = sampleTargetUserID
		item.TargetWalletAddress = sampleTargetWallet
		item.AudienceType = resolveTargetType(allUsers, targetCount, hasGroupSource)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate share items: %w", err)
	}
	return items, nil
}

func normalizeAudiences(input []UserShareAudience) []UserShareAudience {
	if len(input) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	items := make([]UserShareAudience, 0, len(input))
	for _, raw := range input {
		aud := UserShareAudience{
			AudienceType:  strings.TrimSpace(strings.ToLower(raw.AudienceType)),
			TargetUserID:  strings.TrimSpace(raw.TargetUserID),
			TargetWallet:  strings.TrimSpace(strings.ToLower(raw.TargetWallet)),
			SourceGroupID: strings.TrimSpace(raw.SourceGroupID),
		}
		if aud.AudienceType == "" {
			aud.AudienceType = shareuser.AudienceTypeUser
		}
		switch aud.AudienceType {
		case shareuser.AudienceTypeAllUsers:
			aud.TargetUserID = ""
			aud.TargetWallet = ""
			aud.SourceGroupID = ""
		case shareuser.AudienceTypeUser:
			if aud.TargetUserID == "" {
				continue
			}
		default:
			continue
		}
		key := aud.AudienceType + "|" + aud.TargetUserID + "|" + aud.TargetWallet + "|" + aud.SourceGroupID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, aud)
	}
	return items
}

func makeAudienceID(shareID string, aud UserShareAudience) string {
	h := sha1.New()
	_, _ = h.Write([]byte(shareID))
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(aud.AudienceType))
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(aud.TargetUserID))
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(aud.TargetWallet))
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(aud.SourceGroupID))
	sum := hex.EncodeToString(h.Sum(nil))
	return "aud_" + sum
}

func resolveTargetType(allUsers bool, targetCount int, hasGroupSource bool) string {
	if allUsers {
		return shareuser.AudienceTypeAllUsers
	}
	if hasGroupSource {
		return "groups"
	}
	if targetCount >= 1 {
		return "addresses"
	}
	return "addresses"
}
