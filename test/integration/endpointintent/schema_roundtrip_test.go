package endpointintent_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/metrofun/swobu/internal/adapters/outbound/persistence"
)

func TestEndpointIntentStore_RejectsUnsupportedSchemaVersion(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "endpoints.json")
	if err := os.WriteFile(path, []byte("{\n  \"version\": 99,\n  \"endpoints\": []\n}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	store, err := persistence.NewEndpointIntentStore(persistence.EndpointIntentStoreConfig{Path: path})
	if err != nil {
		t.Fatalf("NewEndpointIntentStore returned error: %v", err)
	}

	_, err = store.ListEndpoints(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported endpoint intent schema version") {
		t.Fatalf("error = %v, want unsupported schema version", err)
	}
}

func TestEndpointIntentStore_RejectsInvalidPersistedEndpointShape(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "endpoints.json")
	raw := `{
  "version": 1,
  "endpoints": [
    {
      "name": "alpha",
      "selected_provider_config_ref": "config-missing",
      "provider_configs": [
        {
          "ref": "config-a",
          "provider_spec": "openai",
          "protocol_kind": "chat_completions"
        },
        {
          "ref": "config-b",
          "provider_spec": "openai",
          "protocol_kind": "chat_completions"
        }
      ]
    }
  ]
}
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	store, err := persistence.NewEndpointIntentStore(persistence.EndpointIntentStoreConfig{Path: path})
	if err != nil {
		t.Fatalf("NewEndpointIntentStore returned error: %v", err)
	}

	_, err = store.ListEndpoints(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "selected provider config must resolve to one provider config") {
		t.Fatalf("error = %v, want selected-provider-config invariant failure", err)
	}
}
