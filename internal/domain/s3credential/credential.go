package s3credential

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrNotFound          = errors.New("s3 credential not found")
	ErrInvalidCredential = errors.New("invalid s3 credential")
	ErrRevoked           = errors.New("s3 credential revoked")
	ErrExpired           = errors.New("s3 credential expired")
)

const StatusActive = "active"
const StatusRevoked = "revoked"

// Credential contains the minimum authenticated identity needed by S3.
// Secret is resolved only at verification time and must never be logged.
type Credential struct {
	ID               string
	OwnerUserID      string
	Name             string
	AccessKeyID      string
	Secret           string
	SecretKeyVersion int
	RootPath         string
	Permissions      string
	Status           string
	ExpiresAt        *time.Time
	LastUsedAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (c *Credential) Validate(now time.Time) error {
	if c == nil || strings.TrimSpace(c.OwnerUserID) == "" || strings.TrimSpace(c.AccessKeyID) == "" || strings.TrimSpace(c.Secret) == "" {
		return ErrInvalidCredential
	}
	if c.Status != StatusActive {
		return ErrRevoked
	}
	if c.ExpiresAt != nil && now.After(*c.ExpiresAt) {
		return ErrExpired
	}
	return nil
}
