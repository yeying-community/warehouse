package webdavfs

import (
	"bytes"
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/infrastructure/atomicfile"
	"golang.org/x/net/webdav"
)

// UnicodeFileSystem 包装 webdav.Dir 以正确支持 Unicode 路径
type UnicodeFileSystem struct {
	dir           string
	virtualByDir  map[string][]virtualFileEntry
	virtualByPath map[string]virtualFileEntry
}

// VirtualFile 是不落盘、只读展示在 WebDAV 目录中的文件。
type VirtualFile struct {
	Path    string
	Content []byte
	ModTime time.Time
	Mode    os.FileMode
}

// NewUnicodeFileSystem 创建一个支持 Unicode 路径的 FileSystem
func NewUnicodeFileSystem(dir string) *UnicodeFileSystem {
	return NewUnicodeFileSystemWithVirtualFiles(dir, nil)
}

func NewUnicodeFileSystemWithVirtualFiles(dir string, virtualFiles []VirtualFile) *UnicodeFileSystem {
	fsys := &UnicodeFileSystem{
		dir:           dir,
		virtualByDir:  make(map[string][]virtualFileEntry),
		virtualByPath: make(map[string]virtualFileEntry),
	}
	for _, item := range virtualFiles {
		entry, ok := newVirtualFileEntry(item)
		if !ok {
			continue
		}
		fsys.virtualByPath[entry.path] = entry
		fsys.virtualByDir[entry.parent] = append(fsys.virtualByDir[entry.parent], entry)
	}
	return fsys
}

