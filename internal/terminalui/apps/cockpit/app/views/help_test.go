package views

import "testing"

func TestFallbackURLForHelpAction(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		note  string
		label string
		want  string
	}{
		{
			name:  "opened note shows fallback url",
			note:  "ask question opened; fallback https://x.com/ml_review",
			label: "ask question",
			want:  "https://x.com/ml_review",
		},
		{
			name:  "failed note for file issue",
			note:  "file issue open failed; fallback https://github.com/swobuforge/swobu/issues",
			label: "file issue",
			want:  "https://github.com/swobuforge/swobu/issues",
		},
		{
			name:  "note for other action ignored",
			note:  "ask question opened; fallback https://x.com/ml_review",
			label: "file issue",
			want:  "",
		},
		{
			name:  "unknown note grammar ignored",
			note:  "support link is missing",
			label: "ask question",
			want:  "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := fallbackURLForHelpAction(tc.note, tc.label)
			if got != tc.want {
				t.Fatalf("fallbackURLForHelpAction() = %q, want %q", got, tc.want)
			}
		})
	}
}
