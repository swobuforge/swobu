package exchange

import (
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestRoutableTargetFromCustomConnectionPreservesProviderIdentity(t *testing.T) {
	connection, err := routing.NewCustomConnection("https://example.test/v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, protocol := range []string{"responses", "messages"} {
		t.Run(protocol, func(t *testing.T) {
			target, err := RoutableTargetFromConnection("custom-a", connection, protocol)
			if err != nil {
				t.Fatal(err)
			}
			if target.ProviderSpec != "custom" {
				t.Fatalf("provider spec = %q, want custom", target.ProviderSpec)
			}
		})
	}
}
