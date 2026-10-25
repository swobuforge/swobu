//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package connect

import (
	"errors"
	"os"
)

func lockAttemptAdmission(*os.File) error {
	return errors.New("client attempt admission locking unsupported on this platform")
}
