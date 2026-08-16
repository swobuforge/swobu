//go:build !windows

package clientconnect

import (
	"os"
)

func replaceForeignFile(source, destination string) error {
	return os.Rename(source, destination)
}
