package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func testDirectoryFlushAfterRename(t *testing.T, open func(string) (*os.File, error)) {
	root := t.TempDir()
	path := filepath.Join(root, "before")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	directory, err := open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if err := os.Rename(path, filepath.Join(root, "after")); err != nil {
		t.Fatal(err)
	}
	// A path lookup would now fail. The flush still owes the original object.
	if err := SyncDirectory(directory); err != nil {
		t.Fatal(err)
	}
}

func TestDirectoryFlushRejectsWrongOrClosedHandle(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "file")
	if err != nil {
		t.Fatal(err)
	}
	if err := SyncDirectory(file); err == nil {
		t.Fatal("file accepted as directory")
	}
	file.Close()
	if err := SyncDirectory(file); err == nil {
		t.Fatal("closed handle accepted")
	}
}
