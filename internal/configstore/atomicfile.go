package configstore

import (
	"os"

	"github.com/swobuforge/swobu/internal/platform/atomicfile"
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
	return atomicfile.ReplaceWith(path, raw, 0o600, atomicfile.Operations{
		CreateTemp: ops.createTemp,
		Write:      ops.write,
		SyncFile:   ops.syncFile,
		Rename:     ops.rename,
		SyncDir:    func(string) error { return nil },
	})
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
