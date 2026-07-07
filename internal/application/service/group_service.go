package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/yeying-community/warehouse/internal/domain/group"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
)

type GroupService struct {
	repo repository.GroupRepository
}

func NewGroupService(repo repository.GroupRepository) *GroupService {
	return &GroupService{repo: repo}
}

func (s *GroupService) ListGroups(ctx context.Context, u *user.User) ([]*group.Group, error) {
	return s.repo.ListVisibleGroups(ctx, u.ID, u.WalletAddress)
}

func (s *GroupService) CreateGroup(ctx context.Context, u *user.User, name string) (*group.Group, error) {
	grp, err := group.NewGroup(u.ID, name)
	if err != nil {
		return nil, err
	}
	ownerMemberName := strings.TrimSpace(u.Username)
	if ownerMemberName == "" {
		ownerMemberName = strings.TrimSpace(u.WalletAddress)
	}
	var ownerMember *group.Member
	if strings.TrimSpace(u.WalletAddress) != "" {
		ownerMember, err = group.NewMember(u.ID, grp.ID, ownerMemberName, u.WalletAddress, nil)
		if err != nil {
			return nil, err
		}
		ownerMember.Status = group.MemberStatusActive
	}
	if err := s.repo.CreateGroup(ctx, grp, ownerMember); err != nil {
		return nil, err
	}
	return grp, nil
}

func (s *GroupService) RenameGroup(ctx context.Context, u *user.User, groupID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("group name is required")
	}
	return s.repo.UpdateGroupName(ctx, u.ID, groupID, name)
}

func (s *GroupService) DeleteGroup(ctx context.Context, u *user.User, groupID string) error {
	return s.repo.DeleteGroup(ctx, u.ID, groupID)
}

func (s *GroupService) ListMembers(ctx context.Context, u *user.User) ([]*group.Member, error) {
	return s.repo.ListVisibleMembers(ctx, u.ID, u.WalletAddress)
}

func (s *GroupService) CreateMember(ctx context.Context, u *user.User, name, wallet, groupID string, tags []string) (*group.Member, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil, fmt.Errorf("group id is required")
	}
	targetGroup, err := s.repo.GetVisibleGroupByID(ctx, u.ID, u.WalletAddress, groupID)
	if err != nil {
		return nil, err
	}
	member, err := group.NewMember(targetGroup.UserID, groupID, name, wallet, sanitizeTags(tags))
	if err != nil {
		return nil, err
	}
	if !targetGroup.CanInvite {
		return nil, group.ErrGroupPermissionDenied
	}
	member.Status = group.MemberStatusPending
	if err := s.repo.CreateMember(ctx, member); err != nil {
		return nil, err
	}
	return member, nil
}

func (s *GroupService) UpdateMember(ctx context.Context, u *user.User, id, name, wallet, groupID string, tags *[]string) (*group.Member, error) {
	member, err := s.repo.GetMemberByID(ctx, u.ID, id)
	if err != nil {
		return nil, err
	}
	originalWallet := member.WalletAddress
	originalGroupID := member.GroupID
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
	if !strings.EqualFold(originalWallet, member.WalletAddress) || originalGroupID != member.GroupID {
		member.Status = group.MemberStatusPending
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

func (s *GroupService) DeleteMember(ctx context.Context, u *user.User, id string) error {
	return s.repo.DeleteMember(ctx, u.ID, id)
}

func (s *GroupService) ApproveMember(ctx context.Context, u *user.User, id string) error {
	return s.repo.UpdateMemberStatusByWallet(ctx, u.WalletAddress, id, group.MemberStatusActive)
}

func (s *GroupService) RejectMember(ctx context.Context, u *user.User, id string) error {
	return s.repo.DeleteMemberByWallet(ctx, u.WalletAddress, id)
}
