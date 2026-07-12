package routing

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
)

func bedrockDefaultProfileFromEnvOrList(profiles []string) string {
	if fromEnv := trimRoutingInput(platformconfig.ReadEnvTrim("AWS_PROFILE")); fromEnv != "" {
		return fromEnv
	}
	if len(profiles) == 0 {
		return ""
	}
	return trimRoutingInput(profiles[0])
}

func TestBedrockResolvedRegion_ExplicitRegionTakesPrecedence(t *testing.T) {
	t.Setenv("AWS_REGION", "eu-west-1")
	t.Setenv("AWS_DEFAULT_REGION", "eu-west-2")

	got := bedrockResolvedRegion("ap-south-1", "https://bedrock-mantle.us-east-1.api.aws/v1")
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

func TestBedrockCredentialRefHelpers_AWSChainRefs(t *testing.T) {
	if !isBedrockAWSProfileCredentialRef("aws_env_session") {
		t.Fatal("aws_env_session should be treated as AWS chain credential ref")
	}
	if !isBedrockAWSProfileCredentialRef("profile:work-prod") {
		t.Fatal("profile:work-prod should be treated as AWS chain credential ref")
	}
	if !isBedrockAWSProfileCredentialRef("aws_profile") {
		t.Fatal("aws_profile should be treated as AWS chain credential ref")
	}
}

func TestBedrockDefaultProfileFromEnvOrList_EnvWins(t *testing.T) {
	t.Setenv("AWS_PROFILE", "from-env")
	got := bedrockDefaultProfileFromEnvOrList([]string{"first", "second"})
	if got != "from-env" {
		t.Fatalf("bedrockDefaultProfileFromEnvOrList=%q want from-env", got)
	}
}

func TestBedrockDiscoveredAWSProfiles_DedupesAndKeepsOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", "")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "")
	awsDir := filepath.Join(home, ".aws")
	if err := os.MkdirAll(awsDir, 0o755); err != nil {
		t.Fatalf("mkdir .aws: %v", err)
	}
	configRaw := `
[default]
[profile prod]
[profile dev]
[profile prod]
`
	credentialsRaw := `
[default]
[qa]
[dev]
`
	if err := os.WriteFile(filepath.Join(awsDir, "config"), []byte(configRaw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(awsDir, "credentials"), []byte(credentialsRaw), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	got := stateModel.BedrockDiscoveredAWSProfiles()
	want := []string{"default", "prod", "dev", "qa"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stateModel.BedrockDiscoveredAWSProfiles=%v want %v", got, want)
	}
}

func TestBedrockDiscoveredAWSProfiles_UsesActiveAWSFiles(t *testing.T) {
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", "")
	homeAWS := filepath.Join(home, ".aws")
	if err := os.MkdirAll(homeAWS, 0o755); err != nil {
		t.Fatalf("mkdir home .aws: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeAWS, "config"), []byte("[profile home-bedrock]\nregion = us-west-2\n"), 0o600); err != nil {
		t.Fatalf("write home config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeAWS, "credentials"), []byte("[home-bedrock]\naws_access_key_id = home\naws_secret_access_key = home\n"), 0o600); err != nil {
		t.Fatalf("write home credentials: %v", err)
	}

	active := t.TempDir()
	activeConfig := filepath.Join(active, "config")
	if err := os.WriteFile(activeConfig, []byte("[profile swobu-bedrock]\nregion = us-east-1\n"), 0o600); err != nil {
		t.Fatalf("write active config: %v", err)
	}
	activeCreds := filepath.Join(active, "credentials")
	if err := os.WriteFile(activeCreds, []byte("[swobu-bedrock]\naws_access_key_id = active\naws_secret_access_key = active\n"), 0o600); err != nil {
		t.Fatalf("write active credentials: %v", err)
	}
	t.Setenv("AWS_CONFIG_FILE", activeConfig)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", activeCreds)

	got := stateModel.BedrockDiscoveredAWSProfiles()
	want := []string{"swobu-bedrock"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stateModel.BedrockDiscoveredAWSProfiles=%v want %v", got, want)
	}
}

func TestBedrockDiscoveredAWSProfiles_NoAWSDir_ReturnsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", "")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "")

	got := stateModel.BedrockDiscoveredAWSProfiles()
	if len(got) != 0 {
		t.Fatalf("stateModel.BedrockDiscoveredAWSProfiles=%v want empty", got)
	}
}

func TestBedrockDiscoveredAWSProfiles_EmptyAndInvalidFiles_ReturnsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", "")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "")
	awsDir := filepath.Join(home, ".aws")
	if err := os.MkdirAll(awsDir, 0o755); err != nil {
		t.Fatalf("mkdir .aws: %v", err)
	}
	if err := os.WriteFile(filepath.Join(awsDir, "config"), []byte("not a section\nfoo=bar\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(awsDir, "credentials"), []byte(""), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}

	got := stateModel.BedrockDiscoveredAWSProfiles()
	if len(got) != 0 {
		t.Fatalf("stateModel.BedrockDiscoveredAWSProfiles=%v want empty", got)
	}
}

func TestBedrockDefaultProfileFromEnvOrList_NoEnvNoProfiles_ReturnsEmpty(t *testing.T) {
	t.Setenv("AWS_PROFILE", "")
	got := bedrockDefaultProfileFromEnvOrList(nil)
	if got != "" {
		t.Fatalf("bedrockDefaultProfileFromEnvOrList=%q want empty", got)
	}
}

func TestEncodeBedrockProfileCredentialRef_EmptyMeansAWSChainDefault(t *testing.T) {
	got := encodeBedrockProfileCredentialRef("")
	if got != "aws_profile" {
		t.Fatalf("encodeBedrockProfileCredentialRef(empty)=%q want aws_profile", got)
	}
}

func TestBedrockProfileSummary_EmptyShowsAuto(t *testing.T) {
	got := bedrockProfileSummary("")
	if got != "auto" {
		t.Fatalf("bedrockProfileSummary(empty)=%q", got)
	}
}
