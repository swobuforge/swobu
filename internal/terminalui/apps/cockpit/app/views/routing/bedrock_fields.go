package routing

import (
	"strings"

	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

const bedrockDefaultRegion = "us-east-1"

func bedrockRegionFromBaseURL(baseURL string) string {
	return trimRoutingInput(stateModel.BedrockRegionFromBaseURL(baseURL))
}

func bedrockRegionFromEnv() string {
	if region := trimRoutingInput(platformconfig.ReadEnvTrim("AWS_REGION")); region != "" { // swobu:io-string source=boundary
		return region
	}
	return trimRoutingInput(platformconfig.ReadEnvTrim("AWS_DEFAULT_REGION")) // swobu:io-string source=boundary
}

func bedrockResolvedRegion(region string, baseURL string) string {
	if region = trimRoutingInput(region); region != "" {
		return region
	}
	if region := trimRoutingInput(bedrockRegionFromBaseURL(baseURL)); region != "" {
		return region
	}
	return trimRoutingInput(bedrockRegionFromEnv())
}

func bedrockBaseURLForRegion(region string) string {
	region = trimRoutingInput(region)
	if region == "" {
		region = bedrockDefaultRegionFromList()
	}
	return stateModel.BedrockBaseURLForRegion(region)
}

func bedrockDefaultRegionFromList() string {
	regions := bedrockRegions()
	if len(regions) == 0 {
		return bedrockDefaultRegion
	}
	for _, region := range regions {
		if strings.EqualFold(trimRoutingInput(region), bedrockDefaultRegion) { // swobu:io-string source=boundary
			return bedrockDefaultRegion
		}
	}
	return trimRoutingInput(regions[0])
}

func bedrockProfileFromCredentialRef(ref string) string {
	ref = trimRoutingInput(ref)
	if !strings.HasPrefix(lowerRoutingInput(ref), "profile:") {
		return ""
	}
	return trimRoutingInput(ref[len("profile:"):])
}

func encodeBedrockProfileCredentialRef(profile string) string {
	profile = trimRoutingInput(profile)
	if profile == "" {
		return string("aws_profile")
	}
	return "profile:" + profile
}

func isBedrockAWSProfileCredentialRef(ref string) bool {
	trimmed := trimRoutingInput(ref)
	if trimmed == "" {
		return true
	}
	if strings.EqualFold(trimmed, "aws_profile") {
		return true
	}
	return strings.HasPrefix(lowerRoutingInput(trimmed), "profile:")
}

func trimRoutingInput(value string) string {
	return strings.TrimSpace(value) // swobu:io-string source=boundary
}

func lowerRoutingInput(value string) string {
	return strings.ToLower(value) // swobu:io-string source=boundary
}
