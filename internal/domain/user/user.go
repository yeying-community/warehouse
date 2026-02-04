package user

import (
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrInvalidUsername   = errors.New("invalid username")
	ErrInvalidPassword   = errors.New("invalid password")
	ErrInvalidAddress    = errors.New("invalid wallet address")
	ErrDuplicateUsername = errors.New("username already exists")
	ErrDuplicateAddress  = errors.New("wallet address already exists")
	ErrQuotaExceeded     = errors.New("storage quota exceeded") // 新增
	ErrInvalidQuota      = errors.New("invalid quota value")    // 新增
)

// User 用户领域模型
type User struct {
	ID            string
	Username      string
	Password      string // 加密后的密码
	WalletAddress string // 以太坊钱包地址
	Directory     string
	Permissions   *Permissions
	Rules         []*Rule
	Quota         int64 // 存储配额（字节），0 表示无限制
	UsedSpace     int64 // 已使用空间（字节）
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Permissions 权限
type Permissions struct {
	Create bool
	Read   bool
	Update bool
	Delete bool
}

// Rule 权限规则
type Rule struct {
	Path        string
	Permissions *Permissions
	Regex       bool
	regexOnce   sync.Once
	regex       *regexp.Regexp
	regexErr    error
}

// NewUser 创建新用户
func NewUser(username, directory string) *User {
	now := time.Now()
	return &User{
		ID:          generateID(),
		Username:    username,
		Directory:   directory,
		Permissions: DefaultPermissions(),
		Rules:       make([]*Rule, 0),
		Quota:       1073741824, // 默认 1GB
		UsedSpace:   0,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// SetPassword 设置密码
func (u *User) SetPassword(hashedPassword string) {
	u.Password = hashedPassword
	u.UpdatedAt = time.Now()
}

// SetWalletAddress 设置钱包地址
func (u *User) SetWalletAddress(address string) error {
	if address == "" {
		return ErrInvalidAddress
	}
	u.WalletAddress = strings.ToLower(address)
	u.UpdatedAt = time.Now()
	return nil
}

// SetQuota 设置配额
func (u *User) SetQuota(quota int64) error {
	if quota < 0 {
		return ErrInvalidQuota
	}
	u.Quota = quota
	u.UpdatedAt = time.Now()
	return nil
}

// UpdateUsedSpace 更新已使用空间
func (u *User) UpdateUsedSpace(usedSpace int64) error {
	if usedSpace < 0 {
		usedSpace = 0
	}
	u.UsedSpace = usedSpace
	u.UpdatedAt = time.Now()
	return nil
}

// CanUpload 检查是否可以上传指定大小的文件
func (u *User) CanUpload(fileSize int64) error {
	// 如果配额为 0，表示无限制
	if u.Quota == 0 {
		return nil
	}

	// 检查是否超过配额
	if u.UsedSpace+fileSize > u.Quota {
		return ErrQuotaExceeded
	}

	return nil
}

// GetAvailableSpace 获取可用空间
func (u *User) GetAvailableSpace() int64 {
	// 如果配额为 0，返回 -1 表示无限制
	if u.Quota == 0 {
		return -1
	}

	available := u.Quota - u.UsedSpace
	if available < 0 {
		return 0
	}
	return available
}

// GetQuotaUsagePercent 获取配额使用百分比
func (u *User) GetQuotaUsagePercent() float64 {
	if u.Quota == 0 {
		return 0
	}
	return float64(u.UsedSpace) / float64(u.Quota) * 100
}

// HasQuota 是否设置了配额限制
func (u *User) HasQuota() bool {
	return u.Quota > 0
}

// HasPassword 是否设置了密码
func (u *User) HasPassword() bool {
	return u.Password != ""
}

// HasWalletAddress 是否设置了钱包地址
func (u *User) HasWalletAddress() bool {
	return u.WalletAddress != ""
}

// CanAccess 检查是否可以访问路径
func (u *User) CanAccess(path string, requiredPerm string) bool {
	// 先检查规则
	for _, rule := range u.Rules {
		if rule.Matches(path) {
			return rule.HasPermission(requiredPerm)
		}
	}

	// 使用默认权限
	return u.Permissions.Has(requiredPerm)
}

// DefaultPermissions 默认权限（只读）
func DefaultPermissions() *Permissions {
	return &Permissions{
		Create: false,
		Read:   true,
		Update: false,
		Delete: false,
	}
}

// FullPermissions 完整权限
func FullPermissions() *Permissions {
	return &Permissions{
		Create: true,
		Read:   true,
		Update: true,
		Delete: true,
	}
}

// ParsePermissions 解析权限字符串 (CRUD)
func ParsePermissions(s string) *Permissions {
	p := &Permissions{}
	s = strings.ToUpper(s)

	p.Create = strings.Contains(s, "C")
	p.Read = strings.Contains(s, "R")
	p.Update = strings.Contains(s, "U")
	p.Delete = strings.Contains(s, "D")

	return p
}

// String 权限字符串表示
func (p *Permissions) String() string {
	var s strings.Builder
	if p.Create {
		s.WriteString("C")
	}
	if p.Read {
		s.WriteString("R")
	}
	if p.Update {
		s.WriteString("U")
	}
	if p.Delete {
		s.WriteString("D")
	}
	return s.String()
}

// Has 是否有指定权限
func (p *Permissions) Has(perm string) bool {
	switch strings.ToUpper(perm) {
	case "C", "CREATE":
		return p.Create
	case "R", "READ":
		return p.Read
	case "U", "UPDATE":
		return p.Update
	case "D", "DELETE":
		return p.Delete
	default:
		return false
	}
}

// Matches 规则是否匹配路径
func (r *Rule) Matches(path string) bool {
	if r.Regex {
		re, err := r.compileRegex()
		if err != nil || re == nil {
			return false
		}
		return re.MatchString(path)
	}
	return strings.HasPrefix(path, r.Path)
}

func (r *Rule) compileRegex() (*regexp.Regexp, error) {
	r.regexOnce.Do(func() {
		r.regex, r.regexErr = regexp.Compile(r.Path)
	})
	return r.regex, r.regexErr
}

// HasPermission 规则是否有权限
func (r *Rule) HasPermission(perm string) bool {
	return r.Permissions.Has(perm)
}

// generateID 生成用户 ID
func generateID() string {
	return uuid.NewString()
}
