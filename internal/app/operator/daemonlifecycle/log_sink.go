package daemonlifecycle

import (
	"os"
	"path/filepath"
	"strings"
)

func openDaemonLogSink() (string, *os.File, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(cacheDir) == "" {
		file, tempErr := os.CreateTemp("", "swobu-daemon-*.log")
		if tempErr != nil {
			return "", nil, tempErr
		}
		return file.Name(), file, nil
	}
	logDir := filepath.Join(cacheDir, "swobu", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", nil, err
	}
	logPath := filepath.Join(logDir, "daemon.log")
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", nil, err
	}
	return logPath, file, nil
}
