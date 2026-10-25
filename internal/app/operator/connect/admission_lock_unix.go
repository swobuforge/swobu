//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package connect

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockAttemptAdmission(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX)
}
