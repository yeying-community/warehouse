package service

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yeying-community/warehouse/internal/domain/permission"
	"github.com/yeying-community/warehouse/internal/domain/quota"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/atomicfile"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	webdavfs "github.com/yeying-community/warehouse/internal/infrastructure/webdav"
	"go.uber.org/zap"
)

var (
	ErrUploadSessionNotFound  = errors.New("upload session not found")
	ErrUploadSessionForbidden = errors.New("upload session forbidden")
	ErrUploadSessionInvalid   = errors.New("invalid upload session")
	ErrUploadSessionTooLarge  = errors.New("upload exceeds size limit")
	ErrUploadSessionChecksum  = errors.New("upload session checksum mismatch")
)

const (
	UploadSessionStatusActive    = "active"
	UploadSessionStatusCompleted = "completed"
	UploadSessionStatusAborted   = "aborted"

	UploadSessionScopeWebDAV = "webdav"
	UploadSessionScopeShare  = "share"

	DefaultUploadChunkSize int64 = 8 * 1024 * 1024
	MaxUploadChunkSize     int64 = 64 * 1024 * 1024
	MaxUploadObjectSize    int64 = 10 * 1024 * 1024 * 1024
	UploadSessionTTL             = 24 * time.Hour
)

type UploadSessionCreateInput struct {
	Path         string
	ShareID      string
	Size         int64
	ChunkSize    int64
	FileName     string
	ContentType  string
	LastModified int64
}

