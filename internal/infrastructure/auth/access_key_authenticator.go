package auth

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/accesskey"
	domainauth "github.com/yeying-community/warehouse/internal/domain/auth"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/crypto"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

type AccessKeyAuthenticator struct {
	userRepo user.Repository
	keyRepo  repository.WebDAVAccessKeyRepository
	hasher   *crypto.PasswordHasher
	logger   *zap.Logger
	now      func() time.Time
}

func NewAccessKeyAuthenticator(
	userRepo user.Repository,
	keyRepo repository.WebDAVAccessKeyRepository,
	logger *zap.Logger,
) *AccessKeyAuthenticator {
	return &AccessKeyAuthenticator{
		userRepo: userRepo,
		keyRepo:  keyRepo,
		hasher:   crypto.NewPasswordHasher(),
		logger:   logger,
		now:      time.Now,
	}
}

func (a *AccessKeyAuthenticator) Name() string {
	return "webdav-access-key"
}

func (a *AccessKeyAuthenticator) CanHandle(credentials interface{}) bool {
	creds, ok := credentials.(*domainauth.BasicCredentials)
	if !ok {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(creds.Username), "ak_")
}

func (a *AccessKeyAuthenticator) Authenticate(ctx context.Context, credentials interface{}) (*user.User, error) {
	creds, ok := credentials.(*domainauth.BasicCredentials)
	if !ok {
		return nil, fmt.Errorf("invalid credentials type")
	}
	keyID := strings.TrimSpace(creds.Username)
	secret := strings.TrimSpace(creds.Password)
	if keyID == "" || secret == "" {
		return nil, domainauth.ErrInvalidCredentials
	}

	key, err := a.keyRepo.FindByKeyID(ctx, keyID)
	if err != nil {
		if err == accesskey.ErrNotFound {
			return nil, domainauth.ErrInvalidCredentials
		}
		return nil, fmt.Errorf("find access key: %w", err)
	}
	if key.Status != accesskey.StatusActive {
		return nil, domainauth.ErrInvalidCredentials
	}
	now := a.now()
	if key.IsExpired(now) {
		return nil, accesskey.ErrAccessKeyExpired
	}
	if err := a.hasher.Verify(key.SecretHash, secret); err != nil {
		return nil, domainauth.ErrInvalidCredentials
	}

	owner, err := a.userRepo.FindByID(ctx, key.OwnerUserID)
	if err != nil {
		return nil, err
	}
	bindingPaths, err := a.keyRepo.ListBindingPathsByAccessKeyID(ctx, key.ID)
	if err != nil {
		return nil, fmt.Errorf("list access key bindings: %w", err)
	}
	if len(bindingPaths) == 0 {
		return nil, domainauth.ErrInvalidCredentials
	}

	scoped := *owner
	scoped.Permissions = user.ParsePermissions("")
	scoped.Rules = buildScopedRules(owner, bindingPaths, key.Permissions)
	scoped.Directory = resolveOwnerDirectory(owner)

	if err := a.keyRepo.TouchByID(ctx, key.ID, now); err != nil {
		a.logger.Warn("failed to update access key last used time",
			zap.String("key_id", key.KeyID),
			zap.Error(err))
	}

	return &scoped, nil
}

func (a *AccessKeyAuthenticator) EnrichContext(ctx context.Context, credentials interface{}) context.Context {
	creds, ok := credentials.(*domainauth.BasicCredentials)
	if !ok {
		return ctx
	}
	keyID := strings.TrimSpace(creds.Username)
	if !strings.HasPrefix(keyID, "ak_") {
		return ctx
	}
	return middleware.WithAccessKeyContext(ctx, &middleware.AccessKeyContext{KeyID: keyID})
}

func resolveOwnerDirectory(owner *user.User) string {
	base := strings.TrimSpace(owner.Directory)
	if base == "" {
		base = strings.TrimSpace(owner.Username)
	}
	return base
}

func buildScopedRules(owner *user.User, bindingPaths []string, permissions string) []*user.Rule {
	base := normalizeRulePath(resolveOwnerDirectory(owner))
	if base == "" {
		base = "/"
	}
	perms := user.ParsePermissions(permissions)
	rules := make([]*user.Rule, 0, len(bindingPaths))
	for _, raw := range bindingPaths {
		normalized := normalizeRulePath(raw)
		target := base
		if normalized != "/" {
			target = joinRulePath(base, strings.TrimPrefix(normalized, "/"))
		}
		pattern := fmt.Sprintf("^%s(/|$)", regexp.QuoteMeta(target))
		rules = append(rules, &user.Rule{
			Path:        pattern,
			Permissions: perms,
			Regex:       true,
		})
	}
	return rules
}

func normalizeRulePath(raw string) string {
	clean := strings.TrimSpace(raw)
	clean = strings.ReplaceAll(clean, "\\", "/")
	if clean == "" {
		return "/"
	}
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	clean = path.Clean(clean)
	if clean == "." {
		return "/"
	}
	return clean
}

func joinRulePath(base, rel string) string {
	base = normalizeRulePath(base)
	rel = strings.TrimPrefix(strings.TrimSpace(rel), "/")
	if rel == "" {
		return base
	}
	if base == "/" {
		return normalizeRulePath("/" + rel)
	}
	return normalizeRulePath(path.Join(base, rel))
}
