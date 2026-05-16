package providercatalog

import "testing"

func TestTransitionalProtocolSeam_IntentionalAndConstrained(t *testing.T) {
	t.Parallel()

	for _, spec := range SupportedSpecs() {
		modes := AllowedAuthModesForSpec(spec)
		if len(modes) == 0 {
			t.Fatalf("provider %q must declare at least one allowed auth mode", spec)
		}
	}

	// Provider catalog must remain product/config truth only.
	if !SupportsSpec("chatgpt") {
		t.Fatal("chatgpt provider must remain declared")
	}
}
