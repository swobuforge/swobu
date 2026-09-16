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

func TestOperatorProviderOptionsKeepOpenAIChoicesAdjacent(t *testing.T) {
	options := operatorProviderOptions()
	for index, option := range options {
		if option.ProviderSpec != "openai" {
			continue
		}
		if index+1 >= len(options) || options[index+1].ProviderSpec != "chatgpt" {
			t.Fatalf("openai options are not adjacent: %#v", options)
		}
		if option.DisplayName != "OpenAI API" || options[index+1].DisplayName != "OpenAI · ChatGPT subscription" {
			t.Fatalf("OpenAI labels = %q, %q", option.DisplayName, options[index+1].DisplayName)
		}
		return
	}
	t.Fatal("openai provider option missing")
}

func TestOperatorProviderOptionsIncludeQualifiedVercel(t *testing.T) {
	for _, option := range operatorProviderOptions() {
		if option.ProviderSpec == "vercel" {
			if option.DisplayName != "Vercel AI Gateway" {
				t.Fatalf("Vercel option = %#v", option)
			}
			return
		}
	}
	t.Fatal("qualified Vercel option missing")
}
