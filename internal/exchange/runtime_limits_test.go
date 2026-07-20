package exchange

import "testing"

func TestDefaultRuntimeLimitsSeparateTransportMediaAndCheckpointOwnership(t *testing.T) {
	limits := DefaultRuntimeLimits()
	if err := limits.Validate(); err != nil {
		t.Fatal(err)
	}
	if limits.Media.MaxTotalImageBytes >= limits.MaxCheckpointBytes {
		t.Fatalf("image aggregate %d leaves no checkpoint structural budget below %d", limits.Media.MaxTotalImageBytes, limits.MaxCheckpointBytes)
	}
}

func TestRuntimeLimitsRejectZeroAsImplicitInheritance(t *testing.T) {
	if err := (RuntimeLimits{}).Validate(); err == nil {
		t.Fatal("zero runtime limits were accepted as implicit defaults")
	}
}
