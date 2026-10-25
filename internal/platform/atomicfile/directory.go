package atomicfile

import (
	"errors"
	"os"
)

// SyncDirectory flushes the opened directory's metadata. Windows requires a
// writable directory handle for FlushFileBuffers; Go's os.Open is read-only.
// Reopening must retain object identity, including for os.Root consumers.
// Failure remains failure: callers retain their original commit/dispatch law.
func SyncDirectory(directory *os.File) error {
	info, err := directory.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("directory flush requires a directory")
	}
	return syncDirectoryHandle(directory)
}
