//go:build windows

package clientconnect

import "golang.org/x/sys/windows"

func replaceForeignFile(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	// The sibling temporary file is complete and closed before replacement. The
	// kernel promises no half-written config, not database-style crash durability.
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING)
}
