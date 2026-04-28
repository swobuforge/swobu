package modelcatalog

import (
	"context"
	"fmt"
	"testing"

	"github.com/metrofun/swobu/internal/ports"
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
