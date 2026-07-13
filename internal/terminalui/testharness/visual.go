package testharness

import (
	"path/filepath"
	"strings"

	"github.com/swobuforge/swobu/testscreen/fixture"
	testingpath "github.com/swobuforge/swobu/testscreen/testpath"
)

const updateWireframesEnv = "SWOBU_UPDATE_WIREFRAMES"

// ViewReport aliases fixture.Report so callers in the _test package do not need
type ViewReport = fixture.Report

// VisualAssertBuilder creates a fixture-backed visual assert.
// The fixture path is derived as:
//
//	testdata/<testfile>__<testname>/fixture/<assertname>.txt
func AssertVisual(assertName string) VisualAssertBuilder {
	return VisualAssertBuilder{
		fixture: fixture.Config{
			Path:    deriveVisualFixturePath(assertName),
			MinCols: 60,
			MinRows: 18,
		},
	}
}

// VisualAssertBuilder chains configuration for a visual assertion.
type VisualAssertBuilder struct {
	fixture fixture.Config
}

// Normalize applies a function to both expected and actual before comparing.
func (b VisualAssertBuilder) Normalize(fn func(string) string) VisualAssertBuilder {
	b.fixture.Normalize = fn
	return b
}

// Fixture overrides the derived fixture path.
func (b VisualAssertBuilder) Fixture(path string) VisualAssertBuilder {
	path = strings.TrimSpace(path)
	if path != "" {
		b.fixture.Path = path
	}
	return b
}

// Viewport sets the minimum viewport dimensions used when normalizing.
func (b VisualAssertBuilder) Viewport(minCols, minRows int) VisualAssertBuilder {
	if minCols > 0 {
		b.fixture.MinCols = minCols
	}
	if minRows > 0 {
		b.fixture.MinRows = minRows
	}
	return b
}

// Against compares snapshot against the configured fixture and returns a report.
func (b VisualAssertBuilder) Against(snapshot string) ViewReport {
	return fixture.Compare(snapshot, b.fixture, updateWireframesEnv)
}

// Now compares snapshot against the configured fixture and returns an error on mismatch.
func (b VisualAssertBuilder) Now(snapshot string) error {
	return fixture.Compare(snapshot, b.fixture, updateWireframesEnv).Err
}

func deriveVisualFixturePath(assertName string) string {
	name := testingpath.Token(assertName)
	if name == "" {
		name = "assert"
	}
	testFile := "unknown_testfile"
	testName := "unknown_testname"
	file, fn, line, ok := testingpath.CallerTestFrame([]string{"/opencore/internal/terminalui/testharness/"})
	if ok {
		testFile = testingpath.FileStem(file)
		testName = testingpath.FunctionToken(fn, line)
	}
	testID := testingpath.TestID(testFile, testName)
	baseName := name + ".txt"
	return filepath.Join("testdata", testID, "fixture", baseName)
}
