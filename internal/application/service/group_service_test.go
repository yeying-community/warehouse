package service

import (
	"context"
	"strings"
	"testing"

	"github.com/yeying-community/warehouse/internal/domain/group"
	"github.com/yeying-community/warehouse/internal/domain/user"
)

func TestGroupServiceCreateGroupAddsOwnerAsActiveMember(t *testing.T) {
	ctx := context.Background()
	repo := newFakeGroupRepository()
	svc := NewGroupService(repo)
	owner := &user.User{ID: "owner-user", Username: "Owner", WalletAddress: "0xowner"}

	grp, err := svc.CreateGroup(ctx, owner, "team")
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}

	var ownerMember *group.Member
	for _, member := range repo.members {
		if member.GroupID == grp.ID && strings.EqualFold(member.WalletAddress, owner.WalletAddress) {
			ownerMember = member
			break
		}
	}
	if ownerMember == nil {
		t.Fatal("owner member was not created")
	}
	if ownerMember.Status != group.MemberStatusActive {
		t.Fatalf("owner member status = %q, want %q", ownerMember.Status, group.MemberStatusActive)
	}
	if ownerMember.Tags == nil {
		t.Fatal("owner member tags should be an empty slice, got nil")
	}
}

func TestGroupServiceListGroupsRequiresInviteBeforeTargetCanSeeGroup(t *testing.T) {
	ctx := context.Background()
	repo := newFakeGroupRepository()
	svc := NewGroupService(repo)
	owner := &user.User{ID: "owner-user", WalletAddress: "0xowner"}
	invited := &user.User{ID: "invited-user", WalletAddress: "0xinvited"}

	grp, err := svc.CreateGroup(ctx, owner, "team")
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}

	groups, err := svc.ListGroups(ctx, invited)
	if err != nil {
		t.Fatalf("ListGroups() before invite error = %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("ListGroups() before invite returned %d groups, want 0", len(groups))
	}

	if _, err := svc.CreateMember(ctx, owner, "Invited", invited.WalletAddress, grp.ID, nil); err != nil {
		t.Fatalf("CreateMember() error = %v", err)
	}
	groups, err = svc.ListGroups(ctx, invited)
	if err != nil {
		t.Fatalf("ListGroups() after invite error = %v", err)
	}
	if len(groups) != 1 || groups[0].ID != grp.ID {
		t.Fatalf("ListGroups() after invite = %#v, want group %s", groups, grp.ID)
	}
}

