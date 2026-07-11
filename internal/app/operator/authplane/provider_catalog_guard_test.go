package authplane

import (
	"testing"

	"github.com/swobuforge/swobu/internal/profile"
)

func TestAuthplaneProviderSpecs_AreDeclaredInProviderCatalog(t *testing.T) {
	t.Parallel()
	for _, spec := range []string{ChatGPTProviderSpec} {
		if !profile.SupportsSpec(spec) {
			t.Fatalf("authplane provider spec %q must be declared in providercatalog", spec)
		}
	}
}
