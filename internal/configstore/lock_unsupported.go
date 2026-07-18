//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package configstore

import (
	"fmt"
	"os"
)

func acquireFileLock(*os.File) error {
	return fmt.Errorf("file locking is unsupported on this platform")
}
func releaseFileLock(*os.File) error             { return nil }
func validatePrivateDirectory(os.FileInfo) error { return nil }
