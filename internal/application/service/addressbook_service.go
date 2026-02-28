package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/yeying-community/warehouse/internal/domain/addressbook"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
)

type AddressBookService struct {
	repo repository.AddressBookRepository
}

func NewAddressBookService(repo repository.AddressBookRepository) *AddressBookService {
	return &AddressBookService{repo: repo}
}

func (s *AddressBookService) ListGroups(ctx context.Context, u *user.User) ([]*addressbook.Group, error) {
	return s.repo.ListGroupsByUser(ctx, u.ID)
}

func (s *AddressBookService) CreateGroup(ctx context.Context, u *user.User, name string) (*addressbook.Group, error) {
	group, err := addressbook.NewGroup(u.ID, name)
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateGroup(ctx, group); err != nil {
		return nil, err
	}
	return group, nil
}

func (s *AddressBookService) RenameGroup(ctx context.Context, u *user.User, groupID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("group name is required")
	}
	return s.repo.UpdateGroupName(ctx, u.ID, groupID, name)
}

func (s *AddressBookService) DeleteGroup(ctx context.Context, u *user.User, groupID string) error {
	return s.repo.DeleteGroup(ctx, u.ID, groupID)
}

func (s *AddressBookService) ListContacts(ctx context.Context, u *user.User) ([]*addressbook.Contact, error) {
	return s.repo.ListContactsByUser(ctx, u.ID)
}

func (s *AddressBookService) CreateContact(ctx context.Context, u *user.User, name, wallet, groupID string, tags []string) (*addressbook.Contact, error) {
	if strings.TrimSpace(groupID) != "" {
		if _, err := s.repo.GetGroupByID(ctx, u.ID, groupID); err != nil {
			return nil, err
		}
	}
	contact, err := addressbook.NewContact(u.ID, groupID, name, wallet, sanitizeTags(tags))
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateContact(ctx, contact); err != nil {
		return nil, err
	}
	return contact, nil
}

func (s *AddressBookService) UpdateContact(ctx context.Context, u *user.User, id, name, wallet, groupID string, tags *[]string) (*addressbook.Contact, error) {
	contact, err := s.repo.GetContactByID(ctx, u.ID, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) != "" {
		contact.Name = strings.TrimSpace(name)
	}
	if strings.TrimSpace(wallet) != "" {
		contact.WalletAddress = strings.ToLower(strings.TrimSpace(wallet))
	}
	if groupID != "" {
		if _, err := s.repo.GetGroupByID(ctx, u.ID, groupID); err != nil {
			return nil, err
		}
		contact.GroupID = groupID
	} else {
		contact.GroupID = ""
	}
	if tags != nil {
		contact.Tags = sanitizeTags(*tags)
	}
	if err := s.repo.UpdateContact(ctx, contact); err != nil {
		return nil, err
	}
	return contact, nil
}

func sanitizeTags(input []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(input))
	for _, raw := range input {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, tag)
	}
	return result
}

func (s *AddressBookService) DeleteContact(ctx context.Context, u *user.User, id string) error {
	return s.repo.DeleteContact(ctx, u.ID, id)
}
