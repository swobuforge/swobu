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

// TestTerminalUIPackageBoundaries enforces import isolation for the
// remaining noninteractive terminalui residue.
//
// Active interactive cockpit lives in internal/cockpit (go-tui). The
// terminalui subtree is retained only for:
//   - CLI startup presentation (internal/terminalui/apps/cli)
//   - Session/mode plumbing (internal/terminalui/session)
//   - Transcript output primitives (internal/terminalui/transcript)
//
// All legacy interactive packages (apps/cockpit, engine/retained, core,
// components, component, toolkit, view/retained) have been deleted and must
// not be imported.
func TestTerminalUIPackageBoundaries(t *testing.T) {
	root := packageDir(t)

	deletedPackages := []string{
		"/internal/terminalui/apps/cockpit/",
		"/internal/terminalui/engine/retained/",
		"/internal/terminalui/core/",
		"/internal/terminalui/component/",
		"/internal/terminalui/components/",
		"/internal/terminalui/toolkit/",
		"/internal/terminalui/view/retained/",
	}

	for _, pattern := range []string{
		"./session/...",
		"./transcript/...",
		"./apps/cli/...",
		"./engine/output/...",
		"./engine/reconcile/...",
		"./view/layout/...",
	} {
		pkgs := goListPackages(t, root, pattern)
		for _, pkg := range pkgs {
			for _, imp := range pkg.Imports {
				for _, deleted := range deletedPackages {
					if strings.Contains(imp, deleted) {
						t.Fatalf("%s imports deleted package %q", pkg.ImportPath, imp)
					}
				}
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
