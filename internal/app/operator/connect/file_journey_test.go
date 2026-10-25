package connect

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/clientconnect"
)

func TestCanonicalConnectRealFilePreviewStaleReviewAndApply(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("CODEX_HOME", directory)
	path := filepath.Join(directory, "config.toml")
	const original = "model = \"old-model\"\nmodel_provider = \"old-provider\"\nunrelated = \"preserve-me\"\n[model_providers.swobu.http_headers]\nAuthorization=\"Bearer fixture-client-token\"\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(clientconnect.NewService(), testCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	target, err := clientconnect.NewTarget("research", "http://127.0.0.1:7926/workspaces/research")
	if err != nil {
		t.Fatal(err)
	}
	target = target.WithModelID("coding").WithAuthentication("structural-test-principal", "")
	preview, err := service.Plan(t.Context(), "codex", target)
	if err != nil || preview.Workspace != "research" || preview.Client.ID != "codex" || len(preview.Changes) == 0 || preview.AlreadyConfigured {
		t.Fatalf("canonical preview: %v", err)
	}
	read := func() string {
		t.Helper()
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	if read() != original {
		t.Fatal("preview mutated client configuration")
	}
	review := preview.Review()
	changed := strings.Replace(original, "old-model", "concurrent-model", 1)
	if err := os.WriteFile(path, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	current, err := service.Apply(t.Context(), "codex", review, target)
	if !errors.Is(err, ErrReviewChanged) || current.Client.ID != "codex" || read() != changed {
		t.Fatal("stale review wrote client file or lost recovery projection")
	}
	fresh, err := service.Plan(t.Context(), "codex", target)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := service.Apply(t.Context(), "codex", fresh.Review(), target)
	if err != nil || !verified.AlreadyConfigured || len(verified.Changes) != 0 {
		t.Fatalf("reviewed apply not verified: %v", err)
	}
	committed := read()
	if !strings.Contains(committed, "http://127.0.0.1:7926/workspaces/research") || !strings.Contains(committed, `unrelated = "preserve-me"`) || strings.Contains(committed, "/c/research") {
		t.Fatal("canonical apply lost endpoint or unrelated configuration")
	}
	replayed, err := service.Apply(t.Context(), "codex", verified.Review(), target)
	if err != nil || !replayed.AlreadyConfigured || read() != committed {
		t.Fatal("already configured readback rewrote client file")
	}
}
