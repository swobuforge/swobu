package clientconnect

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const maxForeignConfigurationBytes int64 = 16 << 20

type foreignFile struct {
	logical  string
	physical string
	raw      []byte
	mode     fs.FileMode
	existed  bool
}

func inspectForeignFile(logical string, empty []byte) (foreignFile, error) {
	physical, err := resolvePhysicalPath(logical)
	if err != nil {
		return foreignFile{}, err
	}
	_, err = os.Stat(physical)
	if errors.Is(err, os.ErrNotExist) {
		return foreignFile{logical: logical, physical: physical, raw: append([]byte(nil), empty...), mode: 0o600}, nil
	}
	if err != nil {
		return foreignFile{}, err
	}
	raw, mode, err := readBoundedForeignFileSnapshot(physical)
	if err != nil {
		return foreignFile{}, err
	}
	return foreignFile{logical: logical, physical: physical, raw: raw, mode: mode.Perm(), existed: true}, nil
}

func readBoundedForeignFileSnapshot(path string) ([]byte, fs.FileMode, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("client configuration is not a regular file")
	}
	if info.Size() > maxForeignConfigurationBytes {
		return nil, 0, fmt.Errorf("client configuration exceeds 16 MiB")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxForeignConfigurationBytes+1))
	if err != nil {
		return nil, 0, err
	}
	if int64(len(raw)) > maxForeignConfigurationBytes {
		return nil, 0, fmt.Errorf("client configuration exceeds 16 MiB")
	}
	return raw, info.Mode(), nil
}

func resolvePhysicalPath(logical string) (string, error) {
	abs, err := filepath.Abs(logical)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(abs); err == nil {
		return filepath.EvalSymlinks(abs)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if errors.Is(err, os.ErrNotExist) {
		return abs, nil
	}
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filepath.Base(abs)), nil
}

func (f foreignFile) replace(next []byte) error {
	if int64(len(next)) > maxForeignConfigurationBytes {
		return fmt.Errorf("planned client configuration exceeds 16 MiB; nothing was overwritten")
	}
	if err := os.MkdirAll(filepath.Dir(f.physical), 0o700); err != nil {
		return fmt.Errorf("create client configuration directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(f.physical), "."+filepath.Base(f.physical)+".swobu-*")
	if err != nil {
		return fmt.Errorf("create client configuration temporary file: %w", err)
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	mode := f.mode
	if mode == 0 {
		mode = 0o600
	}
	if err := temp.Chmod(mode); err != nil {
		return err
	}
	if _, err := temp.Write(next); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := replaceForeignFile(tempPath, f.physical); err != nil {
		return fmt.Errorf("replace client configuration: %w", err)
	}
	committed = true
	return nil
}
