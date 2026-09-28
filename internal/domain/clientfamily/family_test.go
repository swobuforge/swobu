package clientfamily

import "testing"

func TestClassifyBoundsClientProductWithoutVersion(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want Family
	}{
		{"Codex/1.2.3 platform/foo", Codex},
		{"Claude-Code/2.0", ClaudeCode},
		{"Cline/1", Cline},
		{"OpenCode/1", OpenCode},
		{"Aider/1", Aider},
		{"swobu-vscode/0.4.7", VSCode},
		{"private-client/secret-version", Other},
		{"", Unknown},
	} {
		if got := Classify(test.raw); got != test.want || !got.Valid() {
			t.Errorf("Classify(%q) = %q, want %q", test.raw, got, test.want)
		}
	}
}
