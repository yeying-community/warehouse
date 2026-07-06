package addressbook

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrGroupNotFound      = errors.New("group not found")
	ErrMemberNotFound     = errors.New("member not found")
	ErrDuplicateGroupName = errors.New("group name already exists")
	ErrDuplicateMember    = errors.New("member already exists in group")
)

type Group struct {
	ID        string
	UserID    string
	Name      string
	CreatedAt time.Time
}

type Member struct {
	ID            string
	UserID        string
	GroupID       string
	Name          string
	WalletAddress string
	Tags          []string
	CreatedAt     time.Time
}

func NewGroup(userID, name string) (*Group, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("group name is required")
	}
	now := time.Now()
	return &Group{
		ID:        uuid.NewString(),
		UserID:    userID,
		Name:      name,
		CreatedAt: now,
	}, nil
}

func NewMember(userID, groupID, name, walletAddress string, tags []string) (*Member, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil, errors.New("group id is required")
	}
	name = strings.TrimSpace(name)
	walletAddress = strings.TrimSpace(walletAddress)
	if walletAddress == "" {
		return nil, errors.New("wallet address is required")
	}
	if name == "" {
		name = strings.ToLower(walletAddress)
	}
	now := time.Now()
	return &Member{
		ID:            uuid.NewString(),
		UserID:        userID,
		GroupID:       groupID,
		Name:          name,
		WalletAddress: strings.ToLower(walletAddress),
		Tags:          tags,
		CreatedAt:     now,
	}, nil
}
