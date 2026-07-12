package bedrock

import "github.com/swobuforge/swobu/internal/domain/canonical"

func validateBedrockMantleEndpoint(baseURL string) error {
	class, _ := bedrockEndpointClassAndRegion(baseURL)
	if class == "bedrock_mantle_openai_compat" {
		return nil
	}
	host := trimBedrockInput(mustParseURL(baseURL).Hostname())
	if host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "example.test" {
		return nil
	}
	return canonical.BadEndpoint("bedrock provider requires a Bedrock Mantle endpoint host (bedrock-mantle.<region>.api.aws/v1)")
}
