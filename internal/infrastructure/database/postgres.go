package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
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
			email VARCHAR(255) UNIQUE,
			directory TEXT NOT NULL,
			permissions VARCHAR(10) NOT NULL DEFAULT 'R',
			quota BIGINT NOT NULL DEFAULT 1073741824,
			used_space BIGINT NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// 兼容旧表结构：新增 email 字段
		`ALTER TABLE IF EXISTS users
			ADD COLUMN IF NOT EXISTS email VARCHAR(255)`,

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

		// 内部共享主表：支持单用户/分组/全员共享的统一共享对象
		`CREATE TABLE IF NOT EXISTS internal_share_items (
			id VARCHAR(50) PRIMARY KEY,
			owner_user_id VARCHAR(50) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			owner_username VARCHAR(255) NOT NULL,
			name TEXT NOT NULL,
			path TEXT NOT NULL,
			is_dir BOOLEAN NOT NULL DEFAULT FALSE,
			permissions VARCHAR(10) NOT NULL,
			expires_at TIMESTAMP NULL,
			status VARCHAR(20) NOT NULL DEFAULT 'active',
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// 内部共享受众表：共享对象可以绑定多个受众（用户或全员）
		`CREATE TABLE IF NOT EXISTS internal_share_audiences (
			id VARCHAR(50) PRIMARY KEY,
			share_id VARCHAR(50) NOT NULL REFERENCES internal_share_items(id) ON DELETE CASCADE,
			audience_type VARCHAR(20) NOT NULL,
			target_user_id VARCHAR(50) NULL REFERENCES users(id) ON DELETE CASCADE,
			target_wallet_address VARCHAR(255) NULL,
			source_group_id VARCHAR(50) NULL REFERENCES address_groups(id) ON DELETE SET NULL,
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

		// 复制 outbox：active 记录文件变更，后台异步分发到 standby
		`CREATE TABLE IF NOT EXISTS replication_outbox (
			id BIGSERIAL PRIMARY KEY,
			source_node_id TEXT NOT NULL,
			target_node_id TEXT NOT NULL,
			op TEXT NOT NULL,
			path TEXT NULL,
			from_path TEXT NULL,
			to_path TEXT NULL,
			is_dir BOOLEAN NOT NULL DEFAULT FALSE,
			content_sha256 TEXT NULL,
			file_size BIGINT NULL,
			assignment_generation BIGINT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			attempt_count INT NOT NULL DEFAULT 0,
			next_retry_at TIMESTAMP NOT NULL DEFAULT NOW(),
			last_error TEXT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			dispatched_at TIMESTAMP NULL
		)`,

		// 复制位点：记录 standby 已应用到哪个 outbox 序号
		`CREATE TABLE IF NOT EXISTS replication_offsets (
			source_node_id TEXT NOT NULL,
			target_node_id TEXT NOT NULL,
			assignment_generation BIGINT NULL,
			last_applied_outbox_id BIGINT NOT NULL,
			last_applied_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			PRIMARY KEY (source_node_id, target_node_id)
		)`,

		// 历史补齐任务：记录每次 reconcile 运行情况
		`CREATE TABLE IF NOT EXISTS replication_reconcile_jobs (
			id BIGSERIAL PRIMARY KEY,
			source_node_id TEXT NOT NULL,
			target_node_id TEXT NOT NULL,
			assignment_generation BIGINT NULL,
			watermark_outbox_id BIGINT NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			scanned_items BIGINT NOT NULL DEFAULT 0,
			pending_items BIGINT NOT NULL DEFAULT 0,
			started_at TIMESTAMP NOT NULL DEFAULT NOW(),
			completed_at TIMESTAMP NULL,
			last_error TEXT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// 历史补齐条目：记录任务扫描出的待补齐路径
		`CREATE TABLE IF NOT EXISTS replication_reconcile_items (
			id BIGSERIAL PRIMARY KEY,
			job_id BIGINT NOT NULL REFERENCES replication_reconcile_jobs(id) ON DELETE CASCADE,
			path TEXT NOT NULL,
			is_dir BOOLEAN NOT NULL DEFAULT FALSE,
			file_size BIGINT NULL,
			modified_at TIMESTAMP NULL,
			state TEXT NOT NULL DEFAULT 'pending',
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			UNIQUE(job_id, path)
		)`,

		// 共享控制面节点注册表：standby/active 通过数据库心跳注册，便于 peer 自动发现
		`CREATE TABLE IF NOT EXISTS cluster_nodes (
			node_id TEXT PRIMARY KEY,
			role TEXT NOT NULL,
			advertise_url TEXT NOT NULL,
			last_heartbeat_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT TIMEZONE('UTC', NOW()),
			updated_at TIMESTAMP NOT NULL DEFAULT TIMEZONE('UTC', NOW())
		)`,

		// 共享控制面复制分配表：记录 active 与 standby 的正式 assignment。
		// 当前阶段仅用于 schema 准备与运维观察，还未接管复制流量。
		`CREATE TABLE IF NOT EXISTS cluster_replication_assignments (
			id BIGSERIAL PRIMARY KEY,
			active_node_id TEXT NOT NULL,
			standby_node_id TEXT NOT NULL,
			state TEXT NOT NULL,
			generation BIGINT NOT NULL DEFAULT 1,
			lease_expires_at TIMESTAMP NULL,
			last_reconcile_job_id BIGINT NULL,
			last_error TEXT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT TIMEZONE('UTC', NOW()),
			updated_at TIMESTAMP NOT NULL DEFAULT TIMEZONE('UTC', NOW()),
			UNIQUE(active_node_id, standby_node_id)
		)`,

		// 补充分享表字段（兼容已存在表）
		`ALTER TABLE replication_outbox ADD COLUMN IF NOT EXISTS assignment_generation BIGINT NULL`,
		`ALTER TABLE replication_offsets ADD COLUMN IF NOT EXISTS assignment_generation BIGINT NULL`,
		`ALTER TABLE replication_reconcile_jobs ADD COLUMN IF NOT EXISTS assignment_generation BIGINT NULL`,
		`ALTER TABLE cluster_nodes ALTER COLUMN created_at SET DEFAULT TIMEZONE('UTC', NOW())`,
		`ALTER TABLE cluster_nodes ALTER COLUMN updated_at SET DEFAULT TIMEZONE('UTC', NOW())`,
		`ALTER TABLE cluster_replication_assignments ALTER COLUMN created_at SET DEFAULT TIMEZONE('UTC', NOW())`,
		`ALTER TABLE cluster_replication_assignments ALTER COLUMN updated_at SET DEFAULT TIMEZONE('UTC', NOW())`,
		`ALTER TABLE share_items ADD COLUMN IF NOT EXISTS view_count BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE share_items ADD COLUMN IF NOT EXISTS download_count BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE internal_share_items ADD COLUMN IF NOT EXISTS status VARCHAR(20) NOT NULL DEFAULT 'active'`,

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
		`CREATE INDEX IF NOT EXISTS idx_internal_share_items_owner_created
			ON internal_share_items(owner_user_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_internal_share_items_path ON internal_share_items(path)`,
		`CREATE INDEX IF NOT EXISTS idx_internal_share_audiences_share ON internal_share_audiences(share_id)`,
		`CREATE INDEX IF NOT EXISTS idx_internal_share_audiences_target_user
			ON internal_share_audiences(target_user_id)
			WHERE target_user_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_internal_share_audiences_all_users
			ON internal_share_audiences(audience_type)
			WHERE audience_type = 'all_users'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_internal_share_audiences_share_user
			ON internal_share_audiences(share_id, audience_type, target_user_id)
			WHERE audience_type = 'user' AND target_user_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_internal_share_audiences_share_all_users
			ON internal_share_audiences(share_id, audience_type)
			WHERE audience_type = 'all_users'`,

		// 好友地址分组索引
		`CREATE INDEX IF NOT EXISTS idx_address_groups_user_id ON address_groups(user_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_address_groups_user_name ON address_groups(user_id, name)`,

		// 好友地址索引
		`CREATE INDEX IF NOT EXISTS idx_address_contacts_user_id ON address_contacts(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_address_contacts_group_id ON address_contacts(group_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_address_contacts_user_wallet ON address_contacts(user_id, wallet_address)`,

		// 复制 outbox 索引
		`CREATE INDEX IF NOT EXISTS idx_replication_outbox_pair_pending
			ON replication_outbox(source_node_id, target_node_id, status, next_retry_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_replication_outbox_pair_created
			ON replication_outbox(source_node_id, target_node_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_replication_reconcile_jobs_pair
			ON replication_reconcile_jobs(source_node_id, target_node_id, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_replication_reconcile_items_job_state
			ON replication_reconcile_items(job_id, state, id)`,
		`CREATE INDEX IF NOT EXISTS idx_cluster_nodes_role_heartbeat
			ON cluster_nodes(role, last_heartbeat_at DESC, node_id)`,
		`CREATE INDEX IF NOT EXISTS idx_cluster_replication_assignments_active
			ON cluster_replication_assignments(active_node_id, updated_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_cluster_replication_assignments_standby
			ON cluster_replication_assignments(standby_node_id, updated_at DESC, id DESC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_cluster_replication_assignments_standby_effective
			ON cluster_replication_assignments(standby_node_id)
			WHERE state IN ('pending', 'reconciling', 'replicating', 'draining')`,

		// 兼容已有地址簿表
		`ALTER TABLE address_contacts ADD COLUMN IF NOT EXISTS tags TEXT[] NOT NULL DEFAULT '{}'`,

		// 创建钱包地址索引
		`CREATE INDEX IF NOT EXISTS idx_users_wallet_address ON users(wallet_address) WHERE wallet_address IS NOT NULL`,

		// 创建邮箱索引
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email) WHERE email IS NOT NULL`,

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

		// 创建内部共享表的更新时间触发器
		`DROP TRIGGER IF EXISTS update_internal_share_items_updated_at ON internal_share_items`,
		`CREATE TRIGGER update_internal_share_items_updated_at
		BEFORE UPDATE ON internal_share_items
		FOR EACH ROW
		EXECUTE FUNCTION update_updated_at_column()`,

		// 兼容迁移：将旧 share_user_items 自动回填到统一内部共享模型
		`INSERT INTO internal_share_items (
			id, owner_user_id, owner_username, name, path, is_dir, permissions, expires_at, status, created_at, updated_at
		)
		SELECT
			s.id,
			s.owner_user_id,
			s.owner_username,
			s.name,
			s.path,
			s.is_dir,
			s.permissions,
			s.expires_at,
			'active',
			s.created_at,
			s.created_at
		FROM share_user_items s
		ON CONFLICT (id) DO NOTHING`,
		`INSERT INTO internal_share_audiences (
			id, share_id, audience_type, target_user_id, target_wallet_address, source_group_id, created_at
		)
		SELECT
			'aud_' || md5(s.id || '|user|' || s.target_user_id || '|' || s.target_wallet_address || '|'),
			s.id,
			'user',
			s.target_user_id,
			s.target_wallet_address,
			NULL,
			s.created_at
		FROM share_user_items s
		ON CONFLICT (id) DO NOTHING`,
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
