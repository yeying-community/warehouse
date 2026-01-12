package webdavfs

import (
	"context"
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
	baseName := path.Base(filepath.ToSlash(name))
	return &fileInfo{FileInfo: info, name: baseName}, nil
}

// OpenFile 打开或创建文件
func (fsys *UnicodeFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	fullPath := filepath.Join(fsys.dir, name)
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
	fullPath := filepath.Join(fsys.dir, name)
	return os.MkdirAll(fullPath, perm)
}

// Rename 重命名/移动文件
func (fsys *UnicodeFileSystem) Rename(ctx context.Context, oldName, newName string) error {
	oldPath := filepath.Join(fsys.dir, oldName)
	newPath := filepath.Join(fsys.dir, newName)
	return os.Rename(oldPath, newPath)
}

// RemoveAll 删除文件或目录
func (fsys *UnicodeFileSystem) RemoveAll(ctx context.Context, name string) error {
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

// ResolvePath 解析并规范化路径
func ResolvePath(path string) string {
	path = filepath.Clean(path)
	path = strings.ReplaceAll(path, "\\", "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

// 确保 UnicodeFileSystem 实现 webdav.FileSystem
var _ webdav.FileSystem = (*UnicodeFileSystem)(nil)
