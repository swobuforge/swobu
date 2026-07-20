package provider

import (
	"testing"
	"time"
)

func TestDefaultImageFetchHasBoundedTotalPreparationTime(t *testing.T) {
	policy := DefaultImageFetchPolicy()
	network, enabled := policy.NetworkPolicy()
	if !enabled || policy.TotalPreparationTimeout() <= 0 || policy.TotalPreparationTimeout() >= time.Duration(DefaultMediaLimits().MaxImages)*network.PerImageTimeout {
		t.Fatalf("image preparation timeout is not bounded below sequential per-image worst case: %#v", policy)
	}
}

func TestMediaLimitsRejectZeroAsImplicitInheritance(t *testing.T) {
	if err := (MediaLimits{}).Validate(); err == nil {
		t.Fatal("zero media limits were accepted as implicit defaults")
	}
}

func TestImageFetchPolicyRejectsUnreachableNetworkConfiguration(t *testing.T) {
	disabled := DisabledImageFetchPolicy()
	if _, enabled := disabled.NetworkPolicy(); enabled {
		t.Fatal("disabled materialization retained network configuration")
	}
	pattern, _ := NewHostPattern("example.test")
	if _, err := NewImageFetchPolicy(NetworkPolicy{Access: NetworkPublicOnly, AllowedHosts: []HostPattern{pattern}, MaxRedirects: 3, PerImageTimeout: time.Second}, time.Minute); err == nil {
		t.Fatal("public-only policy accepted ignored allowlist")
	}
}

func TestHostPatternRejectsMalformedWildcards(t *testing.T) {
	for _, raw := range []string{"", "foo.*.test", "*", "https://example.test"} {
		if _, err := NewHostPattern(raw); err == nil {
			t.Fatalf("host pattern %q accepted", raw)
		}
	}
}
