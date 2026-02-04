package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/yeying-community/webdav/internal/domain/user"
	"github.com/yeying-community/webdav/internal/infrastructure/database"
)

// PostgresUserRepository PostgreSQL 用户仓储
type PostgresUserRepository struct {
	db *database.PostgresDB
}

// NewPostgresUserRepository 创建 PostgreSQL 用户仓储
func NewPostgresUserRepository(db *database.PostgresDB) (*PostgresUserRepository, error) {
	return &PostgresUserRepository{db: db}, nil
}

// FindByUsername 根据用户名查找用户
func (r *PostgresUserRepository) FindByUsername(ctx context.Context, username string) (*user.User, error) {
	query := `
		SELECT id, username, password, wallet_address, directory, permissions, 
		       quota, used_space, created_at, updated_at
		FROM users
		WHERE username = $1
	`

	u := &user.User{}
	var walletAddress sql.NullString
	var password sql.NullString
	var permissionsStr string

	err := r.db.DB.QueryRowContext(ctx, query, username).Scan(
		&u.ID,
		&u.Username,
		&password,
		&walletAddress,
		&u.Directory,
		&permissionsStr,
		&u.Quota,
		&u.UsedSpace,
		&u.CreatedAt,
		&u.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, user.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query user: %w", err)
	}

	u.Password = password.String
	u.WalletAddress = walletAddress.String
	u.Permissions = user.ParsePermissions(permissionsStr)

	// 加载用户规则
	rules, err := r.loadUserRules(ctx, u.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load user rules: %w", err)
	}
	u.Rules = rules

	return u, nil
}

// FindByWalletAddress 根据钱包地址查找用户
func (r *PostgresUserRepository) FindByWalletAddress(ctx context.Context, address string) (*user.User, error) {
	query := `
		SELECT id, username, password, wallet_address, directory, permissions,
		       quota, used_space, created_at, updated_at
		FROM users
		WHERE LOWER(wallet_address) = LOWER($1)
	`

	u := &user.User{}
	var walletAddress sql.NullString
	var password sql.NullString
	var permissionsStr string

	err := r.db.DB.QueryRowContext(ctx, query, address).Scan(
		&u.ID,
		&u.Username,
		&password,
		&walletAddress,
		&u.Directory,
		&permissionsStr,
		&u.Quota,
		&u.UsedSpace,
		&u.CreatedAt,
		&u.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, user.ErrUserNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("failed to query user: %w", err)
	}

	u.Password = password.String
	u.WalletAddress = walletAddress.String
	u.Permissions = user.ParsePermissions(permissionsStr)

	// 加载用户规则
	rules, err := r.loadUserRules(ctx, u.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load user rules: %w", err)
	}
	u.Rules = rules

	return u, nil
}

// FindByID 根据ID查找用户
func (r *PostgresUserRepository) FindByID(ctx context.Context, id string) (*user.User, error) {
	query := `
		SELECT id, username, password, wallet_address, directory, permissions,
		       quota, used_space, created_at, updated_at
		FROM users
		WHERE id = $1
	`

	u := &user.User{}
	var walletAddress sql.NullString
	var password sql.NullString
	var permissionsStr string

	err := r.db.DB.QueryRowContext(ctx, query, id).Scan(
		&u.ID,
		&u.Username,
		&password,
		&walletAddress,
		&u.Directory,
		&permissionsStr,
		&u.Quota,
		&u.UsedSpace,
		&u.CreatedAt,
		&u.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, user.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query user: %w", err)
	}

	u.Password = password.String
	u.WalletAddress = walletAddress.String
	u.Permissions = user.ParsePermissions(permissionsStr)

	// 加载用户规则
	rules, err := r.loadUserRules(ctx, u.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load user rules: %w", err)
	}
	u.Rules = rules

	return u, nil
}