func TestGroupServiceRejectInviteHidesGroupFromTarget(t *testing.T) {
	ctx := context.Background()
	repo := newFakeGroupRepository()
	svc := NewGroupService(repo)
	owner := &user.User{ID: "owner-user", WalletAddress: "0xowner"}
	invited := &user.User{ID: "invited-user", WalletAddress: "0xinvited"}

	grp, err := svc.CreateGroup(ctx, owner, "team")
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	member, err := svc.CreateMember(ctx, owner, "Invited", invited.WalletAddress, grp.ID, nil)
	if err != nil {
		t.Fatalf("CreateMember() error = %v", err)
	}

	if err := svc.RejectMember(ctx, invited, member.ID); err != nil {
		t.Fatalf("RejectMember() error = %v", err)
	}
	groups, err := svc.ListGroups(ctx, invited)
	if err != nil {
		t.Fatalf("ListGroups() after reject error = %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("ListGroups() after reject returned %d groups, want 0", len(groups))
	}
}

func TestGroupServiceMemberInviteRequiresTargetConfirmation(t *testing.T) {
	ctx := context.Background()
	repo := newFakeGroupRepository()
	svc := NewGroupService(repo)
	owner := &user.User{ID: "owner-user", WalletAddress: "0xowner"}
	invited := &user.User{ID: "invited-user", WalletAddress: "0xinvited"}
	other := &user.User{ID: "other-user", WalletAddress: "0xother"}

	grp, err := group.NewGroup(owner.ID, "team")
	if err != nil {
		t.Fatalf("NewGroup() error = %v", err)
	}
	repo.groups[grp.ID] = grp

	member, err := svc.CreateMember(ctx, owner, "Invited", invited.WalletAddress, grp.ID, nil)
	if err != nil {
		t.Fatalf("CreateMember() error = %v", err)
	}
	if member.Status != group.MemberStatusPending {
		t.Fatalf("CreateMember() status = %q, want %q", member.Status, group.MemberStatusPending)
	}

	if err := svc.ApproveMember(ctx, other, member.ID); err != group.ErrMemberNotFound {
		t.Fatalf("ApproveMember() by unrelated wallet error = %v, want %v", err, group.ErrMemberNotFound)
	}
	if got := repo.members[member.ID].Status; got != group.MemberStatusPending {
		t.Fatalf("status after unrelated approve = %q, want pending", got)
	}

	if err := svc.ApproveMember(ctx, invited, member.ID); err != nil {
		t.Fatalf("ApproveMember() by invited wallet error = %v", err)
	}
	if got := repo.members[member.ID].Status; got != group.MemberStatusActive {
		t.Fatalf("status after invited approve = %q, want active", got)
	}
}

func TestGroupServiceActiveMemberInviteRequiresTargetConfirmation(t *testing.T) {
	ctx := context.Background()
	repo := newFakeGroupRepository()
	svc := NewGroupService(repo)
	owner := &user.User{ID: "owner-user", WalletAddress: "0xowner"}
	memberUser := &user.User{ID: "member-user", WalletAddress: "0xmember"}
	invited := &user.User{ID: "invited-user", WalletAddress: "0xinvited"}

	grp, err := svc.CreateGroup(ctx, owner, "team")
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	member, err := svc.CreateMember(ctx, owner, "Member", memberUser.WalletAddress, grp.ID, nil)
	if err != nil {
		t.Fatalf("CreateMember(owner invite) error = %v", err)
	}
	if err := svc.ApproveMember(ctx, memberUser, member.ID); err != nil {
		t.Fatalf("ApproveMember(member) error = %v", err)
	}

	invite, err := svc.CreateMember(ctx, memberUser, "Invited", invited.WalletAddress, grp.ID, nil)
	if err != nil {
		t.Fatalf("CreateMember(member invite) error = %v", err)
	}
	if invite.Status != group.MemberStatusPending {
		t.Fatalf("member invite status = %q, want %q", invite.Status, group.MemberStatusPending)
	}
	if err := svc.ApproveMember(ctx, owner, invite.ID); err != group.ErrMemberNotFound {
		t.Fatalf("ApproveMember(owner) error = %v, want %v", err, group.ErrMemberNotFound)
	}
	if err := svc.ApproveMember(ctx, invited, invite.ID); err != nil {
		t.Fatalf("ApproveMember(invited) error = %v", err)
	}
	if got := repo.members[invite.ID].Status; got != group.MemberStatusActive {
		t.Fatalf("status after invited approve = %q, want %q", got, group.MemberStatusActive)
	}
}

func TestGroupServiceRejectActiveMemberInviteRequiresTargetWallet(t *testing.T) {
	ctx := context.Background()
	repo := newFakeGroupRepository()
	svc := NewGroupService(repo)
	owner := &user.User{ID: "owner-user", WalletAddress: "0xowner"}
	memberUser := &user.User{ID: "member-user", WalletAddress: "0xmember"}
	invited := &user.User{ID: "invited-user", WalletAddress: "0xinvited"}

	grp, err := svc.CreateGroup(ctx, owner, "team")
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	member, err := svc.CreateMember(ctx, owner, "Member", memberUser.WalletAddress, grp.ID, nil)
	if err != nil {
		t.Fatalf("CreateMember(owner invite) error = %v", err)
	}
	if err := svc.ApproveMember(ctx, memberUser, member.ID); err != nil {
		t.Fatalf("ApproveMember(member) error = %v", err)
	}
	invite, err := svc.CreateMember(ctx, memberUser, "Invited", invited.WalletAddress, grp.ID, nil)
	if err != nil {
		t.Fatalf("CreateMember(member invite) error = %v", err)
	}

	if err := svc.RejectMember(ctx, owner, invite.ID); err != group.ErrMemberNotFound {
		t.Fatalf("RejectMember(owner) error = %v, want %v", err, group.ErrMemberNotFound)
	}
	if _, ok := repo.members[invite.ID]; !ok {
		t.Fatal("invite deleted by owner wallet")
	}
	if err := svc.RejectMember(ctx, invited, invite.ID); err != nil {
		t.Fatalf("RejectMember(invited) error = %v", err)
	}
	if _, ok := repo.members[invite.ID]; ok {
		t.Fatal("invite still exists after invited reject")
	}
}

func TestGroupServiceRejectMemberInviteRequiresTargetWallet(t *testing.T) {
	ctx := context.Background()
	repo := newFakeGroupRepository()
	svc := NewGroupService(repo)
	owner := &user.User{ID: "owner-user", WalletAddress: "0xowner"}
	invited := &user.User{ID: "invited-user", WalletAddress: "0xinvited"}
	other := &user.User{ID: "other-user", WalletAddress: "0xother"}

	grp, err := group.NewGroup(owner.ID, "team")
	if err != nil {
		t.Fatalf("NewGroup() error = %v", err)
	}
	repo.groups[grp.ID] = grp

	member, err := svc.CreateMember(ctx, owner, "Invited", invited.WalletAddress, grp.ID, nil)
	if err != nil {
		t.Fatalf("CreateMember() error = %v", err)
	}
	if err := svc.RejectMember(ctx, other, member.ID); err != group.ErrMemberNotFound {
		t.Fatalf("RejectMember() by unrelated wallet error = %v, want %v", err, group.ErrMemberNotFound)
	}
	if _, ok := repo.members[member.ID]; !ok {
		t.Fatal("member deleted by unrelated wallet")
	}

	if err := svc.RejectMember(ctx, invited, member.ID); err != nil {
		t.Fatalf("RejectMember() by invited wallet error = %v", err)
	}
	if _, ok := repo.members[member.ID]; ok {
		t.Fatal("member still exists after invited reject")
	}
}

type fakeGroupRepository struct {
	groups  map[string]*group.Group
	members map[string]*group.Member
}

func newFakeGroupRepository() *fakeGroupRepository {
	return &fakeGroupRepository{
		groups:  make(map[string]*group.Group),
		members: make(map[string]*group.Member),
	}
}

func (r *fakeGroupRepository) CreateGroup(_ context.Context, grp *group.Group, ownerMember *group.Member) error {
	r.groups[grp.ID] = cloneGroup(grp)
	if ownerMember != nil {
		r.members[ownerMember.ID] = cloneMember(ownerMember)
	}
	return nil
}

func (r *fakeGroupRepository) GetGroupByID(_ context.Context, userID, groupID string) (*group.Group, error) {
	grp, ok := r.groups[groupID]
	if !ok || grp.UserID != userID {
		return nil, group.ErrGroupNotFound
	}
	return cloneGroup(grp), nil
}

func (r *fakeGroupRepository) GetVisibleGroupByID(_ context.Context, userID, walletAddress, groupID string) (*group.Group, error) {
	grp, ok := r.groups[groupID]
	if !ok {
		return nil, group.ErrGroupNotFound
	}
	if grp.UserID == userID {
		copied := cloneGroup(grp)
		copied.CanInvite = true
		return copied, nil
	}
	for _, member := range r.members {
		if member.GroupID == groupID && strings.EqualFold(member.WalletAddress, walletAddress) {
			copied := cloneGroup(grp)
			copied.CanInvite = r.isActiveGroupMember(groupID, walletAddress)
			return copied, nil
		}
	}
	return nil, group.ErrGroupNotFound
}

func (r *fakeGroupRepository) ListVisibleGroups(_ context.Context, userID, walletAddress string) ([]*group.Group, error) {
	groups := make([]*group.Group, 0, len(r.groups))
	for _, grp := range r.groups {
		if grp.UserID == userID {
			copied := cloneGroup(grp)
			copied.CanInvite = true
			groups = append(groups, copied)
			continue
		}
		for _, member := range r.members {
			if member.GroupID == grp.ID && strings.EqualFold(member.WalletAddress, walletAddress) {
				copied := cloneGroup(grp)
				copied.CanInvite = r.isActiveGroupMember(grp.ID, walletAddress)
				groups = append(groups, copied)
				break
			}
		}
	}
	return groups, nil
}

func (r *fakeGroupRepository) UpdateGroupName(_ context.Context, userID, groupID, name string) error {
	grp, ok := r.groups[groupID]
	if !ok || grp.UserID != userID {
		return group.ErrGroupNotFound
	}
	grp.Name = name
	return nil
}

func (r *fakeGroupRepository) DeleteGroup(_ context.Context, userID, groupID string) error {
	grp, ok := r.groups[groupID]
	if !ok || grp.UserID != userID {
		return group.ErrGroupNotFound
	}
	delete(r.groups, groupID)
	return nil
}

func (r *fakeGroupRepository) CreateMember(_ context.Context, member *group.Member) error {
	r.members[member.ID] = cloneMember(member)
	return nil
}

func (r *fakeGroupRepository) GetMemberByID(_ context.Context, userID, memberID string) (*group.Member, error) {
	member, ok := r.members[memberID]
	if !ok || member.UserID != userID {
		return nil, group.ErrMemberNotFound
	}
	return cloneMember(member), nil
}

func (r *fakeGroupRepository) ListVisibleMembers(_ context.Context, userID, walletAddress string) ([]*group.Member, error) {
	members := make([]*group.Member, 0, len(r.members))
	for _, member := range r.members {
		if member.UserID == userID ||
			strings.EqualFold(member.WalletAddress, walletAddress) ||
			(member.Status == group.MemberStatusActive && r.isActiveGroupMember(member.GroupID, walletAddress)) {
			members = append(members, cloneMember(member))
		}
	}
	return members, nil
}

func (r *fakeGroupRepository) isActiveGroupMember(groupID, walletAddress string) bool {
	for _, member := range r.members {
		if member.GroupID == groupID &&
			member.Status == group.MemberStatusActive &&
			strings.EqualFold(member.WalletAddress, walletAddress) {
			return true
		}
	}
	return false
}

func (r *fakeGroupRepository) UpdateMember(_ context.Context, member *group.Member) error {
	current, ok := r.members[member.ID]
	if !ok || current.UserID != member.UserID {
		return group.ErrMemberNotFound
	}
	r.members[member.ID] = cloneMember(member)
	return nil
}

func (r *fakeGroupRepository) UpdateMemberStatusByWallet(_ context.Context, walletAddress, memberID, status string) error {
	member, ok := r.members[memberID]
	if !ok || member.Status != group.MemberStatusPending || !strings.EqualFold(member.WalletAddress, walletAddress) {
		return group.ErrMemberNotFound
	}
	member.Status = group.NormalizeMemberStatus(status)
	return nil
}

func (r *fakeGroupRepository) DeleteMember(_ context.Context, userID, memberID string) error {
	member, ok := r.members[memberID]
	if !ok || member.UserID != userID {
		return group.ErrMemberNotFound
	}
	delete(r.members, memberID)
	return nil
}

func (r *fakeGroupRepository) DeleteMemberByWallet(_ context.Context, walletAddress, memberID string) error {
	member, ok := r.members[memberID]
	if !ok || member.Status != group.MemberStatusPending || !strings.EqualFold(member.WalletAddress, walletAddress) {
		return group.ErrMemberNotFound
	}
	delete(r.members, memberID)
	return nil
}

func cloneGroup(grp *group.Group) *group.Group {
	if grp == nil {
		return nil
	}
	copied := *grp
	return &copied
}

func cloneMember(member *group.Member) *group.Member {
	if member == nil {
		return nil
	}
	copied := *member
	if member.Tags != nil {
		copied.Tags = make([]string, len(member.Tags))
		copy(copied.Tags, member.Tags)
	}
	return &copied
}
