package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/accesskey"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/crypto"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
)

type WebDAVAccessKeyService struct {
	repo   repository.WebDAVAccessKeyRepository
	hasher *crypto.PasswordHasher
}

func NewWebDAVAccessKeyService(repo repository.WebDAVAccessKeyRepository) *WebDAVAccessKeyService {
	return &WebDAVAccessKeyService{
		repo:   repo,
		hasher: crypto.NewPasswordHasher(),
	}
}

type CreateWebDAVAccessKeyInput struct {
	Name        string
	Permissions []string
	Expiry      ShareExpiryInput
}

func (s *WebDAVAccessKeyService) Create(ctx context.Context, owner *user.User, input CreateWebDAVAccessKeyInput) (*accesskey.WebDAVAccessKey, string, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, "", accesskey.ErrInvalidName
	}

	perms, err := normalizePermissionList(input.Permissions)
	if err != nil {
		return nil, "", err
	}

	expiresAt, err := input.Expiry.Resolve(time.Now())
	if err != nil {
		return nil, "", err
	}

	keyID, secret, err := generateAccessKeyPair()
	if err != nil {
		return nil, "", err
	}
	secretHash, err := s.hasher.Hash(secret)
	if err != nil {
		return nil, "", err
	}

	item, err := accesskey.New(owner.ID, name, keyID, secretHash, "/", perms, expiresAt)
	if err != nil {
		return nil, "", err
	}
	if err := s.repo.Create(ctx, item); err != nil {
		return nil, "", err
	}
	return item, secret, nil
}

func (s *WebDAVAccessKeyService) List(ctx context.Context, owner *user.User) ([]*accesskey.WebDAVAccessKey, error) {
	return s.repo.ListByOwner(ctx, owner.ID)
}

func (s *WebDAVAccessKeyService) Revoke(ctx context.Context, owner *user.User, id string) error {
	item, err := s.repo.GetByID(ctx, owner.ID, id)
	if err != nil {
		return err
	}
	if item.Status == accesskey.StatusRevoked {
		return accesskey.ErrAlreadyRevoked
	}
	return s.repo.RevokeByID(ctx, owner.ID, id)
}

func (s *WebDAVAccessKeyService) BindPath(ctx context.Context, owner *user.User, id, rootPath string) error {
	item, err := s.repo.GetByID(ctx, owner.ID, id)
	if err != nil {
		return err
	}
	if item.Status == accesskey.StatusRevoked {
		return accesskey.ErrAlreadyRevoked
	}
	normalized, err := accesskey.NormalizeRootPath(rootPath)
	if err != nil {
		return err
	}
	return s.repo.BindPath(ctx, owner.ID, id, normalized)
}

func (s *WebDAVAccessKeyService) ListBindingPaths(ctx context.Context, owner *user.User, id string) ([]string, error) {
	_, err := s.repo.GetByID(ctx, owner.ID, id)
	if err != nil {
		return nil, err
	}
	return s.repo.ListBindingPathsByAccessKeyID(ctx, id)
}

func normalizePermissionList(items []string) (string, error) {
	if len(items) == 1 && looksLikePermissionString(items[0]) {
		perms := user.ParsePermissions(strings.ToUpper(strings.TrimSpace(items[0])))
		if !perms.Read && !perms.Create && !perms.Update && !perms.Delete {
			perms.Read = true
		}
		return perms.String(), nil
	}

	perms := user.Permissions{}
	for _, item := range items {
		switch strings.ToLower(strings.TrimSpace(item)) {
		case "read", "r":
			perms.Read = true
		case "create", "c":
			perms.Create = true
		case "update", "u":
			perms.Update = true
		case "delete", "d":
			perms.Delete = true
		case "":
			continue
		default:
			return "", fmt.Errorf("invalid permission: %s", item)
		}
	}
	if !perms.Read && !perms.Create && !perms.Update && !perms.Delete {
		perms.Read = true
	}
	return perms.String(), nil
}

func looksLikePermissionString(s string) bool {
	s = strings.ToUpper(strings.TrimSpace(s))
	for _, ch := range s {
		if ch != 'C' && ch != 'R' && ch != 'U' && ch != 'D' {
			return false
		}
	}
	return s != ""
}

func generateAccessKeyPair() (string, string, error) {
	keyIDRaw := make([]byte, 8)
	secretRaw := make([]byte, 24)
	if _, err := rand.Read(keyIDRaw); err != nil {
		return "", "", fmt.Errorf("generate key id: %w", err)
	}
	if _, err := rand.Read(secretRaw); err != nil {
		return "", "", fmt.Errorf("generate key secret: %w", err)
	}
	keyID := "ak_" + strings.ToLower(hex.EncodeToString(keyIDRaw))
	secret := "sk_" + strings.ToLower(hex.EncodeToString(secretRaw))
	return keyID, secret, nil
}
