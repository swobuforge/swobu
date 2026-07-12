package routing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
)

func TestCredentialOptionItems_UsesStableKeys(t *testing.T) {
	items := credentialOptionItems("env", nil, "openai_compatible")
	if len(items) == 0 {
		t.Fatal("expected credential options")
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Key) == "" {
			t.Fatalf("item key empty for label %q", item.Label)
		}
		if _, ok := seen[item.Key]; ok {
			t.Fatalf("duplicate key %q", item.Key)
		}
		seen[item.Key] = struct{}{}
	}
}

func TestBedrockProfilePickerItems_UsesStableKeys(t *testing.T) {
	items := bedrockProfilePickerItems([]string{"  dev  ", "prod"}, "", nil)
	if len(items) != 3 {
		t.Fatalf("items len=%d want 3", len(items))
	}
	if items[0].Key != "auto" {
		t.Fatalf("first item key=%q want auto", items[0].Key)
	}
	for _, item := range items {
		if strings.TrimSpace(item.Key) == "" {
			t.Fatalf("item key empty for label %q", item.Label)
		}
	}
}

func TestPrimaryModelChoiceItems_UsesStableKeys(t *testing.T) {
	snapshot := &state.EndpointSnapshot{
		Name: "endpoint",
		ProviderConfigs: []state.ProviderConfigSnapshot{
			{Ref: "provider-a", ProviderSpec: "openai_compatible"},
			{Ref: "provider-b", ProviderSpec: "bedrock"},
		},
		SelectedProviderConfigRef: "provider-b",
	}
	items := primaryModelChoiceItems(snapshot, nil)
	if len(items) != 2 {
		t.Fatalf("items len=%d want 2", len(items))
	}
	if items[0].Key != "provider-a" || items[1].Key != "provider-b" {
		t.Fatalf("keys=%q,%q want provider refs", items[0].Key, items[1].Key)
	}
}

func TestCreateRunOnChoiceItems_UsesStableKey(t *testing.T) {
	model := state.Model{
		CreateDraftProviderConfig: state.ProviderConfigSnapshot{
			ProviderSpec: "bedrock",
		},
	}
	items := createRunOnChoiceItems(model, nil)
	if len(items) == 0 {
		t.Fatal("expected at least one run-on item")
	}
	if strings.TrimSpace(items[0].Key) == "" {
		t.Fatal("run-on item key must be stable")
	}
}

func TestCredentialFilePickerItems_UsesPathKeysAndFirstFocus(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "zdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	filePath := filepath.Join(dir, "afile.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	items, err := credentialFilePickerItems(credentialFileBrowseState{Dir: dir}, func(credentialFileBrowseState) {}, nil, "", nil)
	if err != nil {
		t.Fatalf("credentialFilePickerItems: %v", err)
	}
	if len(items) < 2 {
		t.Fatalf("items len=%d want >=2", len(items))
	}
	for _, item := range items {
		if strings.TrimSpace(item.Key) == "" {
			t.Fatalf("item key empty for label %q", item.Label)
		}
	}
	if got := credentialFilePickerFirstFocusKey(credentialFileBrowseState{Dir: dir}, ""); got == "" {
		t.Fatal("first focus key must not be empty")
	}
}
