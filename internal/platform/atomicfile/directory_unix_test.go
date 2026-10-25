//go:build !windows

package atomicfile

import (
	"os"
	"testing"
)

func TestDirectoryFlushUsesOpenedObjectAfterRename(t *testing.T) {
	testDirectoryFlushAfterRename(t, os.Open)
}
