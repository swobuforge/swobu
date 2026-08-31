package trafficevidence

import "testing"

func TestClassifyClientFamilyUsesOnlyThePrimaryProductToken(t *testing.T) {
	tests := []struct {
		raw  string
		want ClientFamily
	}{
		{"Codex/1.2 protocol/messages provider/anthropic", ClientFamilyCodex},
		{"Claude-Code/2.0 protocol/responses provider/openai", ClientFamilyClaudeCode},
		{"Cline/3", ClientFamilyCline},
		{"opencode/1.15", ClientFamilyOpenCode},
		{"Aider/0.82", ClientFamilyAider},
		{"NewClient/1", ClientFamilyOther},
		{"", ClientFamilyUnknown},
	}
	for _, test := range tests {
		if got := ClassifyClientFamily(test.raw); got != test.want {
			t.Fatalf("ClassifyClientFamily(%q) = %q, want %q", test.raw, got, test.want)
		}
	}
}
