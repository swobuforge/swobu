package views

import "testing"

func TestShouldRenderCompatibilityRecoveryDetail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		label  string
		detail string
		want   bool
	}{
		{
			name:   "empty detail hidden",
			label:  "restart daemon",
			detail: "",
			want:   false,
		},
		{
			name:   "same text hidden",
			label:  "restart daemon",
			detail: "restart daemon",
			want:   false,
		},
		{
			name:   "case-insensitive same text hidden",
			label:  "restart daemon",
			detail: "Restart Daemon",
			want:   false,
		},
		{
			name:   "actual command shown",
			label:  "restart daemon",
			detail: "swobu daemon restart",
			want:   true,
		},
		{
			name:   "different guidance shown",
			label:  "restart daemon",
			detail: "restart daemon in terminal",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := shouldRenderCompatibilityRecoveryDetail(tt.label, tt.detail)
			if got != tt.want {
				t.Fatalf("shouldRenderCompatibilityRecoveryDetail(%q, %q)=%v want %v", tt.label, tt.detail, got, tt.want)
			}
		})
	}
}
