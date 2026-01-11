package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/yeying-community/webdav/internal/infrastructure/config"
	"github.com/yeying-community/webdav/internal/infrastructure/database"
)

var (
	configPath    = flag.String("config", "config.yaml", "配置文件路径")
	action        = flag.String("action", "up", "迁移动作: up 或 down")
	targetVersion = flag.String("version", "0", "目标版本 (0 表示最新)")

	// 构建信息（由 ldflags 注入）
	version   string
	buildTime string
	gitCommit string
)

func main() {
	flag.Parse()

	// 打印版本信息（如果提供了）
	if version != "" {
		fmt.Printf("WebDAV Migrate Tool\n")
		fmt.Printf("Version: %s\n", version)
		fmt.Printf("Build Time: %s\n", buildTime)
		fmt.Printf("Git Commit: %s\n\n", gitCommit)
	}

	// 加载配置
	loader := config.NewLoader()
	var cfg config.Config
	err := loader.LoadFromFile(*configPath, &cfg)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 检查数据库类型
	if cfg.Database.Type != "postgres" && cfg.Database.Type != "postgresql" {
		log.Fatalf("Migration only supports PostgreSQL, current type: %s", cfg.Database.Type)
	}

	// 连接数据库
	db, err := database.NewPostgresDB(cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 执行迁移
	switch *action {
	case "up":
		fmt.Println("Running migrations...")
		if err := db.Migrate(ctx); err != nil {
			log.Fatalf("Migration failed: %v", err)
		}
		fmt.Println("✓ Migration completed successfully!")

	case "down":
		fmt.Println("Rolling back migrations...")
		if err := rollbackMigrations(ctx, db); err != nil {
			log.Fatalf("Rollback failed: %v", err)
		}
		fmt.Println("✓ Rollback completed successfully!")

	case "status":
		fmt.Println("Checking migration status...")
		if err := checkMigrationStatus(ctx, db); err != nil {
			log.Fatalf("Status check failed: %v", err)
		}

	default:
		log.Fatalf("Unknown action: %s (use 'up', 'down', or 'status')", *action)
	}
}

// rollbackMigrations 回滚迁移
func rollbackMigrations(ctx context.Context, db *database.PostgresDB) error {
	queries := []string{
		"DROP TABLE IF EXISTS recycle_items CASCADE",
		"DROP TABLE IF EXISTS user_rules CASCADE",
		"DROP TABLE IF EXISTS users CASCADE",
		"DROP TRIGGER IF EXISTS update_users_updated_at ON users",
		"DROP FUNCTION IF EXISTS update_updated_at_column()",
	}

	for _, query := range queries {
		fmt.Printf("Executing: %s\n", query)
		if _, err := db.DB.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to execute rollback query: %w", err)
		}
	}

	return nil
}

// checkMigrationStatus 检查迁移状态
func checkMigrationStatus(ctx context.Context, db *database.PostgresDB) error {
	tables := []string{"users", "user_rules"}

	for _, table := range tables {
		var exists bool
		query := `
			SELECT EXISTS (
				SELECT FROM information_schema.tables
				WHERE table_schema = 'public'
				AND table_name = $1
			)
		`
		if err := db.DB.QueryRowContext(ctx, query, table).Scan(&exists); err != nil {
			return fmt.Errorf("failed to check table %s: %w", table, err)
		}

		if exists {
			fmt.Printf("✓ Table '%s' exists\n", table)

			// 获取记录数
			var count int
			countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
			if err := db.DB.QueryRowContext(ctx, countQuery).Scan(&count); err != nil {
				return fmt.Errorf("failed to count records in %s: %w", table, err)
			}
			fmt.Printf("  Records: %d\n", count)
		} else {
			fmt.Printf("✗ Table '%s' does not exist\n", table)
		}
	}

	return nil
}
