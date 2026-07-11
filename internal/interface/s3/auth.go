package s3

import (
	"context"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/s3credential"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
)

// CredentialResolver isolates S3 credential storage from Signature V4.
// Implementations must not log or expose the secret beyond verification.
type CredentialResolver interface {
	Resolve(ctx context.Context, accessKeyID string) (*s3credential.Credential, error)
}

func AccessKeyIDFromAuthorization(raw string) (string, error) {
	authorization, err := parseAuthorization(raw)
	if err != nil {
		return "", err
	}
	return authorization.accessKeyID, nil
}

type RepositoryCredentialResolver struct {
	repo repository.S3CredentialRepository
}

func NewRepositoryCredentialResolver(repo repository.S3CredentialRepository) *RepositoryCredentialResolver {
	return &RepositoryCredentialResolver{repo: repo}
}

func (r *RepositoryCredentialResolver) Resolve(ctx context.Context, accessKeyID string) (*s3credential.Credential, error) {
	if r == nil || r.repo == nil {
		return nil, s3credential.ErrNotFound
	}
	return r.repo.FindByAccessKeyID(ctx, accessKeyID)
}

type StaticCredentialResolver struct {
	credential s3credential.Credential
}

func NewStaticCredentialResolver(credential s3credential.Credential) *StaticCredentialResolver {
	return &StaticCredentialResolver{credential: credential}
}

func (r *StaticCredentialResolver) Resolve(ctx context.Context, accessKeyID string) (*s3credential.Credential, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.credential.AccessKeyID != accessKeyID {
		return nil, s3credential.ErrNotFound
	}
	credential := r.credential
	if err := credential.Validate(time.Now()); err != nil {
		return nil, err
	}
	return &credential, nil
}
