package logging

import (
	"log/slog"
	"os"
	"strings"
)

const envLogLevel = "SWOBU_LOG_LEVEL"

func configuredLevel() (slog.Level, error) {
	raw := strings.TrimSpace(os.Getenv(envLogLevel)) // swobu:io-string source=boundary
	if raw == "" {
		return slog.LevelInfo, nil
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(raw)); err != nil {
		return 0, err
	}
	return level, nil
}
