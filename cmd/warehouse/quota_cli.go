package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/pflag"
	appservice "github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/quota"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/database"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
)

func runQuotaCommand(args []string) error {
	if len(args) == 0 {
		printQuotaHelp()
		return nil
	}

	switch args[0] {
	case "check":
		return runQuotaCheck(args[1:])
	case "rebuild":
		return runQuotaRebuild(args[1:])
	case "-h", "--help", "help":
		printQuotaHelp()
		return nil
	default:
		return fmt.Errorf("unsupported quota subcommand %q", args[0])
	}
}

func printQuotaHelp() {
	fmt.Println("Usage:")
	fmt.Println("  warehouse quota check -c config.yaml --username USERNAME")
	fmt.Println("  warehouse quota rebuild -c config.yaml --username USERNAME")
}

func runQuotaCheck(args []string) error {
	flags := newQuotaFlags("quota-check")
	username := flags.String("username", "", "Target username")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if help, _ := flags.GetBool("help"); help {
		fmt.Println("Usage:")
		fmt.Println("  warehouse quota check -c config.yaml --username USERNAME")
		return nil
	}
	if strings.TrimSpace(*username) == "" {
		return fmt.Errorf("--username is required")
	}

	cfg, db, userRepo, quotaSvc, recycleRepo, err := buildQuotaDependencies(flags)
	if err != nil {
		return err
	}
	defer db.Close()

	u, err := userRepo.FindByUsername(context.Background(), strings.TrimSpace(*username))
	if err != nil {
		return err
	}

	snapshot, err := appservice.CalculateQuotaUsage(context.Background(), cfg, quotaSvc, recycleRepo, u)
	if err != nil {
		return err
	}

	response := map[string]any{
		"username":            u.Username,
		"user_id":             u.ID,
		"quota":               u.Quota,
		"stored_used_space":   u.UsedSpace,
		"recalculated_used":   snapshot.TotalUsed,
		"active_used":         snapshot.ActiveUsed,
		"recycle_used":        snapshot.RecycleUsed,
		"drift":               snapshot.TotalUsed - u.UsedSpace,
		"matches":             snapshot.TotalUsed == u.UsedSpace,
		"unlimited":           u.Quota == 0,
		"user_directory_root": snapshot.UserDir,
	}
	printPrettyJSONFromAny(response)
	return nil
}

func runQuotaRebuild(args []string) error {
	flags := newQuotaFlags("quota-rebuild")
	username := flags.String("username", "", "Target username")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if help, _ := flags.GetBool("help"); help {
		fmt.Println("Usage:")
		fmt.Println("  warehouse quota rebuild -c config.yaml --username USERNAME")
		return nil
	}
	if strings.TrimSpace(*username) == "" {
		return fmt.Errorf("--username is required")
	}

	cfg, db, userRepo, quotaSvc, recycleRepo, err := buildQuotaDependencies(flags)
	if err != nil {
		return err
	}
	defer db.Close()

	u, err := userRepo.FindByUsername(context.Background(), strings.TrimSpace(*username))
	if err != nil {
		return err
	}

	snapshot, err := appservice.CalculateQuotaUsage(context.Background(), cfg, quotaSvc, recycleRepo, u)
	if err != nil {
		return err
	}
	before := u.UsedSpace
	if err := userRepo.UpdateUsedSpace(context.Background(), u.Username, snapshot.TotalUsed); err != nil {
		return err
	}

	response := map[string]any{
		"username":          u.Username,
		"user_id":           u.ID,
		"quota":             u.Quota,
		"before_used_space": before,
		"after_used_space":  snapshot.TotalUsed,
		"active_used":       snapshot.ActiveUsed,
		"recycle_used":      snapshot.RecycleUsed,
		"delta":             snapshot.TotalUsed - before,
		"unlimited":         u.Quota == 0,
	}
	printPrettyJSONFromAny(response)
	return nil
}

func newQuotaFlags(name string) *pflag.FlagSet {
	flags := pflag.NewFlagSet(name, pflag.ContinueOnError)
	flags.StringP("config", "c", "", "Config file path")
	flags.BoolP("help", "h", false, "Show help")
	return flags
}

func buildQuotaDependencies(flags *pflag.FlagSet) (*config.Config, *database.PostgresDB, user.Repository, quota.Service, repository.RecycleRepository, error) {
	configFile, _ := flags.GetString("config")
	cfg, err := loadConfig(configFile, flags)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	db, err := database.NewPostgresDB(cfg.Database)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("connect database: %w", err)
	}

	userRepo, err := repository.NewPostgresUserRepository(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, nil, err
	}
	recycleRepo := repository.NewPostgresRecycleRepository(db.DB)
	quotaSvc := quota.NewService(userRepo)

	return cfg, db, userRepo, quotaSvc, recycleRepo, nil
}
