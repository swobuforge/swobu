package provider

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestTargetSupportDistinguishesUnknownSupportedAndUnsupported(t *testing.T) {
	support := NewTargetSupport(map[canonical.CapabilityPath]Support{
		canonical.RequestToolsDiscovery: SupportSupported,
		canonical.RequestReasoning:      SupportUnsupported,
	})
	if got := support.Get(canonical.RequestToolsDiscovery); got != SupportSupported {
		t.Fatalf("tool support = %v, want supported", got)
	}
	if got := support.Get(canonical.RequestReasoning); got != SupportUnsupported {
		t.Fatalf("reasoning support = %v, want unsupported", got)
	}
	if got := support.Get(canonical.RequestOutputFormat); got != SupportUnknown {
		t.Fatalf("absent support = %v, want unknown", got)
	}
}

func TestTargetSupportCopiesInputEvidence(t *testing.T) {
	values := map[canonical.CapabilityPath]Support{canonical.RequestToolsDiscovery: SupportSupported}
	support := NewTargetSupport(values)
	values[canonical.RequestToolsDiscovery] = SupportUnsupported
	if got := support.Get(canonical.RequestToolsDiscovery); got != SupportSupported {
		t.Fatalf("support changed with source map: %v", got)
	}
}

func TestTargetSupportTreatsInvalidStateAsUnknown(t *testing.T) {
	support := NewTargetSupport(map[canonical.CapabilityPath]Support{
		canonical.RequestToolsDiscovery: Support(99),
	})
	if got := support.Get(canonical.RequestToolsDiscovery); got != SupportUnknown {
		t.Fatalf("invalid support = %v, want unknown", got)
	}
}
