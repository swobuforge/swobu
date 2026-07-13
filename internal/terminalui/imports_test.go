package terminalui_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type listedPackage struct {
	ImportPath string   `json:"ImportPath"`
	Imports    []string `json:"Imports"`
}

func TestTerminalUIPackageBoundaries(t *testing.T) {
	t.Parallel()

	root := packageDir(t)
	cases := []struct {
		name     string
		pattern  string
		forbidFn func(string) bool
	}{
		{
			name:    "core",
			pattern: "./core",
			forbidFn: func(importPath string) bool {
				return strings.Contains(importPath, "/internal/terminalui/")
			},
		},
		{
			name:    "component",
			pattern: "./component",
			forbidFn: func(importPath string) bool {
				if !strings.Contains(importPath, "/internal/terminalui/") {
					return false
				}
				return !strings.Contains(importPath, "/internal/terminalui/core")
			},
		},
		{
			name:    "components",
			pattern: "./components/...",
			forbidFn: func(importPath string) bool {
				return strings.Contains(importPath, "/internal/terminalui/engine/retained/") ||
					strings.Contains(importPath, "/internal/terminalui/apps/") ||
					strings.Contains(importPath, "/internal/terminalui/view/retained")
			},
		},
		{
			name:    "transcript",
			pattern: "./transcript/...",
			forbidFn: func(importPath string) bool {
				return strings.Contains(importPath, "/internal/terminalui/engine/retained/") ||
					strings.Contains(importPath, "/internal/terminalui/apps/") ||
					strings.Contains(importPath, "/internal/terminalui/component/") ||
					strings.Contains(importPath, "/internal/terminalui/components/") ||
					strings.Contains(importPath, "/internal/terminalui/view/retained")
			},
		},
		{
			name:    "cockpit views——retained bridge allowlist",
			pattern: "./apps/cockpit/app/views/...",
			forbidFn: func(importPath string) bool {
				// rendergraph and corelower are already banned; corelower is
				// still needed for bridge functions during migration.
				if strings.Contains(importPath, "/internal/terminalui/engine/retained/rendergraph/") {
					return true
				}
				return false
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertNoForbiddenDirectImports(t, root, tc.pattern, tc.forbidFn)
		})
	}
}

func assertNoForbiddenDirectImports(t *testing.T, root, pattern string, forbidFn func(string) bool) {
	t.Helper()

	for _, pkg := range goListPackages(t, root, pattern) {
		for _, importPath := range pkg.Imports {
			if forbidFn(importPath) {
				t.Fatalf("%s imports forbidden package %q", pkg.ImportPath, importPath)
			}
		}
	}
}

func goListPackages(t *testing.T, root, pattern string) []listedPackage {
	t.Helper()

	cmd := exec.Command("go", "list", "-json", pattern)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list %s: %v\n%s", pattern, err, out)
	}

	dec := json.NewDecoder(bytes.NewReader(out))
	pkgs := make([]listedPackage, 0, 8)
	for {
		var pkg listedPackage
		if err := dec.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode go list %s: %v\n%s", pattern, err, out)
		}
		pkgs = append(pkgs, pkg)
	}
	if len(pkgs) == 0 {
		t.Fatalf("go list %s returned no packages", pattern)
	}
	return pkgs
}

func packageDir(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(file)
}
