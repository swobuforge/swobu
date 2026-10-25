package atomicfile

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestDirectoryFlushUsesOpenedObjectAfterRename(t *testing.T) {
	// os.Open on Windows pins a directory name; a share-delete handle permits
	// the rename needed to falsify an implementation that reopens file.Name().
	testDirectoryFlushAfterRename(t, func(path string) (*os.File, error) {
		name, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return nil, err
		}
		handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
		if err != nil {
			return nil, err
		}
		return os.NewFile(uintptr(handle), path), nil
	})
}
