package profile

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBedrockMantleRegions_AreCanonicalAndCurrent(t *testing.T) {
	t.Parallel()

	regions := BedrockMantleRegions()
	if len(regions) != 13 {
		t.Fatalf("region count = %d, want 13", len(regions))
	}
	if SupportsBedrockMantleRegion("ca-central-1") {
		t.Fatal("ca-central-1 should not be listed as a supported Mantle region")
	}
	if !SupportsBedrockMantleRegion("eu-west-2") {
		t.Fatal("eu-west-2 should be listed as a supported Mantle region")
	}
	if got := BedrockMantleEndpointForRegion("eu-west-2"); got != "https://bedrock-mantle.eu-west-2.api.aws/v1" {
		t.Fatalf("derived endpoint = %q", got)
	}
	if got := BedrockMantleRegionFromEndpoint("https://bedrock-mantle.eu-west-2.api.aws/v1"); got != "eu-west-2" {
		t.Fatalf("derived region = %q", got)
	}
	if got := BedrockMantleRegionFromEndpoint("https://example.test/v1"); got != "" {
		t.Fatalf("non-mantle endpoint region = %q, want empty", got)
	}
}

func TestBedrockProfileCredentialRefHelpers(t *testing.T) {
	t.Parallel()

	if got := BedrockProfileNameFromCredentialRef("profile:work-prod@eu-west-2"); got != "work-prod" {
		t.Fatalf("profile name from credential ref = %q, want work-prod", got)
	}
	if got := BedrockProfileNameFromCredentialRef("work-prod"); got != "" {
		t.Fatalf("strict profile parser should reject bare name, got %q", got)
	}
	if got := BedrockProfileNameFromInput("profile:work-prod@eu-west-2"); got != "work-prod" {
		t.Fatalf("profile name from input = %q, want work-prod", got)
	}
	if got := BedrockProfileNameFromInput("work-prod@eu-west-2"); got != "work-prod" {
		t.Fatalf("profile name from bare input = %q, want work-prod", got)
	}
	if got := BedrockProfileCredentialRef("work-prod"); got != "profile:work-prod" {
		t.Fatalf("profile credential ref = %q, want profile:work-prod", got)
	}
}

func TestAWSSharedConfigProfileNamesMatchesAWSListProfilesShape(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config")
	credentialsPath := filepath.Join(tmp, "credentials")
	if err := os.WriteFile(configPath, []byte(`
[profile zed]
region = us-east-1
[profile alpha]
region = us-east-1
[default]
region = us-east-1
[sso-session ignored]
sso_region = us-east-1
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(credentialsPath, []byte(`
[beta]
aws_access_key_id = test
aws_secret_access_key = test
[alpha]
aws_access_key_id = override
aws_secret_access_key = override
`), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	t.Setenv(awsConfigFileEnv, configPath)
	t.Setenv(awsSharedCredentialsFileEnv, credentialsPath)

	got := AWSSharedConfigProfileNames()
	want := []string{"zed", "alpha", "default", "beta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AWSSharedConfigProfileNames = %#v, want %#v", got, want)
	}
}
