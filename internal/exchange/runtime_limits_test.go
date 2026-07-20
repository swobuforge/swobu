package exchange

import "testing"

func TestDefaultRuntimeLimitsSeparateTransportMediaAndReplayOwnership(t *testing.T) {
	limits := DefaultRuntimeLimits()
	if err := limits.Validate(); err != nil {
		t.Fatal(err)
	}
	if limits.Media.MaxTotalImageBytes >= limits.MaxReplayBytes {
		t.Fatalf("image aggregate %d leaves no replay structural budget below %d", limits.Media.MaxTotalImageBytes, limits.MaxReplayBytes)
	}
}

func TestRuntimeLimitsRejectZeroAsImplicitInheritance(t *testing.T) {
	if err := (RuntimeLimits{}).Validate(); err == nil {
		t.Fatal("zero runtime limits were accepted as implicit defaults")
	}
}
