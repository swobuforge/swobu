package model

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

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

	got := BedrockDiscoveredAWSProfiles()
	want := []string{"swobu-bedrock"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BedrockDiscoveredAWSProfiles=%v want %v", got, want)
	}
}

func TestProviderModelCatalogLoadBlocked_BedrockAWSProfileAvailability(t *testing.T) {
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

	if ProviderModelCatalogLoadBlocked("bedrock", "https://bedrock-mantle.us-east-1.api.aws/v1", "profile:swobu-bedrock") {
		t.Fatal("expected active Bedrock AWS profile to allow model catalog load")
	}
	if !ProviderModelCatalogLoadBlocked("bedrock", "https://bedrock-mantle.us-east-1.api.aws/v1", "profile:home-bedrock") {
		t.Fatal("expected stale home profile to block model catalog load")
	}
	if !ProviderModelCatalogLoadBlocked("bedrock", "https://bedrock-mantle.us-east-1.api.aws/v1", "aws_profile") {
		t.Fatal("expected aws_profile sentinel to block model catalog load until an explicit profile is chosen")
	}
}
