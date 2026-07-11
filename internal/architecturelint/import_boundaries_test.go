package architecturelint

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestImportBoundaries_CanonicalPackage(t *testing.T) {
	t.Parallel()
	assertPackageImportsNotPrefixed(t, packageDirFromHere(t, "..", "domain", "canonical"), []string{
		"github.com/swobuforge/swobu/internal/adapters",
		"github.com/swobuforge/swobu/internal/platform/http",
		"github.com/swobuforge/swobu/internal/platform/httpcontent",
		"github.com/swobuforge/swobu/internal/profile",
		"net/http",
	})
}

func TestImportBoundaries_RequestpathPackage(t *testing.T) {
	t.Parallel()
	assertPackageImportsNotPrefixed(t, packageDirFromHere(t, "..", "app", "requestpath"), []string{
		"github.com/swobuforge/swobu/internal/adapters/outbound/providers",
		"github.com/swobuforge/swobu/internal/adapters/outbound/providers/anthropic",
		"github.com/swobuforge/swobu/internal/adapters/outbound/providers/bedrocknative",
		"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily",
		"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaireal",
	})
}

func TestImportBoundaries_CarrierPackage(t *testing.T) {
	t.Parallel()
	assertPackageImportsNotPrefixed(t, packageDirFromHere(t, "..", "carrier"), []string{
		"github.com/swobuforge/swobu/internal/adapters",
		"github.com/swobuforge/swobu/internal/app",
		"github.com/swobuforge/swobu/internal/ports",
		"github.com/swobuforge/swobu/internal/adapters/outbound/providers",
		"github.com/swobuforge/swobu/internal/adapters/wire/families",
	})
}

func TestImportBoundaries_ExchangePackage(t *testing.T) {
	t.Parallel()
	assertPackageImportsNotPrefixed(t, packageDirFromHere(t, "..", "exchange"), []string{
		"github.com/swobuforge/swobu/internal/adapters/outbound/providers/anthropic",
		"github.com/swobuforge/swobu/internal/adapters/outbound/providers/bedrock",
		"github.com/swobuforge/swobu/internal/adapters/outbound/providers/chatgpt",
		"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily",
		"github.com/swobuforge/swobu/internal/adapters/wire/families",
		"github.com/swobuforge/swobu/internal/adapters/wire/families/chatcompletions",
		"github.com/swobuforge/swobu/internal/adapters/wire/families/completions",
		"github.com/swobuforge/swobu/internal/adapters/wire/families/messages",
		"github.com/swobuforge/swobu/internal/adapters/wire/families/responses",
	})
}

func assertPackageImportsNotPrefixed(t *testing.T, pkgDir string, forbiddenPrefixes []string) {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join(pkgDir, "*.go"))
	if err != nil {
		t.Fatalf("glob package files: %v", err)
	}
	fset := token.NewFileSet()
	for _, path := range entries {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("parse imports %s: %v", path, parseErr)
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			for _, forbidden := range forbiddenPrefixes {
				if strings.HasPrefix(importPath, forbidden) {
					t.Fatalf("forbidden import %q in %s (blocked prefix %q)", importPath, path, forbidden)
				}
			}
		}
	}
}

func packageDirFromHere(t *testing.T, rel ...string) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	base := filepath.Dir(filename)
	parts := append([]string{base}, rel...)
	return filepath.Clean(filepath.Join(parts...))
}
