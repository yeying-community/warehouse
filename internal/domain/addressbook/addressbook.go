package addressbook

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrGroupNotFound      = errors.New("group not found")
	ErrContactNotFound    = errors.New("contact not found")
	ErrDuplicateGroupName = errors.New("group name already exists")
	ErrDuplicateWallet    = errors.New("wallet address already exists")
)

const (
	GroupTypePersonal = "personal"
	GroupTypeTeam     = "team"
)

type Group struct {
	ID        string
	UserID    string
	Name      string
	Type      string
	Role      string
	CanManage bool
	CreatedAt time.Time
}

type Contact struct {
	ID            string
	UserID        string
	GroupID       string
	Name          string
	WalletAddress string
	Tags          []string
	GroupType     string
	CanManage     bool
	CreatedAt     time.Time
}

func NormalizeGroupType(raw string) (string, error) {
	groupType := strings.ToLower(strings.TrimSpace(raw))
	if groupType == "" {
		return GroupTypePersonal, nil
	}
	switch groupType {
	case GroupTypePersonal, GroupTypeTeam:
		return groupType, nil
	default:
		return "", errors.New("invalid group type")
	}
}

func NewGroup(userID, name, groupType string) (*Group, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("group name is required")
	}
	normalizedType, err := NormalizeGroupType(groupType)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	return &Group{
		ID:        uuid.NewString(),
		UserID:    userID,
		Name:      name,
		Type:      normalizedType,
		Role:      "owner",
		CanManage: true,
		CreatedAt: now,
	}, nil
}

func NewContact(userID, groupID, name, walletAddress string, tags []string) (*Contact, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("contact name is required")
	}
	walletAddress = strings.TrimSpace(walletAddress)
	if walletAddress == "" {
		return nil, errors.New("wallet address is required")
	}
	now := time.Now()
	return &Contact{
		ID:            uuid.NewString(),
		UserID:        userID,
		GroupID:       groupID,
		Name:          name,
		WalletAddress: strings.ToLower(walletAddress),
		Tags:          tags,
		GroupType:     GroupTypePersonal,
		CanManage:     true,
		CreatedAt:     now,
	}, nil
}
