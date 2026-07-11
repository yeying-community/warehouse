package s3credential

import (
	"errors"
	"testing"
	"time"
)

func TestCredentialValidate(t *testing.T) {
	valid := Credential{OwnerUserID: "user-1", AccessKeyID: "AKID", Secret: "secret", Status: StatusActive}
	if err := valid.Validate(time.Now()); err != nil {
		t.Fatalf("validate credential: %v", err)
	}

	revoked := valid
	revoked.Status = StatusRevoked
	if !errors.Is(revoked.Validate(time.Now()), ErrRevoked) {
		t.Fatalf("expected revoked error")
	}

	expired := valid
	deadline := time.Now().Add(-time.Minute)
	expired.ExpiresAt = &deadline
	if !errors.Is(expired.Validate(time.Now()), ErrExpired) {
		t.Fatalf("expected expired error")
	}
}
