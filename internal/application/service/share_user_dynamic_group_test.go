package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/yeying-community/warehouse/internal/domain/shareuser"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
	"go.uber.org/zap"
)

func TestCreateByGroupsStoresDynamicGroupAudience(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	cfg := &config.Config{WebDAV: config.WebDAVConfig{Directory: root, Prefix: "/dav"}}

	owner := newShareTestUser(t, "owner", "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	target := newShareTestUser(t, "target", "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	userRepo := newTestUserRepo()
	mustSaveUser(t, userRepo, owner)
	mustSaveUser(t, userRepo, target)

	sharedPath := filepath.Join(root, owner.Directory, "personal", "shared")
	if err := os.MkdirAll(sharedPath, 0o755); err != nil {
		t.Fatalf("mkdir shared path: %v", err)
	}

	groupRepo := newFakeGroupRepository()
	groupSvc := NewGroupService(groupRepo)
	grp, err := groupSvc.CreateGroup(ctx, owner, "team")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	member, err := groupSvc.CreateMember(ctx, owner, target.Username, target.WalletAddress, grp.ID, nil)
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	if err := groupSvc.ApproveMember(ctx, target, member.ID); err != nil {
		t.Fatalf("ApproveMember: %v", err)
	}

	shareRepo := newMemoryShareRepo()
	svc := NewShareUserService(shareRepo, userRepo, groupSvc, nil, cfg, zap.NewNop())

	item, err := svc.CreateByGroups(ctx, owner, []string{grp.ID}, "/personal/shared", "CRUD", ShareExpiryInput{})
	if err != nil {
		t.Fatalf("CreateByGroups: %v", err)
	}

	audiences := shareRepo.audiences[item.ID]
	if len(audiences) != 1 {
		t.Fatalf("stored audiences = %d, want 1: %#v", len(audiences), audiences)
	}
	if got := audiences[0].AudienceType; got != shareuser.AudienceTypeGroup {
		t.Fatalf("audience type = %q, want %q", got, shareuser.AudienceTypeGroup)
	}
	if audiences[0].SourceGroupID != grp.ID {
		t.Fatalf("source group = %q, want %q", audiences[0].SourceGroupID, grp.ID)
	}
	if audiences[0].TargetUserID != "" || audiences[0].TargetWallet != "" {
		t.Fatalf("group audience should not snapshot a target user: %#v", audiences[0])
	}
	if item.AudienceType != "groups" || item.TargetCount != 1 || item.AudienceCount != 1 {
		t.Fatalf("unexpected item metadata: type=%q target=%d audience=%d", item.AudienceType, item.TargetCount, item.AudienceCount)
	}
}

func TestResolveForTargetUsesCurrentGroupMembershipForHistoricalGroupShare(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	cfg := &config.Config{WebDAV: config.WebDAVConfig{Directory: root, Prefix: "/dav"}}

	owner := newShareTestUser(t, "owner", "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	target := newShareTestUser(t, "target", "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	userRepo := newTestUserRepo()
	mustSaveUser(t, userRepo, owner)
	mustSaveUser(t, userRepo, target)

	groupRepo := newFakeGroupRepository()
	groupSvc := NewGroupService(groupRepo)
	grp, err := groupSvc.CreateGroup(ctx, owner, "team")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	member, err := groupSvc.CreateMember(ctx, owner, target.Username, target.WalletAddress, grp.ID, nil)
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	if err := groupSvc.ApproveMember(ctx, target, member.ID); err != nil {
		t.Fatalf("ApproveMember: %v", err)
	}

	shareRepo := newMemoryShareRepo()
	item := shareuser.NewInternalShareItem(owner.ID, owner.Username, "/personal/shared", "shared", true, "CRUD", nil)
	shareRepo.items[item.ID] = cloneShareUserItem(item)
	shareRepo.audiences[item.ID] = []repository.UserShareAudience{
		{
			AudienceType:  shareuser.AudienceTypeUser,
			TargetUserID:  owner.ID,
			TargetWallet:  owner.WalletAddress,
			SourceGroupID: grp.ID,
		},
	}
	svc := NewShareUserService(shareRepo, userRepo, groupSvc, nil, cfg, zap.NewNop())

	if _, _, err := svc.ResolveForTarget(ctx, target, item.ID, "read"); err != nil {
		t.Fatalf("ResolveForTarget active group member: %v", err)
	}

	groupRepo.members[member.ID].Status = "pending"
	if _, _, err := svc.ResolveForTarget(ctx, target, item.ID, "read"); err == nil {
		t.Fatal("ResolveForTarget should deny pending group member")
	}
}

func newShareTestUser(t *testing.T, username, wallet string) *user.User {
	t.Helper()
	u := user.NewUser(username, username)
	if err := u.SetWalletAddress(wallet); err != nil {
		t.Fatalf("SetWalletAddress: %v", err)
	}
	u.Permissions = user.FullPermissions()
	return u
}

func mustSaveUser(t *testing.T, repo *testUserRepo, u *user.User) {
	t.Helper()
	if err := repo.Save(context.Background(), u); err != nil {
		t.Fatalf("save user %s: %v", u.Username, err)
	}
}

type memoryShareRepo struct {
	items     map[string]*shareuser.ShareUserItem
	audiences map[string][]repository.UserShareAudience
}

func newMemoryShareRepo() *memoryShareRepo {
	return &memoryShareRepo{
		items:     make(map[string]*shareuser.ShareUserItem),
		audiences: make(map[string][]repository.UserShareAudience),
	}
}

func (r *memoryShareRepo) CreateWithAudiences(_ context.Context, item *shareuser.ShareUserItem, audiences []repository.UserShareAudience) error {
	r.items[item.ID] = cloneShareUserItem(item)
	r.audiences[item.ID] = append([]repository.UserShareAudience(nil), audiences...)
	return nil
}

func (r *memoryShareRepo) GetByID(_ context.Context, id string) (*shareuser.ShareUserItem, error) {
	item, ok := r.items[id]
	if !ok {
		return nil, shareuser.ErrShareNotFound
	}
	return cloneShareUserItem(item), nil
}

func (r *memoryShareRepo) GetByOwnerID(_ context.Context, ownerID string) ([]*shareuser.ShareUserItem, error) {
	items := make([]*shareuser.ShareUserItem, 0)
	for _, item := range r.items {
		if item.OwnerUserID == ownerID {
			items = append(items, cloneShareUserItem(item))
		}
	}
	return items, nil
}

func (*memoryShareRepo) GetByTargetID(context.Context, string) ([]*shareuser.ShareUserItem, error) {
	return nil, nil
}

func (*memoryShareRepo) UpdatePathsForOwnerMove(context.Context, string, string, string) error {
	return nil
}

func (r *memoryShareRepo) DeleteByID(_ context.Context, id string) error {
	if _, ok := r.items[id]; !ok {
		return shareuser.ErrShareNotFound
	}
	delete(r.items, id)
	delete(r.audiences, id)
	return nil
}

func (r *memoryShareRepo) ListAudiencesByShareID(_ context.Context, shareID string) ([]repository.UserShareAudience, error) {
	audiences, ok := r.audiences[shareID]
	if !ok {
		return nil, fmt.Errorf("share audiences not found")
	}
	return append([]repository.UserShareAudience(nil), audiences...), nil
}

func cloneShareUserItem(item *shareuser.ShareUserItem) *shareuser.ShareUserItem {
	if item == nil {
		return nil
	}
	copied := *item
	return &copied
}

var _ repository.UserShareRepository = (*memoryShareRepo)(nil)
