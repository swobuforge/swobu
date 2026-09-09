package credentialref_test

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

func TestParseKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want credentialref.Kind
	}{
		{"", credentialref.KindEmpty},
		{"secret:openai/default", credentialref.KindSecret},
		{"env:OPENAI_API_KEY", credentialref.KindEnv},
		{"file:/tmp/token", credentialref.KindFile},
		{"/tmp/token", credentialref.KindFile},
		{"~/token", credentialref.KindFile},
		{"abc", credentialref.KindOther},
	}
	for _, tt := range tests {
		if got := credentialref.Parse(tt.in).Kind(); got != tt.want {
			t.Fatalf("Parse(%q).Kind()=%q want=%q", tt.in, got, tt.want)
		}
	}
}

func TestIsEmptyFileSelection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want bool
	}{
		{"file", true},
		{"file:", true},
		{"file:   ", true},
		{"file:/tmp/key", false},
		{"/tmp/key", false},
	}
	for _, tt := range tests {
		if got := credentialref.Parse(tt.in).IsEmptyFileSelection(); got != tt.want {
			t.Fatalf("Parse(%q).IsEmptyFileSelection()=%v want=%v", tt.in, got, tt.want)
		}
	}
}
