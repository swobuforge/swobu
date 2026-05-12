package root

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const rootWireframeFixtureDir = "testdata/wireframes"

func assertVisualByKey(t *testing.T, visible, assertName string) {
	t.Helper()
	key := buildVisualFixtureKey(t, assertName)
	path := filepath.Join(rootWireframeFixtureDir, key.TestName, key.AssertName+".txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create visual fixture dir: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		if writeErr := os.WriteFile(path, []byte(visible), 0o644); writeErr != nil {
			t.Fatalf("bootstrap visual fixture %q: %v", path, writeErr)
		}
		t.Fatalf("missing visual fixture bootstrapped at %q; review and re-run", path)
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read visual fixture %q: %v", path, err)
	}
	if string(expected) == visible {
		return
	}
	artifactDir := filepath.Join(rootWireframeFixtureDir, ".artifacts", key.TestName, key.AssertName)
	_ = os.MkdirAll(artifactDir, 0o755)
	expectedArtifact := filepath.Join(artifactDir, "expected.txt")
	actualArtifact := filepath.Join(artifactDir, "actual.txt")
	diffArtifact := filepath.Join(artifactDir, "diff.txt")
	_ = os.WriteFile(expectedArtifact, expected, 0o644)
	_ = os.WriteFile(actualArtifact, []byte(visible), 0o644)
	_ = os.WriteFile(diffArtifact, []byte(lineDiff(string(expected), visible)), 0o644)
	t.Fatalf("visual mismatch fixture=%q\nartifacts: expected=%s actual=%s diff=%s", path, expectedArtifact, actualArtifact, diffArtifact)
}

type visualFixtureKey struct {
	TestName   string
	AssertName string
}

func buildVisualFixtureKey(t *testing.T, assertName string) visualFixtureKey {
	t.Helper()
	testName := normalizeVisualToken(t.Name())
	if testName == "" {
		testName = "unknown_test"
	}
	name := normalizeVisualToken(assertName)
	if name == "" {
		t.Fatalf("assert name is required")
	}
	return visualFixtureKey{TestName: testName, AssertName: name}
}

func normalizeVisualToken(v string) string {
	value := strings.TrimSpace(strings.ToLower(v))
	value = strings.ReplaceAll(value, "/", "_")
	value = strings.ReplaceAll(value, " ", "_")
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteRune('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func lineDiff(expected, actual string) string {
	e := strings.Split(expected, "\n")
	a := strings.Split(actual, "\n")
	max := len(e)
	if len(a) > max {
		max = len(a)
	}
	var out strings.Builder
	for i := 0; i < max; i++ {
		var ev, av string
		if i < len(e) {
			ev = e[i]
		}
		if i < len(a) {
			av = a[i]
		}
		if ev == av {
			continue
		}
		_, _ = fmt.Fprintf(&out, "line %d\n- %s\n+ %s\n", i+1, ev, av)
	}
	return out.String()
}

func sourceRootTestFile() string {
	pcs := make([]uintptr, 8)
	n := runtime.Callers(2, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if strings.HasSuffix(frame.File, "_test.go") {
			return frame.File
		}
		if !more {
			break
		}
	}
	return ""
}
