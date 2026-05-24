package routing

import (
	"os"
	"path/filepath"
	"strings"

	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
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

func bedrockOpenAICompatibleBaseURLForRegion(region string) string {
	region = trimRoutingInput(region)
	if region == "" {
		region = bedrockDefaultRegionFromList()
	}
	return stateModel.BedrockOpenAICompatibleBaseURLForRegion(region)
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

func bedrockDiscoveredAWSProfiles() []string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return nil
	}
	configPath := filepath.Join(home, ".aws", "config")
	credentialsPath := filepath.Join(home, ".aws", "credentials")
	seen := map[string]bool{}
	out := make([]string, 0, 8)
	appendUnique := func(name string) {
		name = trimRoutingInput(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	if raw, err := os.ReadFile(configPath); err == nil {
		for _, profile := range parseAWSINIProfiles(string(raw), true) {
			appendUnique(profile)
		}
	}
	if raw, err := os.ReadFile(credentialsPath); err == nil {
		for _, profile := range parseAWSINIProfiles(string(raw), false) {
			appendUnique(profile)
		}
	}
	return out
}

func parseAWSINIProfiles(raw string, fromConfig bool) []string {
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, 8)
	for _, line := range lines {
		line = trimRoutingInput(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
			continue
		}
		section := trimRoutingInput(line[1 : len(line)-1])
		if section == "" {
			continue
		}
		if fromConfig {
			if strings.EqualFold(section, "default") {
				out = append(out, "default")
				continue
			}
			if strings.HasPrefix(strings.ToLower(section), "profile ") {
				out = append(out, trimRoutingInput(section[len("profile "):]))
			}
			continue
		}
		out = append(out, section)
	}
	return out
}

func bedrockDefaultProfileFromEnvOrList(profiles []string) string {
	if fromEnv := trimRoutingInput(platformconfig.ReadEnvTrim("AWS_PROFILE")); fromEnv != "" {
		return fromEnv
	}
	if len(profiles) == 0 {
		return ""
	}
	return trimRoutingInput(profiles[0])
}

func isBedrockAWSProfileCredentialRef(ref string) bool {
	trimmed := trimRoutingInput(ref)
	if trimmed == "" {
		return true
	}
	if strings.EqualFold(trimmed, "aws_profile") {
		return true
	}
	if strings.EqualFold(trimmed, "aws_env_session") {
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
