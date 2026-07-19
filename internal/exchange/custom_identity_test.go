package exchange

import (
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestProviderTargetFromCustomConnectionPreservesProviderIdentity(t *testing.T) {
	connection, err := routing.NewCustomConnection("https://example.test/v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, protocol := range []string{"responses", "messages"} {
		t.Run(protocol, func(t *testing.T) {
			target, err := ProviderTargetFromConnection("custom-a", connection, protocol)
			if err != nil {
				t.Fatal(err)
			}
			if target.ProviderSpec != "custom" {
				t.Fatalf("provider spec = %q, want custom", target.ProviderSpec)
			}
		})
	}
}

func TestProviderTargetProjectionCarriesRoutingIdentityAndVersion(t *testing.T) {
	target := requestpathTarget(t, "custom-a")
	snapshot, err := toProviderTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TargetID != target.ID().String() || snapshot.TargetVersion != uint64(target.Version()) {
		t.Fatalf("target snapshot = %#v", snapshot)
	}
}
