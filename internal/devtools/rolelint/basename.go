package rolelint

import (
	"path/filepath"
	"slices"
	"strings"
)

// compoundWeakStems lists weak stems that commonly appear embedded in
// filenames without a separator (e.g. textutil.go, strutils.go).
// Other weak stems only match with an explicit underscore prefix.
var compoundWeakStems = map[string]struct{}{
	"util":  {},
	"utils": {},
}

func isWeakGoBasename(base string) bool {
	base = strings.TrimSpace(base)
	if base == "" {
		return false
	}
	if slices.Contains(weakGoBasenames, base) {
		return true
	}
	ext := filepath.Ext(base)
	if ext != ".go" {
		return false
	}
	name := strings.TrimSuffix(base, ext)
	for _, weak := range weakGoBasenames {
		stem := strings.TrimSuffix(weak, ".go")
		if stem == "" {
			continue
		}
		// Catch suffixed variants with separator: e.g. foo_helpers.go
		if strings.HasSuffix(name, "_"+stem) {
			return true
		}
		// Catch compound forms without separator for stems that commonly
		// embed: e.g. textutil.go, strutils.go.
		if _, compound := compoundWeakStems[stem]; compound && len(name) > len(stem) && strings.HasSuffix(name, stem) {
			return true
		}
	}
	return false
}
