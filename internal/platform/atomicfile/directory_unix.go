//go:build !windows

package atomicfile

import "os"

func syncDirectoryHandle(directory *os.File) error { return directory.Sync() }
