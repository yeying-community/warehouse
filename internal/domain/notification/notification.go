package notification

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	RecipientRoleUser  = "user"
	RecipientRoleAdmin = "admin"
	RecipientRoleAll   = "all"

	SeverityInfo    = "info"
	SeverityWarning = "warning"
	SeverityError   = "error"

	TypeQuota       = "quota"
	TypeShare       = "share"
	TypeSystem      = "system"
	TypeAdminNotice = "admin_notice"
)

var PreferenceTypes = []string{
	TypeQuota,
	TypeShare,
	TypeSystem,
	TypeAdminNotice,
}

type Notification struct {
	ID              string
	RecipientUserID string
	RecipientRole   string
	Type            string
	Title           string
	Content         string
	Severity        string
	ActionURL       string
	DedupeKey       string
	ReadAt          *time.Time
	CreatedAt       time.Time
	ExpiresAt       *time.Time
}

type Preference struct {
	Type    string
	Enabled bool
}

type CreateInput struct {
	RecipientUserID string
	RecipientRole   string
	Type            string
	Title           string
	Content         string
	Severity        string
	ActionURL       string
	DedupeKey       string
	ExpiresAt       *time.Time
}

func New(input CreateInput) *Notification {
	now := time.Now()
	role := strings.TrimSpace(input.RecipientRole)
	if role == "" {
		role = RecipientRoleUser
	}
	severity := strings.TrimSpace(input.Severity)
	if severity == "" {
		severity = SeverityInfo
	}
	return &Notification{
		ID:              uuid.NewString(),
		RecipientUserID: strings.TrimSpace(input.RecipientUserID),
		RecipientRole:   role,
		Type:            strings.TrimSpace(input.Type),
		Title:           strings.TrimSpace(input.Title),
		Content:         strings.TrimSpace(input.Content),
		Severity:        severity,
		ActionURL:       strings.TrimSpace(input.ActionURL),
		DedupeKey:       strings.TrimSpace(input.DedupeKey),
		CreatedAt:       now,
		ExpiresAt:       input.ExpiresAt,
	}
}
