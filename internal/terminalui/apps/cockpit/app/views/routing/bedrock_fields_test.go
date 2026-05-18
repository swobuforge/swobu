package routing

import "testing"

func TestBedrockResolvedRegion_ExplicitRegionTakesPrecedence(t *testing.T) {
	t.Setenv("AWS_REGION", "eu-west-1")
	t.Setenv("AWS_DEFAULT_REGION", "eu-west-2")

	got := bedrockResolvedRegion("ap-south-1", "https://bedrock-runtime.us-east-1.amazonaws.com/openai/v1")
	if got != "ap-south-1" {
		t.Fatalf("bedrockResolvedRegion=%q want ap-south-1", got)
	}
}

func TestBedrockResolvedRegion_UsesAWSRegionWhenURLMissing(t *testing.T) {
	t.Setenv("AWS_REGION", "eu-west-1")
	t.Setenv("AWS_DEFAULT_REGION", "eu-west-2")

	got := bedrockResolvedRegion("", "")
	if got != "eu-west-1" {
		t.Fatalf("bedrockResolvedRegion=%q want eu-west-1", got)
	}
}

func TestBedrockResolvedRegion_FallsBackToAWSDefaultRegion(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "eu-west-2")

	got := bedrockResolvedRegion("", "")
	if got != "eu-west-2" {
		t.Fatalf("bedrockResolvedRegion=%q want eu-west-2", got)
	}
}
