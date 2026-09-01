// Package atomicfile owns private-file replacement primitives shared by local
// persistence adapters.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

type Operations struct {
	CreateTemp func(string, string) (*os.File, error)
	Write      func(*os.File, []byte) (int, error)
	SyncFile   func(*os.File) error
	Rename     func(string, string) error
	SyncDir    func(string) error
}

func DefaultOperations() Operations {
	return Operations{
		CreateTemp: os.CreateTemp,
		Write:      func(file *os.File, raw []byte) (int, error) { return file.Write(raw) },
		SyncFile:   func(file *os.File) error { return file.Sync() },
		Rename:     os.Rename,
		SyncDir: func(path string) error {
			dir, err := os.Open(path)
			if err != nil {
				return err
			}
			defer dir.Close()
			return dir.Sync()
		},
	}
}

func Replace(path string, raw []byte, mode os.FileMode) error {
	return ReplaceWith(path, raw, mode, DefaultOperations())
}

func ReplaceWith(path string, raw []byte, mode os.FileMode, operations Operations) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create private directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("secure private directory: %w", err)
	}
	temp, err := operations.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create private temp: %w", err)
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(mode); err != nil {
		return fmt.Errorf("secure private temp: %w", err)
	}
	written, err := operations.Write(temp, raw)
	if err != nil {
		return fmt.Errorf("write private temp: %w", err)
	}
	if written != len(raw) {
		return fmt.Errorf("write private temp: short write: wrote %d of %d bytes", written, len(raw))
	}
	if err := operations.SyncFile(temp); err != nil {
		return fmt.Errorf("flush private temp: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close private temp: %w", err)
	}
	if err := operations.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace private file: %w", err)
	}
	committed = true
	_ = operations.SyncDir(dir)
	return nil
}
