package webdavfs

import (
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/net/webdav"
)

// UnicodeFileSystem 包装 webdav.Dir 以正确支持 Unicode 路径
type UnicodeFileSystem struct {
	dir string
}

// NewUnicodeFileSystem 创建一个支持 Unicode 路径的 FileSystem
func NewUnicodeFileSystem(dir string) *UnicodeFileSystem {
	return &UnicodeFileSystem{dir: dir}
}

// Stat 返回文件信息
func (fsys *UnicodeFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	fullPath := filepath.Join(fsys.dir, name)
	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}
	baseName := path.Base(strings.TrimSuffix(filepath.ToSlash(name), "/"))
	if IsIgnoredName(baseName) {
		return nil, os.ErrNotExist
	}
	return &fileInfo{FileInfo: info, name: baseName}, nil
}

// OpenFile 打开或创建文件
func (fsys *UnicodeFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	baseName := path.Base(strings.TrimSuffix(filepath.ToSlash(name), "/"))
	if IsIgnoredName(baseName) {
		return nil, os.ErrNotExist
	}
	fullPath := filepath.Join(fsys.dir, name)
	if shouldAtomicWrite(flag) {
		return fsys.openAtomicWriteFile(fullPath, name, perm)
	}
	f, err := os.OpenFile(fullPath, flag, perm)
	if err != nil {
		return nil, err
	}
	return &file{File: f, name: filepath.ToSlash(name)}, nil
}

// Create 新建文件
func (fsys *UnicodeFileSystem) Create(ctx context.Context, name string) (webdav.File, error) {
	return fsys.OpenFile(ctx, name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
}

// Mkdir 新建目录
func (fsys *UnicodeFileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	baseName := path.Base(strings.TrimSuffix(filepath.ToSlash(name), "/"))
	if IsIgnoredName(baseName) {
		return os.ErrNotExist
	}
	fullPath := filepath.Join(fsys.dir, name)
	return os.MkdirAll(fullPath, perm)
}

// Rename 重命名/移动文件
func (fsys *UnicodeFileSystem) Rename(ctx context.Context, oldName, newName string) error {
	oldBase := path.Base(strings.TrimSuffix(filepath.ToSlash(oldName), "/"))
	newBase := path.Base(strings.TrimSuffix(filepath.ToSlash(newName), "/"))
	if IsIgnoredName(oldBase) || IsIgnoredName(newBase) {
		return os.ErrNotExist
	}
	oldPath := filepath.Join(fsys.dir, oldName)
	newPath := filepath.Join(fsys.dir, newName)
	return os.Rename(oldPath, newPath)
}

// RemoveAll 删除文件或目录
func (fsys *UnicodeFileSystem) RemoveAll(ctx context.Context, name string) error {
	baseName := path.Base(strings.TrimSuffix(filepath.ToSlash(name), "/"))
	if IsIgnoredName(baseName) {
		return os.ErrNotExist
	}
	fullPath := filepath.Join(fsys.dir, name)
	return os.RemoveAll(fullPath)
}

// ReadDir 读取目录内容
func (fsys *UnicodeFileSystem) ReadDir(ctx context.Context, name string) ([]os.FileInfo, error) {
	fullPath := filepath.Join(fsys.dir, name)
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, err
	}

	infos := make([]os.FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if IsIgnoredName(entry.Name()) {
			continue
		}
		infos = append(infos, &fileInfo{FileInfo: info, name: entry.Name()})
	}
	return infos, nil
}

// fileInfo 实现 os.FileInfo 并添加自定义名称
type fileInfo struct {
	os.FileInfo
	name string
}

func (fi *fileInfo) Name() string {
	return fi.name
}

// file 包装 os.File
type file struct {
	*os.File
	name string
}

func (f *file) Name() string {
	return f.name
}

type atomicWriteFile struct {
	*os.File
	name       string
	tempPath   string
	targetPath string
	closed     bool
}

func (f *atomicWriteFile) Name() string {
	return f.name
}

func (f *atomicWriteFile) Close() error {
	if f.closed {
		return nil
	}
	f.closed = true

	if _, err := f.Seek(0, io.SeekCurrent); err != nil {
		_ = f.File.Close()
		_ = os.Remove(f.tempPath)
		return err
	}
	if err := f.File.Sync(); err != nil {
		_ = f.File.Close()
		_ = os.Remove(f.tempPath)
		return err
	}
	if err := f.File.Close(); err != nil {
		_ = os.Remove(f.tempPath)
		return err
	}
	if err := os.Rename(f.tempPath, f.targetPath); err != nil {
		_ = os.Remove(f.tempPath)
		return err
	}
	return syncDirectory(filepath.Dir(f.targetPath))
}

func shouldAtomicWrite(flag int) bool {
	writeFlags := os.O_WRONLY | os.O_RDWR
	requiredFlags := os.O_CREATE | os.O_TRUNC
	return flag&writeFlags != 0 && flag&requiredFlags == requiredFlags
}

func (fsys *UnicodeFileSystem) openAtomicWriteFile(fullPath, name string, perm os.FileMode) (webdav.File, error) {
	tempFile, err := os.CreateTemp(filepath.Dir(fullPath), "._upload-*")
	if err != nil {
		return nil, err
	}
	if err := tempFile.Chmod(perm); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempFile.Name())
		return nil, err
	}
	return &atomicWriteFile{
		File:       tempFile,
		name:       filepath.ToSlash(name),
		tempPath:   tempFile.Name(),
		targetPath: fullPath,
	}, nil
}

func syncDirectory(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Sync(); err != nil && !isDirSyncUnsupported(err) {
		return err
	}
	return nil
}

func isDirSyncUnsupported(err error) bool {
	return err == os.ErrInvalid
}

// ResolvePath 解析并规范化路径
func ResolvePath(path string) string {
	path = filepath.Clean(path)
	path = strings.ReplaceAll(path, "\\", "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

// IsIgnoredName 判断是否为需要忽略的系统文件
func IsIgnoredName(name string) bool {
	if name == "" {
		return false
	}
	if name == ".DS_Store" || name == ".AppleDouble" || name == "Thumbs.db" {
		return true
	}
	if strings.HasPrefix(name, "._") {
		return true
	}
	return false
}

// 确保 UnicodeFileSystem 实现 webdav.FileSystem
var _ webdav.FileSystem = (*UnicodeFileSystem)(nil)