// Save 保存用户
func (r *PostgresUserRepository) Save(ctx context.Context, u *user.User) error {
	tx, err := r.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 检查用户是否存在
	var exists bool
	err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)", u.ID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check user existence: %w", err)
	}

	var walletAddress *string
	if u.WalletAddress != "" {
		walletAddress = &u.WalletAddress
	}

	var password *string
	if u.Password != "" {
		password = &u.Password
	}

	if exists {
		// 更新用户
		query := `
			UPDATE users
			SET username = $1, password = $2, wallet_address = $3, directory = $4,
			    permissions = $5, quota = $6, used_space = $7
			WHERE id = $8
		`
		_, err = tx.ExecContext(ctx, query,
			u.Username,
			password,
			walletAddress,
			u.Directory,
			u.Permissions.String(),
			u.Quota,
			u.UsedSpace,
			u.ID,
		)
	} else {
		// 插入新用户
		query := `
			INSERT INTO users (id, username, password, wallet_address, directory, permissions, quota, used_space, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`
		_, err = tx.ExecContext(ctx, query,
			u.ID,
			u.Username,
			password,
			walletAddress,
			u.Directory,
			u.Permissions.String(),
			u.Quota,
			u.UsedSpace,
			u.CreatedAt,
			u.UpdatedAt,
		)
	}

	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			if strings.Contains(err.Error(), "username") {
				return user.ErrDuplicateUsername
			}
			if strings.Contains(err.Error(), "wallet_address") {
				return user.ErrDuplicateAddress
			}
		}
		return fmt.Errorf("failed to save user: %w", err)
	}

	// 删除旧规则
	_, err = tx.ExecContext(ctx, "DELETE FROM user_rules WHERE user_id = $1", u.ID)
	if err != nil {
		return fmt.Errorf("failed to delete old rules: %w", err)
	}

	// 插入新规则
	if len(u.Rules) > 0 {
		for _, rule := range u.Rules {
			query := `
				INSERT INTO user_rules (user_id, path, permissions, regex)
				VALUES ($1, $2, $3, $4)
			`
			_, err = tx.ExecContext(ctx, query, u.ID, rule.Path, rule.Permissions.String(), rule.Regex)
			if err != nil {
				return fmt.Errorf("failed to insert rule: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Delete 删除用户
func (r *PostgresUserRepository) Delete(ctx context.Context, username string) error {
	query := "DELETE FROM users WHERE username = $1"
	result, err := r.db.DB.ExecContext(ctx, query, username)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}

	if rows == 0 {
		return user.ErrUserNotFound
	}

	return nil
}

// List 列出所有用户
func (r *PostgresUserRepository) List(ctx context.Context) ([]*user.User, error) {
	query := `
		SELECT id, username, password, wallet_address, directory, permissions,
		       quota, used_space, created_at, updated_at
		FROM users
		ORDER BY created_at DESC
	`

	rows, err := r.db.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query users: %w", err)
	}
	defer rows.Close()

	var users []*user.User

	for rows.Next() {
		u := &user.User{}
		var walletAddress sql.NullString
		var password sql.NullString
		var permissionsStr string

		err := rows.Scan(
			&u.ID,
			&u.Username,
			&password,
			&walletAddress,
			&u.Directory,
			&permissionsStr,
			&u.Quota,
			&u.UsedSpace,
			&u.CreatedAt,
			&u.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}

		u.Password = password.String
		u.WalletAddress = walletAddress.String
		u.Permissions = user.ParsePermissions(permissionsStr)

		// 加载用户规则
		rules, err := r.loadUserRules(ctx, u.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to load user rules: %w", err)
		}
		u.Rules = rules

		users = append(users, u)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate rows: %w", err)
	}

	return users, nil
}

// UpdateUsedSpace 更新用户已使用空间
func (r *PostgresUserRepository) UpdateUsedSpace(ctx context.Context, username string, usedSpace int64) error {
	query := "UPDATE users SET used_space = $1 WHERE username = $2"
	result, err := r.db.DB.ExecContext(ctx, query, usedSpace, username)
	if err != nil {
		return fmt.Errorf("failed to update used space: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}

	if rows == 0 {
		return user.ErrUserNotFound
	}

	return nil
}

// UpdateQuota 更新用户配额
func (r *PostgresUserRepository) UpdateQuota(ctx context.Context, username string, quota int64) error {
	query := "UPDATE users SET quota = $1 WHERE username = $2"
	result, err := r.db.DB.ExecContext(ctx, query, quota, username)
	if err != nil {
		return fmt.Errorf("failed to update quota: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}

	if rows == 0 {
		return user.ErrUserNotFound
	}

	return nil
}

// loadUserRules 加载用户规则
func (r *PostgresUserRepository) loadUserRules(ctx context.Context, userID string) ([]*user.Rule, error) {
	query := "SELECT path, permissions, regex FROM user_rules WHERE user_id = $1 ORDER BY id"

	rows, err := r.db.DB.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query user rules: %w", err)
	}
	defer rows.Close()

	var rules []*user.Rule
	for rows.Next() {
		rule := &user.Rule{}
		var permissionsStr string

		err := rows.Scan(&rule.Path, &permissionsStr, &rule.Regex)
		if err != nil {
			return nil, fmt.Errorf("failed to scan rule: %w", err)
		}

		rule.Permissions = user.ParsePermissions(permissionsStr)
		rules = append(rules, rule)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate rules: %w", err)
	}

	return rules, nil
}
