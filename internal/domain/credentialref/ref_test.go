package credentialref

import "testing"

func TestParseKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want Kind
	}{
		{"", KindEmpty},
		{"keychain:openai/default", KindKeychain},
		{"env:OPENAI_API_KEY", KindEnv},
		{"file:/tmp/token", KindFile},
		{"/tmp/token", KindFile},
		{"~/token", KindFile},
		{"abc", KindOther},
	}
	for _, tt := range tests {
		if got := Parse(tt.in).Kind(); got != tt.want {
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
		if got := Parse(tt.in).IsEmptyFileSelection(); got != tt.want {
			t.Fatalf("Parse(%q).IsEmptyFileSelection()=%v want=%v", tt.in, got, tt.want)
		}
	}
}
