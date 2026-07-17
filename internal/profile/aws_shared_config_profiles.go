package profile

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

const (
	awsConfigFileEnv            = "AWS_CONFIG_FILE"
	awsSharedCredentialsFileEnv = "AWS_SHARED_CREDENTIALS_FILE"
)

// AWSSharedConfigProfileNames returns profile names in the same shape operators
// see from `aws configure list-profiles`: config sections named [profile x]
// become x, [default] stays default, and credentials-file sections are bare
// profile names.
func AWSSharedConfigProfileNames() []string {
	paths := awsSharedConfigProfileFiles()
	seen := make(map[string]struct{})
	profiles := make([]string, 0)
	for i, path := range paths {
		for _, name := range awsProfileNamesFromFile(path, i == 0) {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			profiles = append(profiles, name)
		}
	}
	return profiles
}

func awsSharedConfigProfileFiles() []string {
	configPath := strings.TrimSpace(os.Getenv(awsConfigFileEnv))
	credentialsPath := strings.TrimSpace(os.Getenv(awsSharedCredentialsFileEnv))
	if configPath == "" || credentialsPath == "" {
		home := userHomeDir()
		if home != "" {
			if configPath == "" {
				configPath = filepath.Join(home, ".aws", "config")
			}
			if credentialsPath == "" {
				credentialsPath = filepath.Join(home, ".aws", "credentials")
			}
		}
	}
	return []string{configPath, credentialsPath}
}

func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(home)
}

func awsProfileNamesFromFile(path string, configFile bool) []string {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	seen := make(map[string]struct{})
	out := make([]string, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		name := awsProfileNameFromSectionHeader(scanner.Text(), configFile)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func awsProfileNameFromSectionHeader(line string, configFile bool) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "[") || !strings.Contains(trimmed, "]") {
		return ""
	}
	section, _, _ := strings.Cut(strings.TrimPrefix(trimmed, "["), "]")
	section = strings.TrimSpace(section)
	if section == "" {
		return ""
	}
	if !configFile {
		return section
	}
	if section == "default" {
		return section
	}
	if name, ok := strings.CutPrefix(section, "profile "); ok {
		return strings.TrimSpace(name)
	}
	return ""
}