type UploadSessionPart struct {
	PartNumber     int       `json:"partNumber"`
	Size           int64     `json:"size"`
	ETag           string    `json:"etag"`
	ChecksumSHA256 string    `json:"checksumSha256"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type UploadSession struct {
	ID             string                    `json:"id"`
	UploaderUserID string                    `json:"uploaderUserId"`
	OwnerUserID    string                    `json:"ownerUserId"`
	OwnerUsername  string                    `json:"ownerUsername"`
	Scope          string                    `json:"scope"`
	TargetPath     string                    `json:"targetPath"`
	TargetFullPath string                    `json:"targetFullPath"`
	ShareID        string                    `json:"shareId,omitempty"`
	Size           int64                     `json:"size"`
	ChunkSize      int64                     `json:"chunkSize"`
	FileName       string                    `json:"fileName"`
	ContentType    string                    `json:"contentType,omitempty"`
	LastModified   int64                     `json:"lastModified,omitempty"`
	Status         string                    `json:"status"`
	CreatedAt      time.Time                 `json:"createdAt"`
	UpdatedAt      time.Time                 `json:"updatedAt"`
	ExpiresAt      time.Time                 `json:"expiresAt"`
	Parts          map[int]UploadSessionPart `json:"parts"`
}

type UploadSessionTarget struct {
	Owner      *user.User
	FullPath   string
	TargetPath string
	Operation  permission.Operation
}

type UploadSessionService struct {
	config           *config.Config
	permissionCheck  permission.Checker
	quotaService     quota.Service
	userRepo         user.Repository
	shareUserService *ShareUserService
	mutationRecorder MutationRecorder
	logger           *zap.Logger
	locks            sync.Map
}

func NewUploadSessionService(
	cfg *config.Config,
	permissionCheck permission.Checker,
	quotaService quota.Service,
	userRepo user.Repository,
	shareUserService *ShareUserService,
	mutationRecorder MutationRecorder,
	logger *zap.Logger,
) *UploadSessionService {
	if mutationRecorder == nil {
		mutationRecorder = noopMutationRecorder{}
	}
	return &UploadSessionService{
		config:           cfg,
		permissionCheck:  permissionCheck,
		quotaService:     quotaService,
		userRepo:         userRepo,
		shareUserService: shareUserService,
		mutationRecorder: mutationRecorder,
		logger:           logger,
	}
}

func (s *UploadSessionService) Create(ctx context.Context, uploader *user.User, input UploadSessionCreateInput) (*UploadSession, error) {
	if uploader == nil {
		return nil, ErrUploadSessionForbidden
	}
	if input.Size < 0 || input.Size > MaxUploadObjectSize {
		return nil, ErrUploadSessionTooLarge
	}
	chunkSize := input.ChunkSize
	if chunkSize <= 0 {
		chunkSize = DefaultUploadChunkSize
	}
	if chunkSize > MaxUploadChunkSize {
		return nil, fmt.Errorf("%w: chunk size exceeds limit", ErrUploadSessionTooLarge)
	}
	target, scope, err := s.resolveTarget(ctx, uploader, input)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(target.FullPath), 0o755); err != nil {
		return nil, err
	}
	if s.quotaService != nil && target.Owner != nil {
		oldSize, err := getExistingFileSize(target.FullPath)
		if err != nil {
			return nil, err
		}
		delta := input.Size - oldSize
		if delta > 0 {
			if err := s.quotaService.CheckQuota(ctx, target.Owner, delta); err != nil {
				return nil, err
			}
		}
	}
	now := time.Now()
	session := &UploadSession{
		ID:             uuid.NewString(),
		UploaderUserID: uploader.ID,
		OwnerUserID:    target.Owner.ID,
		OwnerUsername:  target.Owner.Username,
		Scope:          scope,
		TargetPath:     target.TargetPath,
		TargetFullPath: target.FullPath,
		ShareID:        strings.TrimSpace(input.ShareID),
		Size:           input.Size,
		ChunkSize:      chunkSize,
		FileName:       strings.TrimSpace(input.FileName),
		ContentType:    strings.TrimSpace(input.ContentType),
		LastModified:   input.LastModified,
		Status:         UploadSessionStatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
		ExpiresAt:      now.Add(UploadSessionTTL),
		Parts:          map[int]UploadSessionPart{},
	}
	if session.FileName == "" {
		session.FileName = path.Base(session.TargetPath)
	}
	if err := s.saveSession(session); err != nil {
		_ = os.RemoveAll(s.sessionDir(session.ID))
		return nil, err
	}
	return session, nil
}

func (s *UploadSessionService) Get(ctx context.Context, uploader *user.User, id string) (*UploadSession, error) {
	session, err := s.loadActiveSession(id)
	if err != nil {
		return nil, err
	}
	if _, err := s.authorizeSession(ctx, uploader, session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *UploadSessionService) UploadPart(ctx context.Context, uploader *user.User, id string, partNumber int, expectedChecksumSHA256 string, src io.Reader) (*UploadSession, UploadSessionPart, error) {
	if partNumber < 1 {
		return nil, UploadSessionPart{}, ErrUploadSessionInvalid
	}
	expectedChecksumSHA256, err := normalizeUploadChecksumSHA256(expectedChecksumSHA256)
	if err != nil {
		return nil, UploadSessionPart{}, err
	}
	unlock := s.lockSession(id)
	defer unlock()
	session, err := s.loadActiveSession(id)
	if err != nil {
		return nil, UploadSessionPart{}, err
	}
	if _, err := s.authorizeSession(ctx, uploader, session); err != nil {
		return nil, UploadSessionPart{}, err
	}
	maxPartNumber := expectedPartCount(session.Size, session.ChunkSize)
	if partNumber > maxPartNumber {
		return nil, UploadSessionPart{}, ErrUploadSessionInvalid
	}
	partPath := s.partPath(session.ID, partNumber)
	tmpPath := partPath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(partPath), 0o700); err != nil {
		return nil, UploadSessionPart{}, err
	}
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, UploadSessionPart{}, err
	}
	md5Hash := md5.New()
	sha256Hash := sha256.New()
	limit := session.ChunkSize
	if limit <= 0 || limit > MaxUploadChunkSize {
		limit = MaxUploadChunkSize
	}
	size, copyErr := io.Copy(io.MultiWriter(file, md5Hash, sha256Hash), io.LimitReader(src, limit+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return nil, UploadSessionPart{}, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return nil, UploadSessionPart{}, closeErr
	}
	if size > limit {
		_ = os.Remove(tmpPath)
		return nil, UploadSessionPart{}, ErrUploadSessionTooLarge
	}
	checksumSHA256 := hex.EncodeToString(sha256Hash.Sum(nil))
	if expectedChecksumSHA256 != "" && expectedChecksumSHA256 != checksumSHA256 {
		_ = os.Remove(tmpPath)
		return nil, UploadSessionPart{}, ErrUploadSessionChecksum
	}
	if err := os.Rename(tmpPath, partPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, UploadSessionPart{}, err
	}
	now := time.Now()
	part := UploadSessionPart{
		PartNumber:     partNumber,
		Size:           size,
		ETag:           hex.EncodeToString(md5Hash.Sum(nil)),
		ChecksumSHA256: checksumSHA256,
		UpdatedAt:      now,
	}
	if session.Parts == nil {
		session.Parts = map[int]UploadSessionPart{}
	}
	session.Parts[partNumber] = part
	session.UpdatedAt = now
	if err := s.saveSession(session); err != nil {
		return nil, UploadSessionPart{}, err
	}
	return session, part, nil
}

func (s *UploadSessionService) Complete(ctx context.Context, uploader *user.User, id string) (*UploadSession, error) {
	unlock := s.lockSession(id)
	defer unlock()
	session, err := s.loadActiveSession(id)
	if err != nil {
		return nil, err
	}
	target, err := s.authorizeSession(ctx, uploader, session)
	if err != nil {
		return nil, err
	}
	if err := s.validateCompleteParts(session); err != nil {
		return nil, err
	}
	oldSize, err := getExistingFileSize(target.FullPath)
	if err != nil {
		return nil, err
	}
	delta := session.Size - oldSize
	reserved := false
	var reservedUsed int64
	if reserveRepo, ok := s.userRepo.(quotaReserveRepository); ok && delta != 0 {
		reservedUsed, err = reserveRepo.ReserveUsedSpaceDelta(ctx, target.Owner.Username, delta)
		if err != nil {
			return nil, err
		}
		reserved = true
	} else if s.quotaService != nil && delta > 0 {
		if err := s.quotaService.CheckQuota(ctx, target.Owner, delta); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(target.FullPath), 0o755); err != nil {
		if reserved {
			_ = s.userRepo.(quotaReserveRepository).ReleaseUsedSpaceDelta(ctx, target.Owner.Username, delta)
		}
		return nil, err
	}
	out, err := atomicfile.Open(target.FullPath, 0o644)
	if err != nil {
		if reserved {
			_ = s.userRepo.(quotaReserveRepository).ReleaseUsedSpaceDelta(ctx, target.Owner.Username, delta)
		}
		return nil, err
	}
	for partNumber := 1; partNumber <= expectedPartCount(session.Size, session.ChunkSize); partNumber++ {
		if err := copyPartFile(out, s.partPath(session.ID, partNumber)); err != nil {
			out.Abort()
			if reserved {
				_ = s.userRepo.(quotaReserveRepository).ReleaseUsedSpaceDelta(ctx, target.Owner.Username, delta)
			}
			return nil, err
		}
	}
	if err := out.Close(); err != nil {
		if reserved {
			_ = s.userRepo.(quotaReserveRepository).ReleaseUsedSpaceDelta(ctx, target.Owner.Username, delta)
		}
		return nil, err
	}
	if reserved {
		_ = target.Owner.UpdateUsedSpace(reservedUsed)
	} else if s.userRepo != nil && delta != 0 {
		used, err := s.userRepo.UpdateUsedSpaceDelta(ctx, target.Owner.Username, delta)
		if err != nil {
			return nil, err
		}
		_ = target.Owner.UpdateUsedSpace(used)
	}
	if err := s.mutationRecorder.EnsureDir(ctx, filepath.Dir(target.FullPath)); err != nil {
		return nil, err
	}
	if err := s.mutationRecorder.UpsertFile(ctx, target.FullPath); err != nil {
		return nil, err
	}
	session.Status = UploadSessionStatusCompleted
	session.TargetFullPath = target.FullPath
	session.TargetPath = target.TargetPath
	session.UpdatedAt = time.Now()
	if err := s.saveSession(session); err != nil {
		return nil, err
	}
	if err := os.RemoveAll(s.sessionDir(session.ID)); err != nil && s.logger != nil {
		s.logger.Warn("failed to remove completed upload session", zap.String("id", session.ID), zap.Error(err))
	}
	return session, nil
}

func (s *UploadSessionService) Abort(ctx context.Context, uploader *user.User, id string) error {
	unlock := s.lockSession(id)
	defer unlock()
	session, err := s.loadSession(id)
	if err != nil {
		return err
	}
	if _, err := s.authorizeSession(ctx, uploader, session); err != nil {
		return err
	}
	session.Status = UploadSessionStatusAborted
	session.UpdatedAt = time.Now()
	_ = s.saveSession(session)
	return os.RemoveAll(s.sessionDir(id))
}

func (s *UploadSessionService) CleanupExpired(ctx context.Context, now time.Time) (int, error) {
	entries, err := os.ReadDir(s.sessionRoot())
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	cleaned := 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return cleaned, err
		}
		if !entry.IsDir() {
			continue
		}
		session, err := s.loadSession(entry.Name())
		if errors.Is(err, ErrUploadSessionNotFound) {
			continue
		}
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("failed to load upload session during cleanup", zap.String("id", entry.Name()), zap.Error(err))
			}
			continue
		}
		if session.Status == UploadSessionStatusActive && now.Before(session.ExpiresAt) {
			continue
		}
		if err := os.RemoveAll(s.sessionDir(entry.Name())); err != nil {
			return cleaned, err
		}
		cleaned++
	}
	return cleaned, nil
}

func (s *UploadSessionService) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		_, _ = s.CleanupExpired(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *UploadSessionService) authorizeSession(ctx context.Context, uploader *user.User, session *UploadSession) (*UploadSessionTarget, error) {
	if uploader == nil || session == nil || uploader.ID != session.UploaderUserID {
		return nil, ErrUploadSessionForbidden
	}
	input := UploadSessionCreateInput{
		Path:      session.TargetPath,
		ShareID:   session.ShareID,
		Size:      session.Size,
		ChunkSize: session.ChunkSize,
		FileName:  session.FileName,
	}
	target, _, err := s.resolveTarget(ctx, uploader, input)
	if err != nil {
		return nil, err
	}
	session.TargetFullPath = target.FullPath
	session.TargetPath = target.TargetPath
	return target, nil
}

func (s *UploadSessionService) resolveTarget(ctx context.Context, uploader *user.User, input UploadSessionCreateInput) (*UploadSessionTarget, string, error) {
	if strings.TrimSpace(input.ShareID) != "" {
		target, err := s.resolveShareTarget(ctx, uploader, input)
		return target, UploadSessionScopeShare, err
	}
	target, err := s.resolveWebDAVTarget(ctx, uploader, input.Path)
	return target, UploadSessionScopeWebDAV, err
}

func (s *UploadSessionService) resolveWebDAVTarget(ctx context.Context, uploader *user.User, rawPath string) (*UploadSessionTarget, error) {
	targetPath, err := normalizeUploadTargetPath(rawPath)
	if err != nil {
		return nil, err
	}
	if isIgnoredUploadPath(targetPath) {
		return nil, ErrUploadSessionInvalid
	}
	userRoot := s.userRootDir(uploader)
	rel := strings.TrimPrefix(targetPath, "/")
	fullPath := filepath.Clean(filepath.Join(userRoot, filepath.FromSlash(rel)))
	if !isPathWithin(userRoot, fullPath) {
		return nil, ErrUploadSessionInvalid
	}
	op := permission.OperationCreate
	if _, err := os.Stat(fullPath); err == nil {
		op = permission.OperationWrite
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := enforceAppScope(ctx, s.config, targetPath, requiredActionForUploadOperation(op)); err != nil {
		return nil, err
	}
	if s.permissionCheck != nil {
		permissionPath := filepath.Join(s.userPermissionRoot(uploader), filepath.FromSlash(rel))
		if err := s.permissionCheck.Check(ctx, uploader, permissionPath, op); err != nil {
			return nil, ErrUploadSessionForbidden
		}
	}
	return &UploadSessionTarget{Owner: uploader, FullPath: fullPath, TargetPath: targetPath, Operation: op}, nil
}

func (s *UploadSessionService) resolveShareTarget(ctx context.Context, uploader *user.User, input UploadSessionCreateInput) (*UploadSessionTarget, error) {
	if s.shareUserService == nil {
		return nil, ErrUploadSessionInvalid
	}
	item, owner, err := s.shareUserService.ResolveForTarget(ctx, uploader, strings.TrimSpace(input.ShareID), "create", "update")
	if err != nil {
		return nil, err
	}
	_, fullPath, err := s.shareUserService.ResolveSharePath(owner, item, input.Path)
	if err != nil {
		return nil, err
	}
	targetPath, err := normalizeUploadTargetPath(input.Path)
	if err != nil {
		return nil, err
	}
	if isIgnoredUploadPath(targetPath) {
		return nil, ErrUploadSessionInvalid
	}
	perms := user.ParsePermissions(item.Permissions)
	if strings.TrimSpace(item.Permissions) == "" {
		perms = user.DefaultPermissions()
	}
	op := permission.OperationCreate
	if _, err := os.Stat(fullPath); err == nil {
		op = permission.OperationWrite
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := enforceAppScope(ctx, s.config, item.Path, requiredActionForUploadOperation(op)); err != nil {
		return nil, err
	}
	if op == permission.OperationWrite {
		if !perms.Has("U") {
			return nil, ErrUploadSessionForbidden
		}
	} else if !perms.Has("C") {
		return nil, ErrUploadSessionForbidden
	}
	return &UploadSessionTarget{Owner: owner, FullPath: fullPath, TargetPath: targetPath, Operation: op}, nil
}

func (s *UploadSessionService) validateCompleteParts(session *UploadSession) error {
	if session == nil || session.Status != UploadSessionStatusActive {
		return ErrUploadSessionNotFound
	}
	expected := expectedPartCount(session.Size, session.ChunkSize)
	if expected == 0 {
		return nil
	}
	var total int64
	for partNumber := 1; partNumber <= expected; partNumber++ {
		part, ok := session.Parts[partNumber]
		if !ok {
			return fmt.Errorf("%w: missing part %d", ErrUploadSessionInvalid, partNumber)
		}
		total += part.Size
		if partNumber < expected && part.Size != session.ChunkSize {
			return fmt.Errorf("%w: invalid part size", ErrUploadSessionInvalid)
		}
		if partNumber == expected {
			expectedLast := session.Size - session.ChunkSize*int64(expected-1)
			if part.Size != expectedLast {
				return fmt.Errorf("%w: invalid final part size", ErrUploadSessionInvalid)
			}
		}
	}
	if total != session.Size {
		return fmt.Errorf("%w: size mismatch", ErrUploadSessionInvalid)
	}
	return nil
}

func (s *UploadSessionService) loadActiveSession(id string) (*UploadSession, error) {
	session, err := s.loadSession(id)
	if err != nil {
		return nil, err
	}
	if session.Status != UploadSessionStatusActive || time.Now().After(session.ExpiresAt) {
		_ = os.RemoveAll(s.sessionDir(id))
		return nil, ErrUploadSessionNotFound
	}
	return session, nil
}

func (s *UploadSessionService) loadSession(id string) (*UploadSession, error) {
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "/") || strings.Contains(id, string(filepath.Separator)) {
		return nil, ErrUploadSessionNotFound
	}
	data, err := os.ReadFile(s.sessionFile(id))
	if os.IsNotExist(err) {
		return nil, ErrUploadSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	var session UploadSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	if session.Parts == nil {
		session.Parts = map[int]UploadSessionPart{}
	}
	return &session, nil
}

func (s *UploadSessionService) saveSession(session *UploadSession) error {
	if session == nil {
		return ErrUploadSessionInvalid
	}
	dir := s.sessionDir(session.ID)
	if err := os.MkdirAll(filepath.Join(dir, "parts"), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "session.json.tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.sessionFile(session.ID))
}

func (s *UploadSessionService) lockSession(id string) func() {
	value, _ := s.locks.LoadOrStore(id, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

func (s *UploadSessionService) sessionRoot() string {
	root := "/data"
	if s != nil && s.config != nil && strings.TrimSpace(s.config.WebDAV.Directory) != "" {
		root = s.config.WebDAV.Directory
	}
	return filepath.Join(root, ".warehouse-uploads")
}

func (s *UploadSessionService) sessionDir(id string) string {
	return filepath.Join(s.sessionRoot(), strings.TrimSpace(id))
}

func (s *UploadSessionService) sessionFile(id string) string {
	return filepath.Join(s.sessionDir(id), "session.json")
}

func (s *UploadSessionService) partPath(id string, partNumber int) string {
	return filepath.Join(s.sessionDir(id), "parts", fmt.Sprintf("part-%05d", partNumber))
}

func (s *UploadSessionService) userRootDir(u *user.User) string {
	userDir := s.userPermissionRoot(u)
	if filepath.IsAbs(userDir) {
		return filepath.Clean(userDir)
	}
	root := "/data"
	if s != nil && s.config != nil && strings.TrimSpace(s.config.WebDAV.Directory) != "" {
		root = s.config.WebDAV.Directory
	}
	return filepath.Clean(filepath.Join(root, userDir))
}

func (s *UploadSessionService) userPermissionRoot(u *user.User) string {
	if u == nil {
		return ""
	}
	if strings.TrimSpace(u.Directory) != "" {
		return u.Directory
	}
	return u.Username
}

func normalizeUploadTargetPath(raw string) (string, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if raw == "" {
		return "", fmt.Errorf("%w: path is required", ErrUploadSessionInvalid)
	}
	clean := path.Clean("/" + strings.TrimLeft(raw, "/"))
	if clean == "." || clean == "/" || strings.HasPrefix(clean, "/..") {
		return "", ErrUploadSessionInvalid
	}
	return clean, nil
}

func isIgnoredUploadPath(rawPath string) bool {
	base := path.Base(strings.TrimSuffix(filepath.ToSlash(rawPath), "/"))
	return webdavfs.IsIgnoredName(base)
}

func requiredActionForUploadOperation(op permission.Operation) string {
	if op == permission.OperationWrite {
		return "update"
	}
	return "create"
}

func expectedPartCount(size, chunkSize int64) int {
	if size <= 0 {
		return 0
	}
	if chunkSize <= 0 {
		chunkSize = DefaultUploadChunkSize
	}
	return int((size + chunkSize - 1) / chunkSize)
}

func normalizeUploadChecksumSHA256(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", nil
	}
	if len(value) != sha256.Size*2 {
		return "", fmt.Errorf("%w: invalid checksum", ErrUploadSessionInvalid)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("%w: invalid checksum", ErrUploadSessionInvalid)
	}
	return value, nil
}

func copyPartFile(dst io.Writer, partPath string) error {
	file, err := os.Open(partPath)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(dst, file)
	return err
}

func UploadSessionParts(session *UploadSession) []UploadSessionPart {
	if session == nil || len(session.Parts) == 0 {
		return []UploadSessionPart{}
	}
	parts := make([]UploadSessionPart, 0, len(session.Parts))
	for _, part := range session.Parts {
		parts = append(parts, part)
	}
	sort.Slice(parts, func(i, j int) bool {
		return parts[i].PartNumber < parts[j].PartNumber
	})
	return parts
}

func ClearUploadDeadlines(w http.ResponseWriter, logger *zap.Logger) {
	controller := http.NewResponseController(w)
	if err := controller.SetReadDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) && logger != nil {
		logger.Debug("failed to clear upload read deadline", zap.Error(err))
	}
	if err := controller.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) && logger != nil {
		logger.Debug("failed to clear upload write deadline", zap.Error(err))
	}
}
