package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/yeying-community/webdav/internal/infrastructure/config"
)

// PostgresDB PostgreSQL 数据库连接
type PostgresDB struct {
	DB *sql.DB
}

// NewPostgresDB 创建 PostgreSQL 数据库连接
func NewPostgresDB(cfg config.DatabaseConfig) (*PostgresDB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host,
		cfg.Port,
		cfg.Username,
		cfg.Password,
		cfg.Database,
		cfg.SSLMode,
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 设置连接池参数
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.MaxLifetime)

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &PostgresDB{DB: db}, nil
}

// Close 关闭数据库连接
func (p *PostgresDB) Close() error {
	return p.DB.Close()
}

// Migrate 执行数据库迁移
func (p *PostgresDB) Migrate(ctx context.Context) error {
	queries := []string{
		// 创建用户表
		`CREATE TABLE IF NOT EXISTS users (
			id VARCHAR(50) PRIMARY KEY,
			username VARCHAR(255) UNIQUE NOT NULL,
			password TEXT,
			wallet_address VARCHAR(42) UNIQUE,
			directory TEXT NOT NULL,
			permissions VARCHAR(10) NOT NULL DEFAULT 'R',
			quota BIGINT NOT NULL DEFAULT 1073741824,
			used_space BIGINT NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// 创建用户规则表
		`CREATE TABLE IF NOT EXISTS user_rules (
			id SERIAL PRIMARY KEY,
			user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			path TEXT NOT NULL,
			permissions VARCHAR(10) NOT NULL,
			regex BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// 创建回收站表
		`CREATE TABLE IF NOT EXISTS recycle_items (
			id VARCHAR(50) PRIMARY KEY,
			hash VARCHAR(50) UNIQUE NOT NULL,
			user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			username VARCHAR(255) NOT NULL,
			directory TEXT NOT NULL,
			name TEXT NOT NULL,
			path TEXT NOT NULL,
			size BIGINT NOT NULL DEFAULT 0,
			deleted_at TIMESTAMP NOT NULL DEFAULT NOW(),
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// 创建分享表
		`CREATE TABLE IF NOT EXISTS share_items (
			id VARCHAR(50) PRIMARY KEY,
			token VARCHAR(50) UNIQUE NOT NULL,
			user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			username VARCHAR(255) NOT NULL,
			name TEXT NOT NULL,
			path TEXT NOT NULL,
			expires_at TIMESTAMP NULL,
			view_count BIGINT NOT NULL DEFAULT 0,
			download_count BIGINT NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// 创建定向分享表（分享给指定用户）
		`CREATE TABLE IF NOT EXISTS share_user_items (
			id VARCHAR(50) PRIMARY KEY,
			owner_user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			owner_username VARCHAR(255) NOT NULL,
			target_user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			target_wallet_address VARCHAR(255) NOT NULL,
			name TEXT NOT NULL,
			path TEXT NOT NULL,
			is_dir BOOLEAN NOT NULL DEFAULT FALSE,
			permissions VARCHAR(10) NOT NULL,
			expires_at TIMESTAMP NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// 好友地址分组
		`CREATE TABLE IF NOT EXISTS address_groups (
			id VARCHAR(50) PRIMARY KEY,
			user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// 好友地址
		`CREATE TABLE IF NOT EXISTS address_contacts (
			id VARCHAR(50) PRIMARY KEY,
			user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			group_id VARCHAR(50) NULL REFERENCES address_groups(id) ON DELETE SET NULL,
			name VARCHAR(255) NOT NULL,
			wallet_address VARCHAR(255) NOT NULL,
			tags TEXT[] NOT NULL DEFAULT '{}',
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// 补充分享表字段（兼容已存在表）
		`ALTER TABLE share_items ADD COLUMN IF NOT EXISTS view_count BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE share_items ADD COLUMN IF NOT EXISTS download_count BIGINT NOT NULL DEFAULT 0`,

		// 补充定向分享表字段（兼容已存在表）
		`ALTER TABLE share_user_items ADD COLUMN IF NOT EXISTS is_dir BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE share_user_items ADD COLUMN IF NOT EXISTS permissions VARCHAR(10) NOT NULL DEFAULT 'R'`,
		`ALTER TABLE share_user_items ADD COLUMN IF NOT EXISTS expires_at TIMESTAMP NULL`,

		// 创建回收站的哈希索引
		`CREATE INDEX IF NOT EXISTS idx_recycle_items_hash ON recycle_items(hash)`,

		// 创建回收站的用户ID索引
		`CREATE INDEX IF NOT EXISTS idx_recycle_items_user_id ON recycle_items(user_id)`,

		// 创建分享的 token 索引
		`CREATE INDEX IF NOT EXISTS idx_share_items_token ON share_items(token)`,

		// 创建分享的用户ID索引
		`CREATE INDEX IF NOT EXISTS idx_share_items_user_id ON share_items(user_id)`,

		// 创建定向分享的用户索引
		`CREATE INDEX IF NOT EXISTS idx_share_user_items_owner_id ON share_user_items(owner_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_share_user_items_target_id ON share_user_items(target_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_share_user_items_target_wallet ON share_user_items(target_wallet_address)`,

		// 好友地址分组索引
		`CREATE INDEX IF NOT EXISTS idx_address_groups_user_id ON address_groups(user_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_address_groups_user_name ON address_groups(user_id, name)`,

		// 好友地址索引
		`CREATE INDEX IF NOT EXISTS idx_address_contacts_user_id ON address_contacts(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_address_contacts_group_id ON address_contacts(group_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_address_contacts_user_wallet ON address_contacts(user_id, wallet_address)`,

		// 兼容已有地址簿表
		`ALTER TABLE address_contacts ADD COLUMN IF NOT EXISTS tags TEXT[] NOT NULL DEFAULT '{}'`,

		// 创建钱包地址索引
		`CREATE INDEX IF NOT EXISTS idx_users_wallet_address ON users(wallet_address) WHERE wallet_address IS NOT NULL`,

		// 创建用户名索引
		`CREATE INDEX IF NOT EXISTS idx_users_username ON users(username)`,

		// 创建用户规则的用户ID索引
		`CREATE INDEX IF NOT EXISTS idx_user_rules_user_id ON user_rules(user_id)`,

		// 创建更新时间触发器函数
		`CREATE OR REPLACE FUNCTION update_updated_at_column()
		RETURNS TRIGGER AS $$
		BEGIN
			NEW.updated_at = NOW();
			RETURN NEW;
		END;
		$$ language 'plpgsql'`,

		// 创建用户表的更新时间触发器
		`DROP TRIGGER IF EXISTS update_users_updated_at ON users`,
		`CREATE TRIGGER update_users_updated_at
		BEFORE UPDATE ON users
		FOR EACH ROW
		EXECUTE FUNCTION update_updated_at_column()`,
	}

	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, query := range queries {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to execute migration query: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
