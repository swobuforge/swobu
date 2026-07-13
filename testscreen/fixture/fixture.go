// Package fixture owns deterministic fixture-backed visual comparison for
// terminal screen snapshots.
//
// Boundary law:
//   - reads/writes test-local fixture files only
//   - no test runtime concerns (retry, timing, pty, daemon)
//   - promotion gate via env var
package fixture

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	testscreendiff "github.com/swobuforge/swobu/testscreen/diff"
)

// Config holds one fixture-backed comparison configuration.
type Config struct {
	Path      string
	Normalize func(string) string
	MinCols   int
	MinRows   int
}

// Report is the result of comparing a rendered snapshot against a fixture.
type Report struct {
	FixturePath string
	Expected    string
	Actual      string
	Diff        string
	Err         error
}

// Compare checks snapshot against the fixture configuration.
// When updateEnv is non-empty and set to a truthy value, it writes snapshot
// to the fixture path instead of comparing.
func Compare(snapshot string, cfg Config, updateEnv string) Report {
	path := strings.TrimSpace(cfg.Path)
	if path == "" {
		return Report{Err: fmt.Errorf("visual fixture path is required")}
	}
	if updateEnabled(updateEnv) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return Report{FixturePath: path, Err: fmt.Errorf("create fixture dir: %w", err)}
		}
		if err := os.WriteFile(path, []byte(snapshot), 0o644); err != nil {
			return Report{FixturePath: path, Err: fmt.Errorf("write fixture %q: %w", path, err)}
		}
		return Report{FixturePath: path}
	}
	expectedRaw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Report{
				FixturePath: path,
				Actual:      snapshot,
				Err: fmt.Errorf(
					"missing visual fixture %q; review snapshot and promote with %s=1 go test ./...",
					path, updateEnv,
				),
			}
		}
		return Report{FixturePath: path, Err: fmt.Errorf("read fixture %q: %w", path, err)}
	}
	res, err := testscreendiff.CompareStrings(string(expectedRaw), snapshot, cfg.MinCols, cfg.MinRows, cfg.Normalize)
	if err == nil {
		return Report{FixturePath: path}
	}
	return Report{
		FixturePath: path,
		Expected:    res.Expected,
		Actual:      res.Actual,
		Diff:        res.Diff,
		Err:         fmt.Errorf("visual mismatch fixture=%q", path),
	}
}

func updateEnabled(envKey string) bool {
	if envKey == "" {
		return false
	}
	value := strings.TrimSpace(os.Getenv(envKey))
	if value == "" {
		return false
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