// Stat 返回文件信息
func (fsys *UnicodeFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	baseName := path.Base(strings.TrimSuffix(filepath.ToSlash(name), "/"))
	if IsIgnoredName(baseName) {
		return nil, os.ErrNotExist
	}
	fullPath := filepath.Join(fsys.dir, name)
	info, err := os.Stat(fullPath)
	if err != nil {
		if entry, ok := fsys.virtualEntryIfNoRealFile(name, err); ok {
			return entry.fileInfo(), nil
		}
		return nil, err
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
	if entry, ok, err := fsys.virtualEntryForOpen(name, fullPath); ok || err != nil {
		if err != nil {
			return nil, err
		}
		if opensForWrite(flag) {
			return nil, os.ErrPermission
		}
		return entry.open(), nil
	}
	if shouldAtomicWrite(flag) {
		return fsys.openAtomicWriteFile(fullPath, name, perm)
	}
	f, err := os.OpenFile(fullPath, flag, perm)
	if err != nil {
		return nil, err
	}
	return &file{
		File:           f,
		name:           filepath.ToSlash(name),
		virtualEntries: fsys.virtualByDir[normalizeFSPath(name)],
	}, nil
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
	if fsys.isVirtualOnly(name) {
		return os.ErrPermission
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
	if fsys.isVirtualOnly(oldName) || fsys.isVirtualOnly(newName) {
		return os.ErrPermission
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
	if fsys.isVirtualOnly(name) {
		return os.ErrPermission
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
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if IsIgnoredName(entry.Name()) {
			continue
		}
		seen[entry.Name()] = struct{}{}
		infos = append(infos, &fileInfo{FileInfo: info, name: entry.Name()})
	}
	for _, entry := range fsys.virtualByDir[normalizeFSPath(name)] {
		if _, ok := seen[entry.name]; ok {
			continue
		}
		infos = append(infos, entry.fileInfo())
	}
	return infos, nil
}

func (fsys *UnicodeFileSystem) virtualEntryIfNoRealFile(name string, statErr error) (virtualFileEntry, bool) {
	if !os.IsNotExist(statErr) {
		return virtualFileEntry{}, false
	}
	entry, ok := fsys.virtualByPath[normalizeFSPath(name)]
	return entry, ok
}

func (fsys *UnicodeFileSystem) virtualEntryForOpen(name, fullPath string) (virtualFileEntry, bool, error) {
	entry, ok := fsys.virtualByPath[normalizeFSPath(name)]
	if !ok {
		return virtualFileEntry{}, false, nil
	}
	if _, err := os.Stat(fullPath); err == nil {
		return virtualFileEntry{}, false, nil
	} else if !os.IsNotExist(err) {
		return virtualFileEntry{}, false, err
	}
	return entry, true, nil
}

func (fsys *UnicodeFileSystem) isVirtualOnly(name string) bool {
	fullPath := filepath.Join(fsys.dir, name)
	_, ok, err := fsys.virtualEntryForOpen(name, fullPath)
	return ok && err == nil
}

func opensForWrite(flag int) bool {
	return flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0
}

type virtualFileEntry struct {
	path    string
	parent  string
	name    string
	content []byte
	modTime time.Time
	mode    os.FileMode
}

func newVirtualFileEntry(item VirtualFile) (virtualFileEntry, bool) {
	virtualPath := normalizeFSPath(item.Path)
	if virtualPath == "/" {
		return virtualFileEntry{}, false
	}
	name := path.Base(virtualPath)
	if IsIgnoredName(name) {
		return virtualFileEntry{}, false
	}
	modTime := item.ModTime
	if modTime.IsZero() {
		modTime = time.Unix(0, 0).UTC()
	}
	mode := item.Mode
	if mode == 0 {
		mode = 0444
	}
	return virtualFileEntry{
		path:    virtualPath,
		parent:  path.Dir(virtualPath),
		name:    name,
		content: append([]byte(nil), item.Content...),
		modTime: modTime,
		mode:    mode,
	}, true
}

func (entry virtualFileEntry) fileInfo() os.FileInfo {
	return virtualFileInfo{
		name:    entry.name,
		size:    int64(len(entry.content)),
		mode:    entry.mode,
		modTime: entry.modTime,
	}
}

func (entry virtualFileEntry) open() webdav.File {
	return &virtualOpenFile{
		reader: bytes.NewReader(entry.content),
		info:   entry.fileInfo(),
	}
}

type virtualFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

func (info virtualFileInfo) Name() string       { return info.name }
func (info virtualFileInfo) Size() int64        { return info.size }
func (info virtualFileInfo) Mode() os.FileMode  { return info.mode }
func (info virtualFileInfo) ModTime() time.Time { return info.modTime }
func (info virtualFileInfo) IsDir() bool        { return false }
func (info virtualFileInfo) Sys() any           { return nil }

type virtualOpenFile struct {
	reader *bytes.Reader
	info   os.FileInfo
}

func (file *virtualOpenFile) Close() error               { return nil }
func (file *virtualOpenFile) Read(p []byte) (int, error) { return file.reader.Read(p) }
func (file *virtualOpenFile) Seek(offset int64, whence int) (int64, error) {
	return file.reader.Seek(offset, whence)
}
func (file *virtualOpenFile) Readdir(count int) ([]os.FileInfo, error) { return nil, os.ErrInvalid }
func (file *virtualOpenFile) Stat() (os.FileInfo, error)               { return file.info, nil }
func (file *virtualOpenFile) Write(p []byte) (int, error)              { return 0, os.ErrPermission }

func normalizeFSPath(name string) string {
	name = strings.TrimSpace(filepath.ToSlash(name))
	if name == "" {
		return "/"
	}
	if !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	return path.Clean(name)
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
	name           string
	virtualEntries []virtualFileEntry
}

func (f *file) Name() string {
	return f.name
}

func (f *file) Readdir(count int) ([]os.FileInfo, error) {
	infos, err := f.File.Readdir(count)
	if err != nil || count > 0 || len(f.virtualEntries) == 0 {
		return infos, err
	}

	seen := make(map[string]struct{}, len(infos))
	for _, info := range infos {
		seen[info.Name()] = struct{}{}
	}
	for _, entry := range f.virtualEntries {
		if _, ok := seen[entry.name]; ok {
			continue
		}
		infos = append(infos, entry.fileInfo())
	}
	return infos, nil
}

type atomicWriteFile struct {
	*atomicfile.File
	name string
}

func (f *atomicWriteFile) Name() string {
	return f.name
}

func shouldAtomicWrite(flag int) bool {
	writeFlags := os.O_WRONLY | os.O_RDWR
	requiredFlags := os.O_CREATE | os.O_TRUNC
	return flag&writeFlags != 0 && flag&requiredFlags == requiredFlags
}

func (fsys *UnicodeFileSystem) openAtomicWriteFile(fullPath, name string, perm os.FileMode) (webdav.File, error) {
	tempFile, err := atomicfile.Open(fullPath, perm)
	if err != nil {
		return nil, err
	}
	return &atomicWriteFile{
		File: tempFile,
		name: filepath.ToSlash(name),
	}, nil
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
