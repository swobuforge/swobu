package adapters

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/adapters/inbound/httpapi"
	"github.com/swobuforge/swobu/internal/app/operator/credentialfiles"
	"github.com/swobuforge/swobu/internal/cockpit/features/target_config"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/profile"
	testkit "github.com/swobuforge/swobu/internal/testkit/cockpittestkit"
)

func TestCredentialFileBrowserRendersEntriesThroughOperatorHTTPPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "opencode"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "credentials.json"), []byte("not-read"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewCredentialFilesHandler(func(_ context.Context, path string) (credentialfiles.Listing, error) {
		if path == "" {
			path = dir
		}
		return credentialfiles.Browse(path)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	adapter := NewLiveOperatorAdapter(server.Client(), strings.TrimPrefix(server.URL, "http://"))
	config := target_config.NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	config.Open()
	config.SelectProvider(string(profile.ProviderSpecOpenCodeZen))
	config.TargetSetupQueries = adapter
	h, err := testkit.NewHarnessAt(target_config.CredentialControlRegion(config, true), 100, 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	frame := waitForCredentialBrowserFrame(t, h, func(frame string) bool {
		return strings.Contains(frame, "opencode/") && strings.Contains(frame, "credentials.json")
	})
	if !strings.Contains(frame, "opencode/") || !strings.Contains(frame, "credentials.json") {
		t.Fatalf("daemon-returned file entries are missing from mounted browser:\n%s", frame)
	}
}

func TestCredentialFileBrowserExplainsMissingDaemonCapability(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	adapter := NewLiveOperatorAdapter(server.Client(), strings.TrimPrefix(server.URL, "http://"))
	config := target_config.NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	config.Open()
	config.SelectProvider(string(profile.ProviderSpecOpenCodeZen))
	config.TargetSetupQueries = adapter
	h, err := testkit.NewHarnessAt(target_config.CredentialControlRegion(config, true), 100, 12)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	frame := waitForCredentialBrowserFrame(t, h, func(frame string) bool { return strings.Contains(frame, "restart or update the Swobu daemon") })
	if !strings.Contains(frame, "restart or update the Swobu daemon") {
		t.Fatalf("missing daemon capability has no actionable recovery:\n%s", frame)
	}
}

func waitForCredentialBrowserFrame(t *testing.T, h *testkit.MockAppHarness, settled func(string) bool) string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	var frame string
	for time.Now().Before(deadline) {
		frame = h.FrameTrimmed()
		if settled(frame) {
			return frame
		}
		time.Sleep(5 * time.Millisecond)
	}
	return frame
}
