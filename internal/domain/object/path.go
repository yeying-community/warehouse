package object

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

var (
	ErrInvalidBucket = errors.New("invalid object bucket")
	ErrInvalidKey    = errors.New("invalid object key")
	ErrPathEscape    = errors.New("object path escapes user root")
)

var supportedBuckets = map[string]struct{}{
	"personal": {},
	"apps":     {},
	"services": {},
}

// ResolvePath maps an S3 bucket/key to a path below one user's asset root.
// The returned path is lexical-safe; callers must still reject symlinks when
// opening paths if the underlying filesystem permits user-created symlinks.
func ResolvePath(webdavRoot, userDirectory, bucket, key string) (string, error) {
	root, err := resolveUserRoot(webdavRoot, userDirectory)
	if err != nil {
		return "", err
	}
	if _, ok := supportedBuckets[bucket]; !ok {
		return "", fmt.Errorf("%w: %q", ErrInvalidBucket, bucket)
	}
	if strings.ContainsRune(key, '\x00') {
		return "", ErrInvalidKey
	}
	key = strings.ReplaceAll(key, "\\", "/")
	for _, segment := range strings.Split(key, "/") {
		if segment == ".." {
			return "", fmt.Errorf("%w: %q", ErrPathEscape, key)
		}
	}
	if key == "" {
		return filepath.Join(root, bucket), nil
	}
	cleanKey := path.Clean("/" + key)
	if cleanKey == "/" || strings.HasPrefix(cleanKey, "/../") || cleanKey == "/.." {
		return "", fmt.Errorf("%w: %q", ErrPathEscape, key)
	}
	cleanKey = strings.TrimPrefix(cleanKey, "/")
	if cleanKey == "" {
		return "", ErrInvalidKey
	}

	target := filepath.Join(root, bucket, filepath.FromSlash(cleanKey))
	if !isWithin(root, target) {
		return "", fmt.Errorf("%w: %q", ErrPathEscape, key)
	}
	return target, nil
}

func resolveUserRoot(webdavRoot, userDirectory string) (string, error) {
	webdavRoot = strings.TrimSpace(webdavRoot)
	userDirectory = strings.TrimSpace(userDirectory)
	if webdavRoot == "" || userDirectory == "" || strings.ContainsRune(userDirectory, '\x00') {
		return "", ErrPathEscape
	}
	root := filepath.Clean(webdavRoot)
	userRoot := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(strings.ReplaceAll(userDirectory, "\\", "/"), "/")))
	if !isWithin(root, userRoot) {
		return "", ErrPathEscape
	}
	return userRoot, nil
}

func isWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return !filepath.IsAbs(rel)
}
