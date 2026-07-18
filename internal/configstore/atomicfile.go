package configstore

import (
	"fmt"
	"os"
	"path/filepath"
)

type fileOps struct {
	createTemp func(string, string) (*os.File, error)
	write      func(*os.File, []byte) (int, error)
	syncFile   func(*os.File) error
	rename     func(string, string) error
	syncDir    func(string) error
}

func defaultFileOps() fileOps {
	return fileOps{
		createTemp: os.CreateTemp,
		write:      func(file *os.File, raw []byte) (int, error) { return file.Write(raw) },
		syncFile:   func(file *os.File) error { return file.Sync() },
		rename:     os.Rename,
		syncDir:    syncDirectory,
	}
}

func replaceFile(path string, raw []byte, ops fileOps) error {
	temp, err := ops.createTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create config temp: %w", err)
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return fmt.Errorf("secure config temp: %w", err)
	}
	written, err := ops.write(temp, raw)
	if err != nil {
		return fmt.Errorf("write config temp: %w", err)
	}
	if written != len(raw) {
		return fmt.Errorf("write config temp: short write: wrote %d of %d bytes", written, len(raw))
	}
	if err := ops.syncFile(temp); err != nil {
		return fmt.Errorf("flush config temp: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close config temp: %w", err)
	}
	if err := ops.rename(tempPath, path); err != nil {
		return fmt.Errorf("replace routing config: %w", err)
	}
	committed = true
	return nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
