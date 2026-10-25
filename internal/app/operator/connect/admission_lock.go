package connect

import (
	"errors"
	"os"
	"path/filepath"
)

func (attempts *Attempts) admissionLock() (*os.File, error) {
	path := filepath.Join(attempts.directory, ".admission.lock")
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular()) {
		return nil, errors.New("invalid client attempt admission lock")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	current, currentErr := os.Lstat(path)
	if err != nil || currentErr != nil || !current.Mode().IsRegular() || !os.SameFile(opened, current) {
		file.Close()
		return nil, errors.New("client attempt admission lock changed")
	}
	if err := lockAttemptAdmission(file); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
