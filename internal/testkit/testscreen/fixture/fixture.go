package fixture

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/swobuforge/swobu/internal/testkit/testscreen"
	testscreendiff "github.com/swobuforge/swobu/internal/testkit/testscreen/diff"
	"github.com/swobuforge/swobu/internal/testkit/testscreen/testpath"
)

const (
	UpdateEnv        = "SWOBU_UPDATE_FIXTURES"
	DefaultBaseDir   = "testdata"
	DefaultAssertion = "default"
	DefaultMinCols   = 60
	DefaultMinRows   = 18
)

type Config struct {
	Path             string
	MinCols, MinRows int
	ExactViewport    bool
}

func Path(testID, assertion string) string { return PathIn(DefaultBaseDir, testID, assertion) }
func PathIn(baseDir, testID, assertion string) string {
	base := strings.TrimSpace(baseDir)
	if base == "" {
		base = DefaultBaseDir
	}
	name := strings.TrimSpace(assertion)
	if name == "" {
		name = DefaultAssertion
	}
	return filepath.Join(base, testpath.TestIDToken(testID), "fixture", testpath.Token(name)+".ansi")
}

type Builder struct{ config Config }

func BuilderFor(testID, assertion string) Builder {
	return Builder{Config{Path: Path(testID, assertion), MinCols: DefaultMinCols, MinRows: DefaultMinRows}}
}
func (b Builder) Fixture(path string) Builder {
	if path = strings.TrimSpace(path); path != "" {
		b.config.Path = path
	}
	return b
}
func (b Builder) Viewport(cols, rows int) Builder {
	if cols > 0 {
		b.config.MinCols = cols
	}
	if rows > 0 {
		b.config.MinRows = rows
	}
	return b
}
func (b Builder) ExactViewport(cols, rows int) Builder {
	b = b.Viewport(cols, rows)
	b.config.ExactViewport = true
	return b
}
func (b Builder) Config() Config { return b.config }

type Report struct {
	FixturePath, Expected, Actual, Diff string
	Err                                 error
}

func CompareScreen(actual testscreen.Screen, cfg Config) Report {
	return compare(actual, cfg, UpdateEnv)
}
func compare(actual testscreen.Screen, cfg Config, updateEnv string) Report {
	path := strings.TrimSpace(cfg.Path)
	if path == "" {
		return Report{Err: fmt.Errorf("visual fixture path is required")}
	}
	promote, err := updateEnabled(updateEnv)
	if err != nil {
		return Report{FixturePath: path, Actual: actual.String(), Err: err}
	}
	if cfg.ExactViewport {
		width, height := actual.Size()
		if width != cfg.MinCols || height != cfg.MinRows {
			return Report{
				FixturePath: path,
				Actual:      actual.String(),
				Err:         fmt.Errorf("actual viewport is %dx%d, want exact %dx%d", width, height, cfg.MinCols, cfg.MinRows),
			}
		}
	}
	fixtureScreen := actual
	canonical := testscreen.WriteANSI(fixtureScreen)
	if cfg.ExactViewport {
		canonical = testscreen.WriteANSIExact(fixtureScreen)
	}
	if promote {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return Report{FixturePath: path, Err: fmt.Errorf("create fixture dir: %w", err)}
		}
		if err := os.WriteFile(path, canonical, 0o644); err != nil {
			return Report{FixturePath: path, Err: fmt.Errorf("write fixture %q: %w", path, err)}
		}
		return Report{FixturePath: path}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Report{FixturePath: path, Actual: actual.String(), Err: fmt.Errorf("missing visual fixture %q; update with %s=1 go test ./... and review the diff", path, updateEnv)}
		}
		return Report{FixturePath: path, Err: fmt.Errorf("read fixture %q: %w", path, err)}
	}
	expected, err := testscreen.ParseANSI(raw)
	if err != nil {
		return Report{FixturePath: path, Actual: actual.String(), Err: fmt.Errorf("parse fixture %q: %w", path, err)}
	}
	normalized := testscreen.WriteANSI(expected)
	if cfg.ExactViewport {
		normalized = testscreen.WriteANSIExact(expected)
	}
	if !bytes.Equal(normalized, raw) {
		return Report{FixturePath: path, Err: fmt.Errorf("visual fixture %q is not canonical; update with %s=1", path, updateEnv)}
	}
	mode := testscreendiff.MinViewport
	if cfg.ExactViewport {
		mode = testscreendiff.ExactViewport
	}
	res, err := testscreendiff.CompareViewport(expected, actual, cfg.MinCols, cfg.MinRows, mode)
	if err == nil {
		return Report{FixturePath: path}
	}
	return Report{FixturePath: path, Expected: res.Expected, Actual: res.Actual, Diff: res.Diff, Err: fmt.Errorf("visual mismatch fixture=%q", path)}
}

func updateEnabled(key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return false, nil
	}
	if v != "1" {
		return false, fmt.Errorf("%s must be unset or 1", key)
	}
	return true, nil
}
