package profile

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestBedrockMantleRegions_AreCanonicalAndCurrent(t *testing.T) {
	t.Parallel()
	regions := BedrockMantleRegions()
	if len(regions) != 13 {
		t.Fatalf("region count = %d, want 13", len(regions))
	}
	if SupportsBedrockMantleRegion("ca-central-1") {
		t.Fatal("ca-central-1 should not be supported")
	}
	if !SupportsBedrockMantleRegion("eu-west-2") {
		t.Fatal("eu-west-2 should be supported")
	}
	if got := BedrockMantleRegionLabel("eu-west-2"); got != "Europe (London) · eu-west-2" {
		t.Fatalf("region label = %q", got)
	}
	if got := EffectiveBedrockAPIURL("eu-west-2", "", protocolkind.Responses); got != "https://bedrock-mantle.eu-west-2.api.aws/v1" {
		t.Fatalf("derived endpoint = %q", got)
	}
	if got := BedrockMantleRegionFromEndpoint("https://bedrock-mantle.eu-west-2.api.aws/v1"); got != "eu-west-2" {
		t.Fatalf("derived region = %q", got)
	}
}
