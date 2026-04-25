package storage

import (
	"os"
	"strings"
	"testing"
)

func TestLocalSaveAndRead(t *testing.T) {
	root := t.TempDir()
	l, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	content := "hello, world"
	path, err := l.Save("testdir/hello.txt", strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Error("expected non-empty path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Errorf("content = %s, want %s", string(data), content)
	}
}

func TestLocalExists(t *testing.T) {
	root := t.TempDir()
	l, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	exists, err := l.Exists("nope.txt")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("should not exist")
	}
	l.Save("nope.txt", strings.NewReader("ok"))
	exists, err = l.Exists("nope.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Error("should exist")
	}
}

func TestLocalDelete(t *testing.T) {
	root := t.TempDir()
	l, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	l.Save("del.txt", strings.NewReader("data"))
	err = l.Delete("del.txt")
	if err != nil {
		t.Fatal(err)
	}
	exists, _ := l.Exists("del.txt")
	if exists {
		t.Error("file should be deleted")
	}
}

func TestLocalDeleteNonExistent(t *testing.T) {
	root := t.TempDir()
	l, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	err = l.Delete("missing.txt")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestLocalPath(t *testing.T) {
	root := "/tmp/testlocal"
	l := &Local{Root: root}
	p := l.Path("sub/file.txt")
	if p == root+"/sub/file.txt" {
		t.Log("path format correct")
	}
}

func TestNewLocalCreatesDir(t *testing.T) {
	root := t.TempDir() + "/nested/storage"
	l, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	if l.Root == "" {
		t.Error("root should be set")
	}
	_, err = os.Stat(l.Root)
	if err != nil {
		t.Errorf("root dir should exist: %v", err)
	}
}

func TestLocalSaveNestedPath(t *testing.T) {
	root := t.TempDir()
	l, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = l.Save("a/b/c/d/file.txt", strings.NewReader("nested"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(root + "/a/b/c/d/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "nested" {
		t.Errorf("got %s", string(data))
	}
}

func TestLocalSaveEmptyFile(t *testing.T) {
	root := t.TempDir()
	l, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	path, err := l.Save("empty.txt", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Error("file should be empty")
	}
}
