package storage

import (
	"io"
	"os"
	"path/filepath"
)

type StorageBackend interface {
	Save(relativePath string, reader io.Reader) (string, error)
	Delete(relativePath string) error
	Path(relativePath string) string
	Exists(relativePath string) (bool, error)
}

type Local struct {
	Root string
}

func NewLocal(root string) (*Local, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0755); err != nil {
		return nil, err
	}
	return &Local{Root: abs}, nil
}

func (l *Local) Save(relativePath string, reader io.Reader) (string, error) {
	fullPath := filepath.Join(l.Root, relativePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return "", err
	}
	f, err := os.Create(fullPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, reader); err != nil {
		return "", err
	}
	return fullPath, nil
}

func (l *Local) Delete(relativePath string) error {
	fullPath := filepath.Join(l.Root, relativePath)
	return os.Remove(fullPath)
}

func (l *Local) Path(relativePath string) string {
	return filepath.Join(l.Root, relativePath)
}

func (l *Local) Exists(relativePath string) (bool, error) {
	_, err := os.Stat(filepath.Join(l.Root, relativePath))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
