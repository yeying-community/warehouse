package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
)

func SyncUserSharePathsForOwnerMove(
	ctx context.Context,
	repo repository.UserShareRepository,
	cfg *config.Config,
	owner *user.User,
	fromFullPath, toFullPath string,
) error {
	if repo == nil || cfg == nil || owner == nil {
		return nil
	}

	fromPath, err := ownerShareStoragePath(cfg, owner, fromFullPath)
	if err != nil {
		return err
	}
	toPath, err := ownerShareStoragePath(cfg, owner, toFullPath)
	if err != nil {
		return err
	}
	if fromPath == toPath {
		return nil
	}
	return repo.UpdatePathsForOwnerMove(ctx, owner.ID, fromPath, toPath)
}

func ownerShareStoragePath(cfg *config.Config, owner *user.User, fullPath string) (string, error) {
	rootDir := ownerUserRootDir(cfg, owner)
	rel, err := filepath.Rel(rootDir, filepath.Clean(fullPath))
	if err != nil {
		return "", fmt.Errorf("resolve share move path: %w", err)
	}
	if rel == "." {
		return "/", nil
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("share move path %q is outside owner root %q", fullPath, rootDir)
	}
	return "/" + filepath.ToSlash(rel), nil
}

func ownerUserRootDir(cfg *config.Config, owner *user.User) string {
	userDir := owner.Directory
	if userDir == "" {
		userDir = owner.Username
	}
	if filepath.IsAbs(userDir) {
		return filepath.Clean(userDir)
	}
	return filepath.Join(cfg.WebDAV.Directory, userDir)
}
