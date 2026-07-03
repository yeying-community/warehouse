package atomicfile

import (
	"io"
	"os"
	"path/filepath"
)

const tempPattern = "._upload-*"

type File struct {
	*os.File
	tempPath   string
	targetPath string
	closed     bool
}

func (f *File) TempPath() string {
	return f.tempPath
}

func Open(targetPath string, perm os.FileMode) (*File, error) {
	tempFile, err := os.CreateTemp(filepath.Dir(targetPath), tempPattern)
	if err != nil {
		return nil, err
	}
	if err := tempFile.Chmod(perm); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempFile.Name())
		return nil, err
	}
	return &File{
		File:       tempFile,
		tempPath:   tempFile.Name(),
		targetPath: targetPath,
	}, nil
}

func (f *File) Close() error {
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
	return SyncParent(filepath.Dir(f.targetPath))
}

func (f *File) Abort() {
	if f.closed {
		return
	}
	f.closed = true
	_ = f.File.Close()
	_ = os.Remove(f.tempPath)
}

func WriteAll(targetPath string, src io.Reader, perm os.FileMode) error {
	f, err := Open(targetPath, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, src); err != nil {
		f.Abort()
		return err
	}
	return f.Close()
}

func SyncParent(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Sync(); err != nil && err != os.ErrInvalid {
		return err
	}
	return nil
}
