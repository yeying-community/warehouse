package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	objectpath "github.com/yeying-community/warehouse/internal/domain/object"
	"github.com/yeying-community/warehouse/internal/infrastructure/atomicfile"
)

type ObjectInfo struct {
	Bucket      string
	Key         string
	Size        int64
	ETag        string
	ContentType string
	ModifiedAt  time.Time
	IsPrefix    bool
}

type ObjectList struct {
	Objects  []ObjectInfo
	Prefixes []string
}

// ObjectService contains filesystem operations shared by protocol adapters.
type ObjectService struct {
	webdavRoot string
}

func NewObjectService(webdavRoot string) *ObjectService {
	return &ObjectService{webdavRoot: filepath.Clean(webdavRoot)}
}

func (s *ObjectService) List(ctx context.Context, userDirectory, bucket, prefix string, delimiter rune) (ObjectList, error) {
	if err := ctx.Err(); err != nil {
		return ObjectList{}, err
	}
	base, err := objectpath.ResolvePath(s.webdavRoot, userDirectory, bucket, "")
	if err != nil {
		return ObjectList{}, err
	}
	if prefix != "" {
		base, err = objectpath.ResolvePath(s.webdavRoot, userDirectory, bucket, prefix)
		if err != nil {
			return ObjectList{}, err
		}
		if info, statErr := os.Stat(base); statErr == nil {
			if !info.IsDir() {
				item, err := s.statObject(bucket, prefix, base)
				if err != nil {
					return ObjectList{}, err
				}
				return ObjectList{Objects: []ObjectInfo{item}}, nil
			}
		} else if !os.IsNotExist(statErr) {
			return ObjectList{}, statErr
		} else {
			base = filepath.Dir(base)
		}
	}

	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return ObjectList{}, nil
		}
		return ObjectList{}, err
	}
	result := ObjectList{Objects: make([]ObjectInfo, 0), Prefixes: make([]string, 0)}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "._upload-") {
			continue
		}
		key := entry.Name()
		if prefix != "" {
			key = strings.TrimSuffix(prefix, "/") + "/" + entry.Name()
		}
		if delimiter == '/' && entry.IsDir() {
			result.Prefixes = append(result.Prefixes, strings.TrimSuffix(key, "/")+"/")
			continue
		}
		fullPath := filepath.Join(base, entry.Name())
		if entry.IsDir() {
			continue
		}
		item, err := s.statObject(bucket, key, fullPath)
		if err != nil {
			return ObjectList{}, err
		}
		result.Objects = append(result.Objects, item)
	}
	sort.Slice(result.Objects, func(i, j int) bool { return result.Objects[i].Key < result.Objects[j].Key })
	sort.Strings(result.Prefixes)
	return result, nil
}

func (s *ObjectService) EnsureBucket(ctx context.Context, userDirectory, bucket string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fullPath, err := objectpath.ResolvePath(s.webdavRoot, userDirectory, bucket, "")
	if err != nil {
		return err
	}
	return os.MkdirAll(fullPath, 0o755)
}

func (s *ObjectService) Stat(ctx context.Context, userDirectory, bucket, key string) (ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return ObjectInfo{}, err
	}
	fullPath, err := objectpath.ResolvePath(s.webdavRoot, userDirectory, bucket, key)
	if err != nil {
		return ObjectInfo{}, err
	}
	return s.statObject(bucket, key, fullPath)
}

func (s *ObjectService) Open(ctx context.Context, userDirectory, bucket, key string) (*os.File, ObjectInfo, error) {
	info, err := s.Stat(ctx, userDirectory, bucket, key)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	fullPath, err := objectpath.ResolvePath(s.webdavRoot, userDirectory, bucket, key)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	file, err := os.Open(fullPath)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	return file, info, nil
}

func (s *ObjectService) Put(ctx context.Context, userDirectory, bucket, key string, src io.Reader) (ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return ObjectInfo{}, err
	}
	fullPath, err := objectpath.ResolvePath(s.webdavRoot, userDirectory, bucket, key)
	if err != nil {
		return ObjectInfo{}, err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return ObjectInfo{}, err
	}
	if err := atomicfile.WriteAll(fullPath, src, 0o644); err != nil {
		return ObjectInfo{}, err
	}
	return s.statObject(bucket, key, fullPath)
}

func (s *ObjectService) Delete(ctx context.Context, userDirectory, bucket, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fullPath, err := objectpath.ResolvePath(s.webdavRoot, userDirectory, bucket, key)
	if err != nil {
		return err
	}
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *ObjectService) statObject(bucket, key, fullPath string) (ObjectInfo, error) {
	stat, err := os.Stat(fullPath)
	if err != nil {
		return ObjectInfo{}, err
	}
	if stat.IsDir() {
		return ObjectInfo{Bucket: bucket, Key: strings.TrimSuffix(key, "/") + "/", IsPrefix: true, ModifiedAt: stat.ModTime()}, nil
	}
	etag, err := fallbackETag(fullPath, stat)
	if err != nil {
		return ObjectInfo{}, err
	}
	contentType := mime.TypeByExtension(filepath.Ext(fullPath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return ObjectInfo{Bucket: bucket, Key: key, Size: stat.Size(), ETag: etag, ContentType: contentType, ModifiedAt: stat.ModTime()}, nil
}

func fallbackETag(path string, stat os.FileInfo) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
