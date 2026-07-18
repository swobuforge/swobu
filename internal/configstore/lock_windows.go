//go:build windows

package configstore

import (
	"os"

	"golang.org/x/sys/windows"
)

func acquireFileLock(file *os.File) error {
	overlapped := new(windows.Overlapped)
	return windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		overlapped,
	)
}

func releaseFileLock(file *os.File) error {
	overlapped := new(windows.Overlapped)
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped)
}

// Windows access control is ACL-based; os.FileMode permission bits do not
// truthfully describe whether another principal can read the directory.
func validatePrivateDirectory(os.FileInfo) error { return nil }
