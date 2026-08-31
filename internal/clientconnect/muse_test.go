package clientconnect

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func museService(home, xdg string) *Service {
	return &Service{
		homeDir: func() (string, error) { return home, nil },
		getenv: func(key string) string {
			if key == "XDG_CONFIG_HOME" {
				return xdg
			}
			return ""
		},
		lookPath: func(string) (string, error) { return "", os.ErrNotExist },
	}
}

func TestMusePathHonorsXDGAndDefaultsToHomeConfig(t *testing.T) {
	home := t.TempDir()
	for _, tc := range []struct {
		name string
		xdg  string
		want string
	}{
		{name: "XDG", xdg: filepath.Join(home, "xdg"), want: filepath.Join(home, "xdg", "muse", "settings.json")},
		{name: "home", want: filepath.Join(home, ".config", "muse", "settings.json")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := museService(home, tc.xdg).musePath()
			if err != nil || got != tc.want {
				t.Fatalf("musePath() = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestMuseDiscoveryUsesPathLocalBinOrSettings(t *testing.T) {
	for _, signal := range []string{"path", "local-bin", "settings"} {
		t.Run(signal, func(t *testing.T) {
			home := t.TempDir()
			svc := museService(home, "")
			if signal == "path" {
				svc.lookPath = func(string) (string, error) { return "/usr/bin/muse", nil }
			} else {
				path := filepath.Join(home, ".local", "bin", "muse")
				if signal == "settings" {
					path = filepath.Join(home, ".config", "muse", "settings.json")
				}
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			present, err := musePresent(svc)
			if err != nil || !present {
				t.Fatalf("musePresent() = %v, %v", present, err)
			}
		})
	}
	home := t.TempDir()
	present, err := musePresent(museService(home, ""))
	if err != nil || present {
		t.Fatalf("absent muse = %v, %v", present, err)
	}
}

func TestMuseCreatesFacadeConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "muse", "settings.json")
	plan, err := planMuse(path, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if plan.plan.RequiresReplace() {
		t.Fatal("fresh Muse configuration must not require replacement")
	}
	if err := plan.apply(); err != nil {
		t.Fatal(err)
	}
	got := readMuseSettings(t, path)
	assertMuseOwnedState(t, got)
	if got["schema_version"] != float64(1) {
		t.Fatalf("schema_version = %#v", got["schema_version"])
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, %v", info.Mode().Perm(), err)
	}
}

func TestMusePreservesUnrelatedConfigurationAndPresentSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := []byte(`{
  "schema_version": 7,
  "provider": "meta",
  "model": "muse-spark-1.2",
  "endpoint_transport": {"base_url":"https://api.meta.ai/v1","auth":"meta", "keep":"yes"},
  "model_catalog": [{"model_id":"muse-spark-1.2","provider_id":"meta","profile_id":"tbh","display_label":"Meta","visibility":"visible","display_order":0,"is_default":true,"context_limit":1,"output_limit":1,"description":"old"}],
  "mcp_servers": {"docs": {"url":"https://example.test"}},
  "tui": {"theme":"dark"},
  "hooks": ["keep"],
  "skills": {"keep":true},
  "future": 9007199254740993123456789
}
`)
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatal(err)
	}
	mutation, err := planMuse(path, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if !mutation.plan.RequiresReplace() {
		t.Fatal("existing Meta-direct Muse configuration must require replacement")
	}
	if err := mutation.apply(); err != nil {
		t.Fatal(err)
	}
	got := readMuseSettings(t, path)
	assertMuseOwnedState(t, got)
	if got["schema_version"] != float64(7) {
		t.Fatalf("present schema version changed: %#v", got["schema_version"])
	}
	endpoint := got["endpoint_transport"].(map[string]any)
	if endpoint["keep"] != "yes" {
		t.Fatalf("endpoint sibling was not preserved: %#v", endpoint)
	}
	for key, want := range map[string]any{
		"mcp_servers": map[string]any{"docs": map[string]any{"url": "https://example.test"}},
		"tui":         map[string]any{"theme": "dark"},
		"hooks":       []any{"keep"},
		"skills":      map[string]any{"keep": true},
	} {
		if !reflect.DeepEqual(got[key], want) {
			t.Fatalf("%s changed: %#v", key, got[key])
		}
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestMuseReplacesCatalogRowsWithUnownedFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	raw := []byte(`{"provider":"meta","model":"default","endpoint_transport":{"base_url":"http://127.0.0.1:7926/c/work/v1","auth":"none"},"model_catalog":[{"model_id":"default","provider_id":"meta","profile_id":"tbh","display_label":"Swobu","visibility":"visible","display_order":0,"is_default":true,"context_limit":1048576,"output_limit":131072,"description":"Muse Spark via Swobu","future":"must be removed"}],"schema_version":1}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	mutation, err := planMuse(path, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if mutation.plan.AlreadyConfigured() || !mutation.plan.RequiresReplace() {
		t.Fatalf("catalog with an extra row field was accepted: %#v", mutation.plan)
	}
	if err := mutation.apply(); err != nil {
		t.Fatal(err)
	}
	encoded, _ := os.ReadFile(path)
	if strings.Contains(string(encoded), "future") {
		t.Fatalf("owned model_catalog retained an old row field: %s", encoded)
	}
}

func TestMusePlanIsIdempotentAndFreshnessProtected(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	path := filepath.Join(xdg, "muse", "settings.json")
	svc := museService(home, xdg)
	first, err := svc.Plan(ClientMuse, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Apply(first); err != nil {
		t.Fatal(err)
	}
	second, err := svc.Plan(ClientMuse, testTarget(t))
	if err != nil || !second.AlreadyConfigured() {
		t.Fatalf("second plan = %#v, %v", second, err)
	}

	raw := readMuseSettings(t, path)
	raw["model"] = "changed"
	encoded, _ := json.Marshal(raw)
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := svc.Apply(second); err == nil || !strings.Contains(err.Error(), "Client configuration changed") {
		t.Fatalf("freshness error = %v", err)
	}
}

func TestMuseRejectsMalformedOwnedStateWithoutChange(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"provider":1,"keep":true}`),
		[]byte(`{"endpoint_transport":"wrong","keep":true}`),
		[]byte(`{"model_catalog":{},"keep":true}`),
		[]byte(`{"provider":"meta","provider":"other"}`),
		[]byte(`not-json`),
	} {
		path := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := planMuse(path, testTarget(t)); err == nil || !strings.Contains(err.Error(), "Nothing changed") {
			t.Fatalf("error = %v for %s", err, raw)
		}
		got, _ := os.ReadFile(path)
		if !reflect.DeepEqual(got, raw) {
			t.Fatalf("malformed settings changed: %s", got)
		}
	}
}

func TestMuseApplyRejectsMutationAfterReview(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	svc := museService(home, xdg)
	plan, err := svc.Plan(ClientMuse, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(xdg, "muse", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"model":"changed"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := svc.Apply(plan); err == nil || !strings.Contains(err.Error(), "Client configuration changed") {
		t.Fatalf("freshness error = %v", err)
	}
}

func readMuseSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	if err := decoder.Decode(&got); err != nil {
		t.Fatalf("decode Muse settings: %v\n%s", err, raw)
	}
	return got
}

func assertMuseOwnedState(t *testing.T, got map[string]any) {
	t.Helper()
	if got["provider"] != "meta" || got["model"] != "default" {
		t.Fatalf("Muse facade selection = %#v", got)
	}
	endpoint, ok := got["endpoint_transport"].(map[string]any)
	if !ok || endpoint["base_url"] != testTarget(t).WorkspaceURL()+"/v1" || endpoint["auth"] != "none" {
		t.Fatalf("Muse endpoint transport = %#v", got["endpoint_transport"])
	}
	catalog, ok := got["model_catalog"].([]any)
	if !ok || len(catalog) != 1 {
		t.Fatalf("Muse model catalog = %#v", got["model_catalog"])
	}
	entry := catalog[0].(map[string]any)
	for key, want := range map[string]any{
		"model_id": "default", "provider_id": "meta", "profile_id": "tbh",
		"display_label": "Swobu", "visibility": "visible", "is_default": true,
		"context_limit": float64(1048576), "output_limit": float64(131072),
		"description": "Muse Spark via Swobu",
	} {
		if entry[key] != want {
			t.Fatalf("catalog %s = %#v, want %#v", key, entry[key], want)
		}
	}
}

func TestMusePresencePropagatesHomeError(t *testing.T) {
	svc := &Service{
		homeDir:  func() (string, error) { return "", errors.New("home unavailable") },
		getenv:   func(string) string { return "" },
		lookPath: func(string) (string, error) { return "", os.ErrNotExist },
	}
	if _, err := musePresent(svc); err == nil {
		t.Fatal("home error was not propagated")
	}
}
