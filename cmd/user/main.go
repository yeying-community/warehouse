package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/yeying-community/webdav/internal/domain/user"
	"github.com/yeying-community/webdav/internal/infrastructure/config"
	"github.com/yeying-community/webdav/internal/infrastructure/crypto"
	"github.com/yeying-community/webdav/internal/infrastructure/database"
	"github.com/yeying-community/webdav/internal/infrastructure/repository"
)

var (
	configPath = flag.String("config", "config.yaml", "配置文件路径")
	action     = flag.String("action", "", "操作: add, update, delete, list, reset-password")
	username   = flag.String("username", "", "用户名")
	password   = flag.String("password", "", "密码")
	wallet     = flag.String("wallet", "", "钱包地址")
	directory  = flag.String("directory", "", "用户目录")
	perms      = flag.String("permissions", "R", "权限 (C=Create, R=Read, U=Update, D=Delete)")
	quota      = flag.Int64("quota", -1, "配额 (字节)，-1 使用默认值")
)

func main() {
	flag.Parse()

	if *action == "" {
		printUsage()
		os.Exit(1)
	}

	// 加载配置
	loader := config.NewLoader()
	var cfg config.Config
	err := loader.LoadFromFile(*configPath, &cfg)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 创建用户仓储
	var userRepo user.Repository
	var db *database.PostgresDB

	if cfg.Database.Type == "postgres" || cfg.Database.Type == "postgresql" {
		db, err = database.NewPostgresDB(cfg.Database)
		if err != nil {
			log.Fatalf("Failed to connect to database: %v", err)
		}
		defer db.Close()

		userRepo, err = repository.NewPostgresUserRepository(db)
		if err != nil {
			log.Fatalf("Failed to create user repository: %v", err)
		}
	} else {
		log.Fatalf("User management tool only supports PostgreSQL")
	}

	ctx := context.Background()

	// 执行操作
	switch *action {
	case "add":
		if err := addUser(ctx, userRepo); err != nil {
			log.Fatalf("Failed to add user: %v", err)
		}
		fmt.Println("✓ User added successfully!")

	case "update":
		if err := updateUser(ctx, userRepo); err != nil {
			log.Fatalf("Failed to update user: %v", err)
		}
		fmt.Println("✓ User updated successfully!")

	case "delete":
		if err := deleteUser(ctx, userRepo); err != nil {
			log.Fatalf("Failed to delete user: %v", err)
		}
		fmt.Println("✓ User deleted successfully!")

	case "list":
		if err := listUsers(ctx, userRepo); err != nil {
			log.Fatalf("Failed to list users: %v", err)
		}

	case "reset-password":
		if err := resetPassword(ctx, userRepo); err != nil {
			log.Fatalf("Failed to reset password: %v", err)
		}
		fmt.Println("✓ Password reset successfully!")

	default:
		log.Fatalf("Unknown action: %s", *action)
	}
}

func printUsage() {
	fmt.Println("WebDAV User Management Tool")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  user -action <action> [flags]")
	fmt.Println()
	fmt.Println("Actions:")
	fmt.Println("  add              Add a new user")
	fmt.Println("  update           Update an existing user")
	fmt.Println("  delete           Delete a user")
	fmt.Println("  list             List all users")
	fmt.Println("  reset-password   Reset user password")
	fmt.Println()
	fmt.Println("Flags:")
	flag.PrintDefaults()
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  # Add a new user")
	fmt.Println("  user -action add -username alice -password secret123 -directory alice -permissions CRUD -quota 5368709120")
	fmt.Println()
	fmt.Println("  # Add a Web3 user")
	fmt.Println("  user -action add -username bob -wallet 0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb -directory bob -permissions CRUD")
	fmt.Println()
	fmt.Println("  # Update user permissions")
	fmt.Println("  user -action update -username alice -permissions RU")
	fmt.Println()
	fmt.Println("  # Reset password")
	fmt.Println("  user -action reset-password -username alice -password newsecret")
	fmt.Println()
	fmt.Println("  # List all users")
	fmt.Println("  user -action list")
	fmt.Println()
	fmt.Println("  # Delete a user")
	fmt.Println("  user -action delete -username alice")
}

