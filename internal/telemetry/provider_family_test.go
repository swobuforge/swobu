package telemetry

import "testing"

func TestNormalizeProviderFamily(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "known spec", in: "openai", want: "openai"},
		{name: "known spec with route suffix", in: "openai:gpt-4.1-mini", want: "openai"},
		{name: "trim and lowercase", in: "  Anthropic:claude  ", want: "anthropic"},
		{name: "unknown collapses", in: "corp-internal:foo", want: providerFamilyOther},
		{name: "empty collapses", in: "", want: providerFamilyOther},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeProviderFamily(tc.in); got != tc.want {
				t.Fatalf("normalizeProviderFamily(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
