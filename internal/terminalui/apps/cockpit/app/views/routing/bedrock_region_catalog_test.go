package routing

import "testing"

func TestBedrockRegions_IsNonEmpty(t *testing.T) {
	regions := bedrockRegions()
	if len(regions) == 0 {
		t.Fatal("bedrock regions empty")
	}
}