func addUser(ctx context.Context, repo user.Repository) error {
	if *username == "" {
		return fmt.Errorf("username is required")
	}

	if *password == "" && *wallet == "" {
		return fmt.Errorf("either password or wallet address is required")
	}

	// 检查用户是否已存在
	if _, err := repo.FindByUsername(ctx, *username); err == nil {
		return fmt.Errorf("user already exists: %s", *username)
	}

	// 创建用户
	u := user.NewUser(*username, *directory)

	// 设置密码
	if *password != "" {
		hasher := crypto.NewPasswordHasher()
		hashedPassword, err := hasher.Hash(*password)
		if err != nil {
			return fmt.Errorf("failed to hash password: %w", err)
		}
		u.SetPassword(hashedPassword)
	}

	// 设置钱包地址
	if *wallet != "" {
		u.SetWalletAddress(strings.ToLower(*wallet))
	}

	// 设置权限
	if *perms != "" {
		u.Permissions = user.ParsePermissions(*perms)
	}

	// 设置配额
	if *quota >= 0 {
		u.SetQuota(*quota)
	}

	// 保存用户
	if err := repo.Save(ctx, u); err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	return nil
}

func updateUser(ctx context.Context, repo user.Repository) error {
	if *username == "" {
		return fmt.Errorf("username is required")
	}

	// 查找用户
	u, err := repo.FindByUsername(ctx, *username)
	if err != nil {
		return fmt.Errorf("user not found: %s", *username)
	}

	// 更新目录
	if *directory != "" {
		u.Directory = *directory
	}

	// 更新权限
	if *perms != "" {
		u.Permissions = user.ParsePermissions(*perms)
	}

	// 更新配额
	if *quota >= 0 {
		u.SetQuota(*quota)
	}

	// 更新钱包地址
	if *wallet != "" {
		u.SetWalletAddress(strings.ToLower(*wallet))
	}

	// 保存更新
	if err := repo.Save(ctx, u); err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	return nil
}

func deleteUser(ctx context.Context, repo user.Repository) error {
	if *username == "" {
		return fmt.Errorf("username is required")
	}

	// 查找用户
	u, err := repo.FindByUsername(ctx, *username)
	if err != nil {
		return fmt.Errorf("user not found: %s", *username)
	}

	// 删除用户
	if err := repo.Delete(ctx, u.ID); err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	return nil
}

func listUsers(ctx context.Context, repo user.Repository) error {
	users, err := repo.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list users: %w", err)
	}

	if len(users) == 0 {
		fmt.Println("No users found")
		return nil
	}

	fmt.Printf("Total users: %d\n\n", len(users))
	fmt.Printf("%-20s %-10s %-50s %-20s %-15s\n", "Username", "Perms", "Wallet Address", "Directory", "Quota")
	fmt.Println(strings.Repeat("-", 120))

	for _, u := range users {
		walletAddr := u.WalletAddress
		if walletAddr == "" {
			walletAddr = "-"
		}

		dir := u.Directory
		if dir == "" {
			dir = "/"
		}

		quotaStr := "unlimited"
		if u.Quota > 0 {
			quotaStr = formatBytes(u.Quota)
		}

		fmt.Printf("%-20s %-10s %-50s %-20s %-15s\n",
			u.Username,
			u.Permissions.String(),
			walletAddr,
			dir,
			quotaStr,
		)
	}

	return nil
}

func resetPassword(ctx context.Context, repo user.Repository) error {
	if *username == "" {
		return fmt.Errorf("username is required")
	}

	if *password == "" {
		return fmt.Errorf("password is required")
	}

	// 查找用户
	u, err := repo.FindByUsername(ctx, *username)
	if err != nil {
		return fmt.Errorf("user not found: %s", *username)
	}

	// 哈希新密码
	hasher := crypto.NewPasswordHasher()
	hashedPassword, err := hasher.Hash(*password)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// 更新密码
	u.SetPassword(hashedPassword)

	// 保存更新
	if err := repo.Save(ctx, u); err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	return nil
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
