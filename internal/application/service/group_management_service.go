package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/yeying-community/warehouse/internal/domain/groupmanagement"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
)

type GroupManagementService struct {
	repo repository.GroupManagementRepository
}

func NewGroupManagementService(repo repository.GroupManagementRepository) *GroupManagementService {
	return &GroupManagementService{repo: repo}
}

func (s *GroupManagementService) ListGroups(ctx context.Context, u *user.User) ([]*groupmanagement.Group, error) {
	return s.repo.ListVisibleGroups(ctx, u.ID, u.WalletAddress)
}

func (s *GroupManagementService) CreateGroup(ctx context.Context, u *user.User, name string) (*groupmanagement.Group, error) {
	group, err := groupmanagement.NewGroup(u.ID, name)
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateGroup(ctx, group); err != nil {
		return nil, err
	}
	return group, nil
}

func (s *GroupManagementService) RenameGroup(ctx context.Context, u *user.User, groupID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("group name is required")
	}
	return s.repo.UpdateGroupName(ctx, u.ID, groupID, name)
}

func (s *GroupManagementService) DeleteGroup(ctx context.Context, u *user.User, groupID string) error {
	return s.repo.DeleteGroup(ctx, u.ID, groupID)
}

func (s *GroupManagementService) ListMembers(ctx context.Context, u *user.User) ([]*groupmanagement.Member, error) {
	return s.repo.ListVisibleMembers(ctx, u.ID, u.WalletAddress)
}

func (s *GroupManagementService) CreateMember(ctx context.Context, u *user.User, name, wallet, groupID string, tags []string) (*groupmanagement.Member, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil, fmt.Errorf("group id is required")
	}
	group, err := s.repo.GetVisibleGroupByID(ctx, u.ID, u.WalletAddress, groupID)
	if err != nil {
		return nil, err
	}
	member, err := groupmanagement.NewMember(group.UserID, groupID, name, wallet, sanitizeTags(tags))
	if err != nil {
		return nil, err
	}
	if group.UserID != u.ID {
		member.Status = groupmanagement.MemberStatusPending
	}
	if err := s.repo.CreateMember(ctx, member); err != nil {
		return nil, err
	}
	return member, nil
}

func (s *GroupManagementService) UpdateMember(ctx context.Context, u *user.User, id, name, wallet, groupID string, tags *[]string) (*groupmanagement.Member, error) {
	member, err := s.repo.GetMemberByID(ctx, u.ID, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) != "" {
		member.Name = strings.TrimSpace(name)
	}
	if strings.TrimSpace(wallet) != "" {
		member.WalletAddress = strings.ToLower(strings.TrimSpace(wallet))
	}
	if strings.TrimSpace(groupID) != "" {
		if _, err := s.repo.GetGroupByID(ctx, u.ID, groupID); err != nil {
			return nil, err
		}
		member.GroupID = groupID
	}
	if tags != nil {
		member.Tags = sanitizeTags(*tags)
	}
	if err := s.repo.UpdateMember(ctx, member); err != nil {
		return nil, err
	}
	return member, nil
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

func (s *GroupManagementService) DeleteMember(ctx context.Context, u *user.User, id string) error {
	return s.repo.DeleteMember(ctx, u.ID, id)
}

func (s *GroupManagementService) ApproveMember(ctx context.Context, u *user.User, id string) error {
	return s.repo.UpdateMemberStatus(ctx, u.ID, id, groupmanagement.MemberStatusActive)
}

func (s *GroupManagementService) RejectMember(ctx context.Context, u *user.User, id string) error {
	return s.repo.DeleteMember(ctx, u.ID, id)
}
