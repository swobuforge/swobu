package model

import (
	"os"
	"path/filepath"
	"strings"

	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

// BedrockDiscoveredAWSProfiles returns the AWS profile names visible to the
// active cockpit runtime.
//
// Runtime bundle files win over ~/.aws because live wrappers materialize the
// active AWS config into bundle-local shared files via AWS_CONFIG_FILE and
// AWS_SHARED_CREDENTIALS_FILE.
func BedrockDiscoveredAWSProfiles() []string {
	configPath, credentialsPath := bedrockSharedProfileFiles()
	seen := map[string]bool{}
	out := make([]string, 0, 8)
	appendUnique := func(name string) {
		name = strings.TrimSpace(name) // swobu:io-string source=boundary
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

func bedrockAWSProfileAvailable(profile string) bool {
	trimmed := strings.TrimSpace(profile) // swobu:io-string source=boundary
	if trimmed == "" {
		return false
	}
	for _, candidate := range BedrockDiscoveredAWSProfiles() {
		if strings.EqualFold(candidate, trimmed) {
			return true
		}
	}
	return false
}

func bedrockSharedProfileFiles() (configPath string, credentialsPath string) {
	configPath = strings.TrimSpace(platformconfig.ReadEnvTrim("AWS_CONFIG_FILE"))                  // swobu:io-string source=boundary
	credentialsPath = strings.TrimSpace(platformconfig.ReadEnvTrim("AWS_SHARED_CREDENTIALS_FILE")) // swobu:io-string source=boundary
	defaultConfigPath, defaultCredentialsPath := bedrockDefaultSharedProfileFiles()
	if configPath == "" {
		configPath = defaultConfigPath
	}
	if credentialsPath == "" {
		credentialsPath = defaultCredentialsPath
	}
	return configPath, credentialsPath
}

func bedrockDefaultSharedProfileFiles() (configPath string, credentialsPath string) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" { // swobu:io-string source=boundary
		return "", ""
	}
	configPath = filepath.Join(home, ".aws", "config")
	credentialsPath = filepath.Join(home, ".aws", "credentials")
	return configPath, credentialsPath
}

func parseAWSINIProfiles(raw string, fromConfig bool) []string {
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, 8)
	for _, line := range lines {
		line = strings.TrimSpace(line) // swobu:io-string source=boundary
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
			continue
		}
		section := strings.TrimSpace(line[1 : len(line)-1]) // swobu:io-string source=boundary
		if section == "" {
			continue
		}
		if fromConfig {
			if strings.EqualFold(section, "default") {
				out = append(out, "default")
				continue
			}
			if strings.HasPrefix(strings.ToLower(section), "profile ") { // swobu:io-string source=boundary
				out = append(out, strings.TrimSpace(section[len("profile "):]))
			}
			continue
		}
		out = append(out, section)
	}
	return out
}
