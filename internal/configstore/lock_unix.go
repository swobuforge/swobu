//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package configstore

import (
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
