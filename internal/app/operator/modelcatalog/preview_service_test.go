package modelcatalog

import (
	"context"
	"fmt"
	"testing"

	"github.com/swobuforge/swobu/internal/ports"
)

type previewProviderCatalogStub struct {
	models []string
	err    error
}

func (s previewProviderCatalogStub) ListModels(ctx context.Context, target ports.RoutableTarget) ([]string, error) {
	_ = ctx
	_ = target
	if s.err != nil {
		return nil, s.err
	}
	return append([]string(nil), s.models...), nil
}

func TestPreviewLoader_Load_TranslatesFileCredentialResolutionError(t *testing.T) {
	loader := NewPreviewLoader(previewProviderCatalogStub{err: fmt.Errorf("BAD_ENDPOINT: credential reference could not be resolved")})
	snapshot, err := loader.Load(context.Background(), PreviewRequest{
		ProviderSpec:  "openrouter",
		BaseURL:       "https://openrouter.ai/api/v1",
		CredentialRef: "file:/tmp/openrouter.key",
		ProtocolKind:  "chat_completions",
	})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	want := "BAD_ENDPOINT: credential file could not be resolved (check file path, read permission, and non-empty token)"
	if snapshot.Error != want {
		t.Fatalf("error = %q, want %q", snapshot.Error, want)
	}
}

func TestPreviewLoader_Load_PreservesNonFileCredentialResolutionError(t *testing.T) {
	loader := NewPreviewLoader(previewProviderCatalogStub{err: fmt.Errorf("BAD_ENDPOINT: credential reference could not be resolved")})
	snapshot, err := loader.Load(context.Background(), PreviewRequest{
		ProviderSpec:  "openrouter",
		BaseURL:       "https://openrouter.ai/api/v1",
		CredentialRef: "env:OPENROUTER_API_KEY",
		ProtocolKind:  "chat_completions",
	})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	want := "BAD_ENDPOINT: credential reference could not be resolved"
	if snapshot.Error != want {
		t.Fatalf("error = %q, want %q", snapshot.Error, want)
	}
}

func TestPreviewLoader_Load_AllowsPrivateBaseURL(t *testing.T) {
	loader := NewPreviewLoader(previewProviderCatalogStub{models: []string{"m1"}})
	snapshot, err := loader.Load(context.Background(), PreviewRequest{
		ProviderSpec: "openrouter",
		BaseURL:      "http://10.1.2.3/v1",
		CredentialRef: "env:OPENROUTER_API_KEY",
		ProtocolKind: "chat_completions",
	})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if snapshot.Error != "" {
		t.Fatalf("error = %q, want empty", snapshot.Error)
	}
}

func TestPreviewLoader_Load_AllowsUnsupportedBaseURLScheme(t *testing.T) {
	loader := NewPreviewLoader(previewProviderCatalogStub{models: []string{"m1"}})
	snapshot, err := loader.Load(context.Background(), PreviewRequest{
		ProviderSpec: "custom",
		BaseURL:      "file:///tmp/provider.sock",
		CredentialRef: "env:CUSTOM_API_KEY",
		ProtocolKind: "chat_completions",
	})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if snapshot.Error != "" {
		t.Fatalf("error = %q, want empty", snapshot.Error)
	}
}

func TestPreviewLoader_Load_AcceptsLocalhostBaseURL(t *testing.T) {
	loader := NewPreviewLoader(previewProviderCatalogStub{models: []string{"m1"}})
	snapshot, err := loader.Load(context.Background(), PreviewRequest{
		ProviderSpec: "custom",
		BaseURL:      "http://127.0.0.1:11434/v1",
		ProtocolKind: "chat_completions",
	})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if snapshot.Error != "" {
		t.Fatalf("error = %q, want empty", snapshot.Error)
	}
	if len(snapshot.ModelIDs) != 1 || snapshot.ModelIDs[0] != "m1" {
		t.Fatalf("model_ids = %v, want [m1]", snapshot.ModelIDs)
	}
}
