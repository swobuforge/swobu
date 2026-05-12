package authplane

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

func TestAuthplaneProviderSpecs_AreDeclaredInProviderCatalog(t *testing.T) {
	t.Parallel()
	for _, spec := range []string{ChatGPTProviderSpec} {
		if !providercatalog.SupportsSpec(spec) {
			t.Fatalf("authplane provider spec %q must be declared in providercatalog", spec)
		}
	}
}
