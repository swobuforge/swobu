package atomicfile

import (
	"errors"
	"os"
	"runtime"

	"golang.org/x/sys/windows"
)

func syncDirectoryHandle(directory *os.File) error {
	defer runtime.KeepAlive(directory)
	// Go opens directories for reading. Resolve the opened object's current
	// name, then compare identities after acquiring a writable handle. A
	// rename/substitution can fail the flush but cannot flush a rival object.
	before, err := directory.Stat()
	if err != nil {
		return err
	}
	name := make([]uint16, 260)
	for {
		n, err := windows.GetFinalPathNameByHandle(windows.Handle(directory.Fd()), &name[0], uint32(len(name)), 0)
		if err != nil {
			return err
		}
		if n < uint32(len(name)) {
			break
		}
		if n >= 32768 {
			return errors.New("directory flush path exceeds Windows path limit")
		}
		name = make([]uint16, n+1)
	}
	handle, err := windows.CreateFile(&name[0], windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	writable := os.NewFile(uintptr(handle), windows.UTF16ToString(name))
	defer writable.Close()
	after, err := writable.Stat()
	if err != nil {
		return err
	}
	if !after.IsDir() || !os.SameFile(before, after) {
		return errors.New("directory changed before flush")
	}
	return writable.Sync()
}
