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
	"github.com/swobuforge/swobu/testscreen/testpath"
)

const (
	// UpdateEnv is the single promotion knob for text terminal visual fixtures.
	UpdateEnv = "SWOBU_UPDATE_FIXTURES"

	DefaultBaseDir   = "testdata"
	DefaultAssertion = "default"
	DefaultMinCols   = 60
	DefaultMinRows   = 18
)

// Config holds one fixture-backed comparison configuration.
type Config struct {
	Path      string
	Normalize func(string) string
	MinCols   int
	MinRows   int
}

// Path builds the canonical visual fixture path:
// testdata/<testid>/fixture/<assertion>.txt.
func Path(testID, assertion string) string {
	return PathIn(DefaultBaseDir, testID, assertion)
}

// PathIn builds the canonical visual fixture path under a caller-selected
// base directory.
func PathIn(baseDir, testID, assertion string) string {
	base := strings.TrimSpace(baseDir)
	if base == "" {
		base = DefaultBaseDir
	}
	name := strings.TrimSpace(assertion)
	if name == "" {
		name = DefaultAssertion
	}
	return filepath.Join(base, testpath.TestIDToken(testID), "fixture", testpath.Token(name)+".txt")
}

// ConfigFor builds a fixture config for the canonical visual fixture path.
func ConfigFor(testID, assertion string) Config {
	return Config{Path: Path(testID, assertion)}
}

// Builder carries the shared fixture configuration chain used by surface
// testkits. Surfaces add only their own terminal operation, such as Now or
// Eventually.
type Builder struct {
	config Config
}

// BuilderFor builds a visual fixture builder with the shared default viewport.
func BuilderFor(testID, assertion string) Builder {
	return Builder{config: Config{
		Path:    Path(testID, assertion),
		MinCols: DefaultMinCols,
		MinRows: DefaultMinRows,
	}}
}

// Normalize sets snapshot normalization for this visual assertion.
func (b Builder) Normalize(fn func(string) string) Builder {
	b.config.Normalize = fn
	return b
}

// Fixture overrides the derived fixture path when a local proof needs an
// explicit file.
func (b Builder) Fixture(path string) Builder {
	path = strings.TrimSpace(path)
	if path != "" {
		b.config.Path = path
	}
	return b
}

// Viewport sets minimum comparison dimensions for fixed terminal frames.
func (b Builder) Viewport(minCols, minRows int) Builder {
	if minCols > 0 {
		b.config.MinCols = minCols
	}
	if minRows > 0 {
		b.config.MinRows = minRows
	}
	return b
}

// Config returns the immutable fixture comparison config for a surface hook.
func (b Builder) Config() Config {
	return b.config
}

// ConfigForIn builds a fixture config under a caller-selected base directory.
func ConfigForIn(baseDir, testID, assertion string) Config {
	return Config{Path: PathIn(baseDir, testID, assertion)}
}

// Report is the result of comparing a rendered snapshot against a fixture.
type Report struct {
	FixturePath string
	Expected    string
	Actual      string
	Diff        string
	Err         error
}

// CompareSnapshot checks snapshot against the fixture configuration using the
// shared visual fixture promotion environment.
func CompareSnapshot(snapshot string, cfg Config) Report {
	return Compare(snapshot, cfg, UpdateEnv)
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
