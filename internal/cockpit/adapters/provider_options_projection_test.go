package adapters

import (
	"slices"
	"sort"
	"strings"
	"testing"
)

func TestOperatorProviderOptionsAreAlphabeticalByDisplayName(t *testing.T) {
	options := operatorProviderOptions()
	got := make([]string, 0, len(options))
	for _, option := range options {
		got = append(got, option.DisplayName)
	}

	want := slices.Clone(got)
	sort.SliceStable(want, func(i, j int) bool {
		return strings.ToLower(want[i]) < strings.ToLower(want[j])
	})

	if !slices.Equal(got, want) {
		t.Fatalf("provider options = %q, want alphabetical order %q", got, want)
	}
}
