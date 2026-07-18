//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package configstore

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// Platform locking stays behind this file so the durable store has no Unix
// dependency and every supported process uses the same lifetime-lock contract.
func acquireFileLock(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}

func releaseFileLock(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}

func validatePrivateDirectory(info os.FileInfo) error {
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("permissions %04o expose routing credentials; require owner-only access", info.Mode().Perm())
	}
	return nil
}
