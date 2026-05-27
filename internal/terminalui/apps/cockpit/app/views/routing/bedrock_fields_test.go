package routing

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

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

func TestParseAWSINIProfiles_ConfigAndCredentials_AreStrict(t *testing.T) {
	configRaw := `
# comment
; comment
[default]
[profile prod]
[profile dev]
[profile   qa]
[profile ]
[not-a-profile]
`
	credentialsRaw := `
[default]
[prod]
[dev]
[ ]
`
	gotConfig := parseAWSINIProfiles(configRaw, true)
	wantConfig := []string{"default", "prod", "dev", "qa"}
	if !reflect.DeepEqual(gotConfig, wantConfig) {
		t.Fatalf("parseAWSINIProfiles(config)=%v want %v", gotConfig, wantConfig)
	}
	gotCreds := parseAWSINIProfiles(credentialsRaw, false)
	wantCreds := []string{"default", "prod", "dev"}
	if !reflect.DeepEqual(gotCreds, wantCreds) {
		t.Fatalf("parseAWSINIProfiles(credentials)=%v want %v", gotCreds, wantCreds)
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
	got := bedrockDiscoveredAWSProfiles()
	want := []string{"default", "prod", "dev", "qa"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bedrockDiscoveredAWSProfiles=%v want %v", got, want)
	}
}

func TestBedrockDiscoveredAWSProfiles_NoAWSDir_ReturnsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", "")

	got := bedrockDiscoveredAWSProfiles()
	if len(got) != 0 {
		t.Fatalf("bedrockDiscoveredAWSProfiles=%v want empty", got)
	}
}

func TestBedrockDiscoveredAWSProfiles_EmptyAndInvalidFiles_ReturnsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", "")
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

	got := bedrockDiscoveredAWSProfiles()
	if len(got) != 0 {
		t.Fatalf("bedrockDiscoveredAWSProfiles=%v want empty", got)
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
