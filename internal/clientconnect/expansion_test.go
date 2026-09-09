package clientconnect

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAutomaticClientIDGrammarIsClosedAndCanonical(t *testing.T) {
	got := AutomaticClientIDs()
	if len(got) != len(adapters) {
		t.Fatalf("IDs = %v", got)
	}
	seen := make(map[ClientID]struct{}, len(adapters))
	for i, adapter := range adapters {
		if adapter.id == "" || adapter.name == "" || adapter.present == nil || adapter.planCurrent == nil {
			t.Fatalf("invalid registry entry: %#v", adapter)
		}
		if _, duplicate := seen[adapter.id]; duplicate {
			t.Fatalf("duplicate client ID %q", adapter.id)
		}
		seen[adapter.id] = struct{}{}
		if got[i] != adapter.id {
			t.Fatalf("IDs = %v", got)
		}
		if parsed, err := ParseClientID(string(adapter.id)); err != nil || parsed != adapter.id {
			t.Fatalf("parse %q = %q, %v", adapter.id, parsed, err)
		}
	}
	for _, invalid := range []string{"Codex", "kilo-code", "openai", ""} {
		if _, err := ParseClientID(invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
}

func TestRegistryEntryAloneExtendsGrammarOrderingAndPlanDispatch(t *testing.T) {
	original := adapters
	t.Cleanup(func() { adapters = original })
	const fifth ClientID = "fifth"
	alwaysPresent := func(*Service) (bool, error) { return true, nil }
	adapters = append(append([]adapter(nil), adapters...), adapter{id: fifth, name: "Fifth", present: alwaysPresent, planCurrent: func(_ context.Context, _ *Service, target Target) (plannedMutation, error) {
		return plannedMutation{plan: Plan{ClientID: fifth, ClientName: "Fifth", Target: target}}, nil
	}})
	ids := AutomaticClientIDs()
	if ids[len(ids)-1] != fifth {
		t.Fatalf("IDs = %v", ids)
	}
	if parsed, err := ParseClientID("fifth"); err != nil || parsed != fifth {
		t.Fatalf("Parse = %q, %v", parsed, err)
	}
	plan, err := (&Service{}).Plan(context.Background(), fifth, testTarget(t))
	if err != nil || plan.ClientID != fifth {
		t.Fatalf("Plan = %#v, %v", plan, err)
	}
}

func TestApplyReplansThroughRegistryAndDetectsChangedKiloLocus(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "kilo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(dir, "kilo.json")
	jsoncPath := filepath.Join(dir, "kilo.jsonc")
	if err := os.WriteFile(jsonPath, []byte(`{"model":"anthropic/model"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &Service{homeDir: func() (string, error) { return home, nil }, getenv: func(string) string { return "" }, lookPath: func(string) (string, error) { return "", os.ErrNotExist }}
	plan, err := service.Plan(context.Background(), ClientKilo, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsoncPath, []byte(`{"model":"anthropic/model"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "Open Connect again") {
		t.Fatalf("error = %v", err)
	}
	for _, path := range []string{jsonPath, jsoncPath} {
		raw, _ := os.ReadFile(path)
		if strings.Contains(string(raw), testTarget(t).WorkspaceURL()) {
			t.Fatalf("stale locus mutated at %s: %s", path, raw)
		}
	}
}

func TestApplyRejectsChangedKiloLocusWithLegacyHijack(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "kilo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(dir, "kilo.json")
	jsoncPath := filepath.Join(dir, "kilo.jsonc")
	original := []byte(`{"model":"anthropic/model"}`)
	if err := os.WriteFile(jsonPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	service := &Service{homeDir: func() (string, error) { return home, nil }, getenv: func(string) string { return "" }, lookPath: func(string) (string, error) { return "", os.ErrNotExist }}
	plan, err := service.Plan(context.Background(), ClientKilo, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	alreadyConfigured := []byte(`{"model":"anthropic/model","provider":{"anthropic":{"options":{"baseURL":"` + testTarget(t).WorkspaceURL() + `"}}}}`)
	if err := os.WriteFile(jsoncPath, alreadyConfigured, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), plan); err == nil {
		t.Fatal("legacy provider hijack accepted as configured")
	}
	gotJSON, _ := os.ReadFile(jsonPath)
	gotJSONC, _ := os.ReadFile(jsoncPath)
	if !bytes.Equal(gotJSON, original) || !bytes.Equal(gotJSONC, alreadyConfigured) {
		t.Fatalf("desired-state short circuit modified files:\njson=%s\njsonc=%s", gotJSON, gotJSONC)
	}
}

func TestDiscoveryIsolatesPresenceAndPlanFailures(t *testing.T) {
	original := adapters
	t.Cleanup(func() { adapters = original })
	alwaysPresent := func(*Service) (bool, error) { return true, nil }
	adapters = []adapter{
		{id: "broken-presence", name: "Broken presence", present: func(*Service) (bool, error) { return false, os.ErrPermission }, planCurrent: func(context.Context, *Service, Target) (plannedMutation, error) {
			t.Fatal("plan after presence error")
			return plannedMutation{}, nil
		}},
		{id: "safe", name: "Safe", present: alwaysPresent, planCurrent: func(_ context.Context, _ *Service, target Target) (plannedMutation, error) {
			return plannedMutation{plan: Plan{ClientID: "safe", ClientName: "Safe", Target: target}}, nil
		}},
		{id: "broken-plan", name: "Broken plan", present: alwaysPresent, planCurrent: func(context.Context, *Service, Target) (plannedMutation, error) {
			return plannedMutation{}, os.ErrPermission
		}},
	}
	clients := (&Service{}).Discover(context.Background(), testTarget(t))
	if len(clients) != 2 || clients[0].ID != "safe" || clients[1].ID != "broken-plan" {
		t.Fatalf("clients = %#v", clients)
	}
}

func TestDiscoveryDoesNotInvokePlanCurrent(t *testing.T) {
	original := adapters
	t.Cleanup(func() { adapters = original })
	adapters = []adapter{
		{
			id:      "test-client",
			name:    "Test Client",
			present: func(*Service) (bool, error) { return true, nil },
			planCurrent: func(context.Context, *Service, Target) (plannedMutation, error) {
				panic("planCurrent must not be called during Discover")
			},
		},
	}
	clients := (&Service{}).Discover(context.Background(), testTarget(t))
	if len(clients) != 1 || clients[0].ID != "test-client" {
		t.Fatalf("unexpected clients: %#v", clients)
	}
}

func TestKiloAndPiPresenceAvoidPlanningAbsentClients(t *testing.T) {
	home := t.TempDir()
	service := &Service{homeDir: func() (string, error) { return home, nil }, getenv: func(string) string { return "" }, lookPath: func(string) (string, error) { return "", os.ErrNotExist }}
	for _, probe := range []struct {
		name string
		fn   func(*Service) (bool, error)
	}{{"kilo", kiloPresent}, {"pi", piPresent}} {
		present, err := probe.fn(service)
		if err != nil || present {
			t.Fatalf("%s present = %v, %v", probe.name, present, err)
		}
	}
}

func TestKiloRefusesConfigDirOverrideAndAcceptsSlashInModelID(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "kilo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "kilo.json"), []byte(`{"model":"openai-compatible/vendor/model"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &Service{homeDir: func() (string, error) { return home, nil }, getenv: func(string) string { return "" }}
	if _, err := service.Plan(context.Background(), ClientKilo, testTarget(t)); err != nil {
		t.Fatalf("slash model rejected: %v", err)
	}
	service.getenv = func(key string) string {
		if key == "KILO_CONFIG_DIR" {
			return "/tmp/other"
		}
		return ""
	}
	if _, err := service.Plan(context.Background(), ClientKilo, testTarget(t)); err == nil || !strings.Contains(err.Error(), "KILO_CONFIG_DIR") {
		t.Fatalf("override error = %v", err)
	}
}

func TestKiloCreatesMissingGlobalConfiguration(t *testing.T) {
	home := t.TempDir()
	service := &Service{
		homeDir: func() (string, error) { return home, nil },
		getenv:  func(string) string { return "" },
		lookPath: func(name string) (string, error) {
			if name == "kilo" {
				return filepath.Join(home, "bin", "kilo"), nil
			}
			return "", os.ErrNotExist
		},
	}
	found := false
	for _, client := range service.Discover(context.Background(), testTarget(t)) {
		if client.ID == ClientKilo {
			found = true
		}
	}
	if !found {
		t.Fatal("Kilo executable was not discovered")
	}
	plan, err := service.Plan(context.Background(), ClientKilo, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".config", "kilo", "kilo.jsonc")
	if plan.ConfigPaths[0] != path {
		t.Fatalf("config path = %q, want %q", plan.ConfigPaths[0], path)
	}
	if plan.RequiresReplace() {
		t.Fatalf("missing configuration requires replace: %#v", plan.Changes)
	}
	if _, err := service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"model":"swobu/default"`, `"baseURL":"` + testTarget(t).WorkspaceURL() + `"`, `"tool_call":true`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %q in %s", want, raw)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	second, err := service.Plan(context.Background(), ClientKilo, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if !second.AlreadyConfigured() {
		t.Fatalf("second plan = %#v", second.Changes)
	}
}

func TestKiloUsesXDGConfigHome(t *testing.T) {
	home := t.TempDir()
	xdg := t.TempDir()
	service := &Service{homeDir: func() (string, error) { return home, nil }, getenv: func(key string) string {
		if key == "XDG_CONFIG_HOME" {
			return xdg
		}
		return ""
	}}
	plan, err := service.Plan(context.Background(), ClientKilo, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(xdg, "kilo", "kilo.jsonc")
	if plan.ConfigPaths[0] != want {
		t.Fatalf("config path = %q, want %q", plan.ConfigPaths[0], want)
	}
	if _, err := service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "kilo", "kilo.jsonc")); !os.IsNotExist(err) {
		t.Fatalf("home fallback was created: %v", err)
	}
}

func TestKiloRefusesLegacyGlobalConfigurationShadow(t *testing.T) {
	for _, tc := range []struct {
		name      string
		canonical bool
		legacy    string
	}{
		{name: "opencode json only", legacy: "opencode.json"},
		{name: "opencode jsonc only", legacy: "opencode.jsonc"},
		{name: "kilo jsonc and opencode json", canonical: true, legacy: "opencode.json"},
		{name: "kilo jsonc and opencode jsonc", canonical: true, legacy: "opencode.jsonc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			dir := filepath.Join(home, ".config", "kilo")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			legacyPath := filepath.Join(dir, tc.legacy)
			legacyRaw := []byte(`{"model":"legacy/model","keep":"legacy"}`)
			if err := os.WriteFile(legacyPath, legacyRaw, 0o600); err != nil {
				t.Fatal(err)
			}
			canonicalPath := filepath.Join(dir, "kilo.jsonc")
			canonicalRaw := []byte(`{"model":"canonical/model","keep":"canonical"}`)
			if tc.canonical {
				if err := os.WriteFile(canonicalPath, canonicalRaw, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			service := &Service{homeDir: func() (string, error) { return home, nil }, getenv: func(string) string { return "" }}
			if _, err := service.Plan(context.Background(), ClientKilo, testTarget(t)); err == nil || !strings.Contains(err.Error(), tc.legacy) {
				t.Fatalf("error = %v", err)
			}
			gotLegacy, err := os.ReadFile(legacyPath)
			if err != nil || !bytes.Equal(gotLegacy, legacyRaw) {
				t.Fatalf("legacy changed: %v, %s", err, gotLegacy)
			}
			if tc.canonical {
				gotCanonical, err := os.ReadFile(canonicalPath)
				if err != nil || !bytes.Equal(gotCanonical, canonicalRaw) {
					t.Fatalf("canonical changed: %v, %s", err, gotCanonical)
				}
			} else if _, err := os.Stat(canonicalPath); !os.IsNotExist(err) {
				t.Fatalf("canonical created: %v", err)
			}
		})
	}
}

func TestPlanEqualityKeepsSemanticFieldIdentity(t *testing.T) {
	left := Plan{Changes: []Change{{Field: "endpoint", After: "a"}}}
	right := Plan{Changes: []Change{{Field: "protocol", After: "a"}}}
	if left.equal(right) {
		t.Fatal("distinct structured paths compared equal")
	}
}

func TestPlanHasOneReviewedSemanticChangeTruth(t *testing.T) {
	target := testTarget(t)
	base := Plan{
		ClientID: ClientCodex, ClientName: "Codex CLI", ConfigPaths: []string{"/tmp/config"},
		Target: target, Changes: []Change{{Field: "endpoint", Before: "https://old", BeforeExists: true, After: target.WorkspaceURL()}},
	}
	mutations := map[string]func(Plan) Plan{
		"client ID":   func(p Plan) Plan { p.ClientID = ClientClaude; return p },
		"client name": func(p Plan) Plan { p.ClientName = "Changed"; return p },
		"config path": func(p Plan) Plan {
			p.ConfigPaths = append([]string(nil), p.ConfigPaths...)
			p.ConfigPaths[0] = "/tmp/other"
			return p
		},
		"changes": func(p Plan) Plan {
			p.Changes = append([]Change(nil), p.Changes...)
			p.Changes[0].Before = "https://newer"
			return p
		},
		"target": func(p Plan) Plan {
			p.Target, _ = NewTarget("other", "http://127.0.0.1:7926/c/other")
			return p
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			if base.equal(mutate(base)) {
				t.Fatalf("mutating exported reviewed field %q was invisible to Apply equality", name)
			}
		})
	}
}

func TestPlanEndpointStatesDeriveFromBeforeAndExistence(t *testing.T) {
	target := testTarget(t)
	for _, tc := range []struct {
		name                  string
		before                string
		exists                bool
		configured, overwrite bool
	}{
		{name: "absent"},
		{name: "present empty", exists: true, overwrite: true},
		{name: "present foreign", before: "https://old", exists: true, overwrite: true},
		{name: "already equal", before: target.WorkspaceURL(), exists: true, configured: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := Plan{Target: target, Changes: semanticChange("endpoint", tc.before, tc.exists, target.WorkspaceURL())}
			if plan.AlreadyConfigured() != tc.configured || plan.RequiresReplace() != tc.overwrite {
				t.Fatalf("configured/replace = %v/%v", plan.AlreadyConfigured(), plan.RequiresReplace())
			}
		})
	}
}

func TestJSONEditorSetStringRejectsExistingNonStringLeaf(t *testing.T) {
	original := []byte(`{"env":{"URL":42,"keep":true}}`)
	if _, err := (jsonEditor{}).SetString(original, keyPath{"env", "URL"}, "https://example"); err == nil || !strings.Contains(err.Error(), "not a string") {
		t.Fatalf("SetString error = %v", err)
	}
}

func TestKiloDeclaresSwobuBackendAndPreservesForeignProviders(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "kilo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "kilo.jsonc")
	original := []byte("{\n  // durable selection\n  \"model\": \"anthropic/claude\",\n  \"provider\": {\n    \"anthropic\": {\"options\": {\"apiKey\": \"{env:KEY}\", \"baseURL\": \"https://old\",},},\n    \"other\": {\"options\": {\"baseURL\": \"https://leave\"}},\n  },\n  \"mcp\": {\"keep\": true},\n}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	service := &Service{homeDir: func() (string, error) { return home, nil }, getenv: func(string) string { return "" }}
	plan, err := service.Plan(context.Background(), ClientKilo, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	text := string(got)
	for _, want := range []string{`"model": "swobu/default"`, `"baseURL":"http://127.0.0.1:7926/c/work"`, `"name":"Swobu"`, `"name":"Swobu default"`, `"tool_call":true`, `"baseURL": "https://old"`, `"baseURL": "https://leave"`, `"mcp": {"keep": true}`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
}

func TestKiloReusesExistingSwobuPresentationAndMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kilo.jsonc")
	original := []byte(`{"model":"other/model","provider":{"swobu":{"name":"My gateway","metadata":{"owner":"human"},"options":{"baseURL":"http://127.0.0.1:7926/c/old"},"models":{"default":{"name":"My default","temperature":0.2,"tool_call":false}}}}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	mutation, err := planKilo(path, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range mutation.plan.Changes {
		if change.Field == "provider name" || change.Field == "model name" {
			t.Fatalf("presentation field was over-owned: %#v", mutation.plan.Changes)
		}
	}
	if err := mutation.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	for _, want := range []string{`"name":"My gateway"`, `"name":"My default"`, `"owner":"human"`, `"temperature":0.2`, `"tool_call":true`, `"baseURL":"http://127.0.0.1:7926/c/work"`} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
}

func TestKiloAddsMissingObjectLevelsAndRejectsStructuralConflict(t *testing.T) {
	for _, provider := range []string{"anthropic", "openai", "openai-compatible"} {
		home := t.TempDir()
		dir := filepath.Join(home, ".config", "kilo")
		_ = os.MkdirAll(dir, 0o700)
		path := filepath.Join(dir, "kilo.jsonc")
		raw := []byte(`{"model":"` + provider + `/model","keep":1}`)
		_ = os.WriteFile(path, raw, 0o600)
		service := &Service{homeDir: func() (string, error) { return home, nil }, getenv: func(string) string { return "" }}
		plan, err := service.Plan(context.Background(), ClientKilo, testTarget(t))
		if err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
		if _, err := service.Apply(context.Background(), plan); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(path)
		if !strings.Contains(string(got), `"model":"swobu/default"`) || !strings.Contains(string(got), `"baseURL":"http://127.0.0.1:7926/c/work"`) {
			t.Fatalf("%s next: %s", provider, got)
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "kilo.jsonc")
	_ = os.WriteFile(path, []byte(`{"model":"anthropic/model","provider":"wrong"}`), 0o600)
	if _, err := planKilo(path, testTarget(t)); err == nil || !strings.Contains(err.Error(), "not an object") {
		t.Fatalf("error = %v", err)
	}
}

func TestDiscoveryIncludesPresentClientsEvenWhenPlanFails(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".codex"), 0o700)
	_ = os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("[broken"), 0o600)
	service := &Service{
		homeDir: func() (string, error) { return home, nil },
		getenv:  func(string) string { return "" },
		lookPath: func(name string) (string, error) {
			if name == "codex" {
				return "/bin/codex", nil
			}
			return "", os.ErrNotExist
		},
	}
	clients := service.Discover(context.Background(), testTarget(t))
	found := false
	for _, client := range clients {
		if client.ID == ClientCodex {
			found = true
		}
	}
	if !found {
		t.Fatalf("present client with broken plan omitted from discovery: %v", clients)
	}
}

func TestKiloServiceResolutionAndOverrides(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "kilo")
	_ = os.MkdirAll(dir, 0o700)
	jsonPath := filepath.Join(dir, "kilo.json")
	_ = os.WriteFile(jsonPath, []byte(`{"model":"openai/model"}`), 0o600)
	service := &Service{homeDir: func() (string, error) { return home, nil }, getenv: func(string) string { return "" }}
	plan, err := service.Plan(context.Background(), ClientKilo, testTarget(t))
	if err != nil || plan.ConfigPaths[0] != jsonPath {
		t.Fatalf("plan = %#v, %v", plan, err)
	}
	jsoncPath := filepath.Join(dir, "kilo.jsonc")
	_ = os.WriteFile(jsoncPath, []byte(`{"model":"anthropic/model"}`), 0o600)
	plan, err = service.Plan(context.Background(), ClientKilo, testTarget(t))
	if err != nil || plan.ConfigPaths[0] != jsoncPath {
		t.Fatalf("precedence plan = %#v, %v", plan, err)
	}
	for _, override := range []string{"KILO_PROVIDER", "KILO_CONFIG", "KILO_CONFIG_DIR", "KILO_CONFIG_CONTENT"} {
		overrideHome := t.TempDir()
		overrideService := &Service{homeDir: func() (string, error) { return overrideHome, nil }, getenv: func(key string) string {
			if key == override {
				return "explicit"
			}
			return ""
		}}
		if _, err := overrideService.Plan(context.Background(), ClientKilo, testTarget(t)); err == nil || !strings.Contains(err.Error(), override) {
			t.Fatalf("%s override error = %v", override, err)
		}
		if _, err := os.Stat(filepath.Join(overrideHome, ".config", "kilo", "kilo.jsonc")); !os.IsNotExist(err) {
			t.Fatalf("%s override created canonical config: %v", override, err)
		}
	}
}

func TestPiDeclaresSwobuProviderThenSelectsIt(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	models := filepath.Join(dir, "models.json")
	_ = os.WriteFile(settings, []byte(`{"defaultProvider":"anthropic","defaultModel":"keep"}`), 0o600)
	original := []byte("{\n  \"providers\": {\n    \"anthropic\": {\"headers\": {\"x\": \"y\"}, \"baseUrl\": \"https://old\", \"modelOverrides\": {\"keep\": true}},\n    \"other\": {\"models\": [1,2,3]}\n  },\n  \"keep\": 9007199254740993123456789\n}\n")
	_ = os.WriteFile(models, original, 0o600)
	service := &Service{homeDir: func() (string, error) { return dir, nil }, getenv: func(key string) string {
		if key == "PI_CODING_AGENT_DIR" {
			return dir
		}
		return ""
	}}
	plan, err := service.Plan(context.Background(), ClientPi, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(models)
	text := string(got)
	for _, want := range []string{`"swobu"`, `"baseUrl":"http://127.0.0.1:7926/c/work"`, `"api":"openai-responses"`, `"apiKey":"swobu"`, `"id":"default"`, `"baseUrl": "https://old"`, `"other": {"models": [1,2,3]}`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
	settingsRaw, _ := os.ReadFile(settings)
	if !strings.Contains(string(settingsRaw), `"defaultProvider":"swobu"`) || !strings.Contains(string(settingsRaw), `"defaultModel":"default"`) {
		t.Fatalf("settings=%s", settingsRaw)
	}
}

func TestPiCreatesMissingGlobalConfiguration(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".pi", "agent")
	service := &Service{getenv: func(key string) string {
		if key == "PI_CODING_AGENT_DIR" {
			return dir
		}
		return ""
	}}
	plan, err := service.Plan(context.Background(), ClientPi, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiresReplace() {
		t.Fatalf("missing configuration requires replace: %#v", plan.Changes)
	}
	wantPaths := []string{filepath.Join(dir, "models.json"), filepath.Join(dir, "settings.json")}
	if !slices.Equal(plan.ConfigPaths, wantPaths) {
		t.Fatalf("config paths = %v, want %v", plan.ConfigPaths, wantPaths)
	}
	if _, err := service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	settings, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(settings), `"defaultProvider":"swobu"`) || !strings.Contains(string(settings), `"defaultModel":"default"`) {
		t.Fatalf("settings=%s", settings)
	}
	models, err := os.ReadFile(filepath.Join(dir, "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"swobu"`, `"baseUrl":"` + testTarget(t).WorkspaceURL() + `"`, `"api":"openai-responses"`, `"id":"default"`, `"name":"Swobu default"`} {
		if !strings.Contains(string(models), want) {
			t.Fatalf("missing %q in models=%s", want, models)
		}
	}
	for _, path := range []string{filepath.Join(dir, "settings.json"), filepath.Join(dir, "models.json")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o", path, info.Mode().Perm())
		}
	}
	second, err := service.Plan(context.Background(), ClientPi, testTarget(t))
	if err != nil || !second.AlreadyConfigured() {
		t.Fatalf("second plan = %#v, error = %v", second.Changes, err)
	}
}

func TestPiNormalizesOwnedHistoricalStateUnderReplacement(t *testing.T) {
	dir := t.TempDir()
	settings, models := filepath.Join(dir, "settings.json"), filepath.Join(dir, "models.json")
	_ = os.WriteFile(settings, []byte(`{"defaultProvider":"swobu","defaultModel":"default"}`), 0o600)
	_ = os.WriteFile(models, []byte(`{"providers":{"swobu":{"baseUrl":"http://127.0.0.1:7926/c/work","api":"openai-completions","apiKey":"user-managed","authHeader":false,"compat":{"supportsDeveloperRole":false},"modelOverrides":{"default":{"maxTokens":1000000}},"models":[{"id":"gpt-4.1-mini"},{"id":"default","maxTokens":1000000}]}}}`), 0o600)
	service := &Service{getenv: func(key string) string {
		if key == "PI_CODING_AGENT_DIR" {
			return dir
		}
		return ""
	}}
	plan, err := service.Plan(context.Background(), ClientPi, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if !plan.RequiresReplace() {
		t.Fatalf("historical state did not require replacement: %#v", plan.Changes)
	}
	for _, field := range []string{"protocol", "model catalog"} {
		if !slices.ContainsFunc(plan.Changes, func(change Change) bool { return change.Field == field }) {
			t.Fatalf("plan lacks %q evidence: %#v", field, plan.Changes)
		}
	}
	verified, err := service.Apply(context.Background(), plan)
	if err != nil || !verified.AlreadyConfigured() {
		t.Fatalf("verified = %#v, error = %v", verified.Changes, err)
	}
	raw, _ := os.ReadFile(models)
	for _, preserved := range []string{`"apiKey":"user-managed"`, `"authHeader":false`, `"compat":{"supportsDeveloperRole":false}`, `"modelOverrides":{"default":{"maxTokens":1000000}}`} {
		if !strings.Contains(string(raw), preserved) {
			t.Fatalf("missing preserved %q in %s", preserved, raw)
		}
	}
	for _, removed := range []string{"gpt-4.1-mini", `"maxTokens":1000000}]`} {
		if strings.Contains(string(raw), removed) {
			t.Fatalf("stale %q remains in %s", removed, raw)
		}
	}
}

func TestPiPreservesJSONCOutsideOwnedLeaves(t *testing.T) {
	dir := t.TempDir()
	settings, models := filepath.Join(dir, "settings.json"), filepath.Join(dir, "models.json")
	_ = os.WriteFile(settings, []byte(`{"defaultProvider":"swobu","defaultModel":"default"}`), 0o600)
	original := []byte("{\n  // before providers\n  \"providers\": {\n    \"other\": {\n      // other provider comment\n      \"opaque\": 9007199254740993123456789,\n    },\n    \"swobu\": {\n      // retained provider metadata comment\n      \"metadata\": {\"owner\": \"human\"},\n      \"api\": \"openai-completions\",\n      \"models\": [{\"id\": \"default\", \"maxTokens\": 1000000}],\n    },\n  },\n}\n")
	_ = os.WriteFile(models, original, 0o600)
	mutation, err := planPi(settings, models, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := mutation.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(models)
	for _, preserved := range []string{"// before providers", "// other provider comment", "// retained provider metadata comment", "9007199254740993123456789", `"owner": "human"`} {
		if !strings.Contains(string(raw), preserved) {
			t.Fatalf("source outside owned leaves lost %q:\n%s", preserved, raw)
		}
	}
	if _, err := (jsonEditor{allowComments: true}).parse(raw); err != nil {
		t.Fatalf("result is not valid Pi JSONC: %v\n%s", err, raw)
	}
}

func TestPiDecodesJSONCInsideOwnedModelsAndPreservesCustomizations(t *testing.T) {
	dir := t.TempDir()
	settings, models := filepath.Join(dir, "settings.json"), filepath.Join(dir, "models.json")
	_ = os.WriteFile(settings, []byte(`{"defaultProvider":"swobu","defaultModel":"default"}`), 0o600)
	raw := []byte(`{
  "providers": {"swobu": {
    "baseUrl": "http://127.0.0.1:7926/c/work",
    "api": "openai-responses",
    "apiKey": "swobu",
    "models": [
      // facade model
      {"id":"default", "name":"Swobu default", "reasoning":false, "input":["text"], "contextWindow":65536, "maxTokens":32000},
    ],
    "compat": {
      // stale provider override
      "supportsDeveloperRole": false,
    },
    "modelOverrides": {
      // stale model override
      "default": {"maxTokens": 1,},
    },
  }}
}`)
	_ = os.WriteFile(models, raw, 0o600)
	mutation, err := planPi(settings, models, testTarget(t))
	if err != nil {
		t.Fatalf("JSONC inside owned subtrees was rejected: %v", err)
	}
	if !slices.ContainsFunc(mutation.plan.Changes, func(change Change) bool { return change.Field == "model catalog" }) {
		t.Fatalf("plan lacks model catalog change: %#v", mutation.plan.Changes)
	}
	if err := mutation.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(models)
	for _, preserved := range []string{"// stale provider override", `"supportsDeveloperRole": false`, "// stale model override", `"default": {"maxTokens": 1,}`} {
		if !strings.Contains(string(got), preserved) {
			t.Fatalf("Pi customization was not preserved %q:\n%s", preserved, got)
		}
	}
}

func TestPiCanonicalModelKeyOrderIsSemantic(t *testing.T) {
	dir := t.TempDir()
	settings, models := filepath.Join(dir, "settings.json"), filepath.Join(dir, "models.json")
	_ = os.WriteFile(settings, []byte(`{"defaultProvider":"swobu","defaultModel":"default"}`), 0o600)
	_ = os.WriteFile(models, []byte(`{"providers":{"swobu":{"baseUrl":"http://127.0.0.1:7926/c/work","api":"openai-responses","apiKey":"swobu","models":[{"name":"Swobu default","id":"default"}]}}}`), 0o600)
	mutation, err := planPi(settings, models, testTarget(t))
	if err != nil || !mutation.plan.AlreadyConfigured() {
		t.Fatalf("reordered canonical model = %#v, error = %v", mutation.plan.Changes, err)
	}
}

func TestPiApplyRejectsConcurrentOwnedModelChange(t *testing.T) {
	dir := t.TempDir()
	settings, models := filepath.Join(dir, "settings.json"), filepath.Join(dir, "models.json")
	_ = os.WriteFile(settings, []byte(`{"defaultProvider":"swobu","defaultModel":"default"}`), 0o600)
	_ = os.WriteFile(models, []byte(`{"providers":{"swobu":{"baseUrl":"http://127.0.0.1:7926/c/work","api":"openai-responses","apiKey":"swobu","models":[{"id":"default","maxTokens":1000}]}}}`), 0o600)
	service := &Service{getenv: func(key string) string {
		if key == "PI_CODING_AGENT_DIR" {
			return dir
		}
		return ""
	}}
	plan, err := service.Plan(context.Background(), ClientPi, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	changed := []byte(`{"providers":{"swobu":{"baseUrl":"http://127.0.0.1:7926/c/work","api":"openai-responses","apiKey":"swobu","models":[{"id":"default","maxTokens":2000}]}}}`)
	_ = os.WriteFile(models, changed, 0o600)
	if _, err := service.Apply(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "configuration changed") {
		t.Fatalf("error = %v", err)
	}
	raw, _ := os.ReadFile(models)
	if !bytes.Equal(raw, changed) {
		t.Fatalf("stale apply changed owned state:\n%s", raw)
	}
}

func TestPiRepairsRecoverableOwnedModelValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{name: "models is not array", raw: `{"providers":{"swobu":{"models":{}}}}`},
		{name: "models is null", raw: `{"providers":{"swobu":{"models":null}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			settings, models := filepath.Join(dir, "settings.json"), filepath.Join(dir, "models.json")
			_ = os.WriteFile(settings, []byte(`{"defaultProvider":"swobu","defaultModel":"default"}`), 0o600)
			_ = os.WriteFile(models, []byte(tc.raw), 0o600)
			mutation, err := planPi(settings, models, testTarget(t))
			if err != nil || !mutation.plan.RequiresReplace() {
				t.Fatalf("plan = %#v, error = %v", mutation.plan.Changes, err)
			}
			if err := mutation.apply(context.Background()); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(models)
			if !strings.Contains(string(got), `"models":[{"id":"default","name":"Swobu default"}]`) {
				t.Fatalf("owned models did not converge: %s", got)
			}
		})
	}
}

func TestPiDoesNotPoliceDuplicateKeysInsideOwnedModels(t *testing.T) {
	dir := t.TempDir()
	settings, models := filepath.Join(dir, "settings.json"), filepath.Join(dir, "models.json")
	_ = os.WriteFile(settings, []byte(`{"defaultProvider":"swobu","defaultModel":"default"}`), 0o600)
	_ = os.WriteFile(models, []byte(`{"providers":{"swobu":{"baseUrl":"http://127.0.0.1:7926/c/work","api":"openai-responses","apiKey":"swobu","models":[{"id":"default","id":"default","name":"Swobu default"}]}}}`), 0o600)
	if _, err := planPi(settings, models, testTarget(t)); err != nil {
		t.Fatalf("duplicate key inside wholly owned models was rejected: %v", err)
	}
}

func TestPiRejectsInvalidOwnedPathOrDocumentShape(t *testing.T) {
	for _, raw := range []string{
		`{"providers":[]}`,
		`{"providers":{"swobu":[]}}`,
		`{"providers":{"swobu":{"api":"openai-responses","api":"openai-completions"}}}`,
		`{"providers":`,
	} {
		dir := t.TempDir()
		settings, models := filepath.Join(dir, "settings.json"), filepath.Join(dir, "models.json")
		_ = os.WriteFile(settings, []byte(`{"defaultProvider":"swobu","defaultModel":"default"}`), 0o600)
		_ = os.WriteFile(models, []byte(raw), 0o600)
		if _, err := planPi(settings, models, testTarget(t)); err == nil {
			t.Fatalf("accepted ambiguous or invalid Pi document: %s", raw)
		}
	}
}

func TestPiSettingsMustBeStrictJSON(t *testing.T) {
	dir := t.TempDir()
	settings, models := filepath.Join(dir, "settings.json"), filepath.Join(dir, "models.json")
	_ = os.WriteFile(settings, []byte("{// invalid for Pi\n\"defaultProvider\":\"swobu\"}\n"), 0o600)
	_ = os.WriteFile(models, []byte(`{"providers":{}}`), 0o600)
	if _, err := planPi(settings, models, testTarget(t)); err == nil || !strings.Contains(err.Error(), "strict JSON") {
		t.Fatalf("error = %v", err)
	}
}

func TestPiExistingEmptyAPIKeyRequiresReplacement(t *testing.T) {
	dir := t.TempDir()
	settings, models := filepath.Join(dir, "settings.json"), filepath.Join(dir, "models.json")
	_ = os.WriteFile(settings, []byte(`{"defaultProvider":"swobu","defaultModel":"default"}`), 0o600)
	_ = os.WriteFile(models, []byte(`{"providers":{"swobu":{"baseUrl":"http://127.0.0.1:7926/c/work","api":"openai-responses","apiKey":"","models":[{"id":"default","name":"Swobu default"}]}}}`), 0o600)
	mutation, err := planPi(settings, models, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if !mutation.plan.RequiresReplace() || !slices.ContainsFunc(mutation.plan.Changes, func(change Change) bool {
		return change.Field == "API key placeholder" && change.BeforeExists
	}) {
		t.Fatalf("empty existing API key was not replacement-gated: %#v", mutation.plan.Changes)
	}
}

func TestFileAdaptersReconcileFreshProfiles(t *testing.T) {
	for _, tc := range []struct {
		name   string
		client ClientID
		envKey string
		dir    string
		path   string
	}{
		{name: "Codex", client: ClientCodex, envKey: "CODEX_HOME", dir: ".codex", path: "config.toml"},
		{name: "Claude", client: ClientClaude, envKey: "CLAUDE_CONFIG_DIR", dir: ".claude", path: "settings.json"},
		{name: "Muse", client: ClientMuse, envKey: "XDG_CONFIG_HOME", dir: ".config", path: filepath.Join("muse", "settings.json")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			configDir := filepath.Join(home, tc.dir)
			service := &Service{homeDir: func() (string, error) { return home, nil }, getenv: func(key string) string {
				if key == tc.envKey {
					return configDir
				}
				return ""
			}}
			plan, err := service.Plan(context.Background(), tc.client, testTarget(t))
			if err != nil || plan.RequiresReplace() {
				t.Fatalf("plan = %#v, error = %v", plan, err)
			}
			wantPath := filepath.Join(configDir, tc.path)
			if !slices.Equal(plan.ConfigPaths, []string{wantPath}) {
				t.Fatalf("config paths = %v, want %v", plan.ConfigPaths, []string{wantPath})
			}
			verified, err := service.Apply(context.Background(), plan)
			if err != nil || !verified.AlreadyConfigured() {
				t.Fatalf("verified = %#v, error = %v", verified, err)
			}
			if _, err := os.Stat(wantPath); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPiPreservesCredentialAndProviderMetadata(t *testing.T) {
	dir := t.TempDir()
	settings, models := filepath.Join(dir, "settings.json"), filepath.Join(dir, "models.json")
	_ = os.WriteFile(settings, []byte(`{"defaultProvider":"other","defaultModel":"old"}`), 0o600)
	_ = os.WriteFile(models, []byte(`{"providers":{"swobu":{"baseUrl":"http://127.0.0.1:7926/c/old","api":"legacy","apiKey":"user-managed","metadata":{"owner":"human"},"models":[{"id":"default","name":"My route","capabilities":{"custom":true}}]}}}`), 0o600)
	mutation, err := planPi(settings, models, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range mutation.plan.Changes {
		if change.Field == "API key placeholder" {
			t.Fatalf("existing credential was over-owned: %#v", mutation.plan.Changes)
		}
	}
	if err := mutation.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(models)
	for _, want := range []string{`"apiKey":"user-managed"`, `"owner":"human"`, `"baseUrl":"http://127.0.0.1:7926/c/work"`, `"api":"openai-responses"`} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
}

func TestPiCreatesMinimalSwobuProviderFromAnySelection(t *testing.T) {
	for _, provider := range []string{"anthropic", "openai"} {
		dir := t.TempDir()
		settings := filepath.Join(dir, "settings.json")
		models := filepath.Join(dir, "models.json")
		_ = os.WriteFile(settings, []byte(`{"defaultProvider":"`+provider+`"}`), 0o600)
		service := &Service{homeDir: os.UserHomeDir, getenv: func(key string) string {
			if key == "PI_CODING_AGENT_DIR" {
				return dir
			}
			return ""
		}}
		plan, err := service.Plan(context.Background(), ClientPi, testTarget(t))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Apply(context.Background(), plan); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(models)
		if !strings.Contains(string(got), `"swobu"`) || !strings.Contains(string(got), testTarget(t).WorkspaceURL()) {
			t.Fatalf("minimal models: %s", got)
		}
	}
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	_ = os.WriteFile(settings, []byte(`{"defaultProvider":"openai-codex"}`), 0o600)
	if _, err := planPi(settings, filepath.Join(dir, "models.json"), testTarget(t)); err != nil {
		t.Fatalf("existing selection should not gate Swobu backend: %v", err)
	}
}

func TestPiApplyRefusesChangedSelectionEvidence(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	models := filepath.Join(dir, "models.json")
	_ = os.WriteFile(settings, []byte(`{"defaultProvider":"anthropic"}`), 0o600)
	service := &Service{homeDir: func() (string, error) { return dir, nil }, getenv: func(key string) string {
		if key == "PI_CODING_AGENT_DIR" {
			return dir
		}
		return ""
	}}
	plan, err := service.Plan(context.Background(), ClientPi, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(settings, []byte(`{"defaultProvider":"openai"}`), 0o600)
	if _, err := service.Apply(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "configuration changed") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(models); !os.IsNotExist(err) {
		t.Fatalf("models was written: %v", err)
	}
}

func TestPiApplyMergesUnrelatedModelCatalogEdit(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	models := filepath.Join(dir, "models.json")
	_ = os.WriteFile(settings, []byte(`{"defaultProvider":"anthropic"}`), 0o600)
	_ = os.WriteFile(models, []byte(`{"providers":{"swobu":{"metadata":{"owner":"before"},"models":[{"id":"other","name":"Before"}]}}}`), 0o600)
	service := &Service{getenv: func(key string) string {
		if key == "PI_CODING_AGENT_DIR" {
			return dir
		}
		return ""
	}}
	plan, err := service.Plan(context.Background(), ClientPi, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(models, []byte(`{"providers":{"swobu":{"metadata":{"owner":"after"},"models":[{"id":"other","name":"Before"}]}}}`), 0o600)
	if _, err := service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(models)
	for _, want := range []string{`"owner":"after"`, `"id":"default"`} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
}

func TestSameBackendWithOldEndpointRequiresReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kilo.json")
	_ = os.WriteFile(path, []byte(`{"model":"swobu/default","provider":{"swobu":{"name":"Swobu","options":{"baseURL":"http://127.0.0.1:7926/c/old"},"models":{"default":{"name":"Swobu default","tool_call":true}}}}}`), 0o600)
	mutation, err := planKilo(path, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if !mutation.plan.RequiresReplace() {
		t.Fatalf("plan did not protect endpoint change: %#v", mutation.plan.Changes)
	}
	if len(mutation.plan.Changes) != 1 || mutation.plan.Changes[0].Field != "endpoint" {
		t.Fatalf("changes = %#v", mutation.plan.Changes)
	}
}

func TestPiApplyRefusesChangedSelectionEvenWhenEndpointBeforeValueMatches(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	models := filepath.Join(dir, "models.json")
	_ = os.WriteFile(settings, []byte(`{"defaultProvider":"anthropic"}`), 0o600)
	_ = os.WriteFile(models, []byte(`{"providers":{"anthropic":{"baseUrl":"https://same"},"openai":{"baseUrl":"https://same"}}}`), 0o600)
	service := &Service{homeDir: func() (string, error) { return dir, nil }, getenv: func(key string) string {
		if key == "PI_CODING_AGENT_DIR" {
			return dir
		}
		return ""
	}}
	plan, err := service.Plan(context.Background(), ClientPi, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(settings, []byte(`{"defaultProvider":"openai"}`), 0o600)
	if _, err := service.Apply(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "configuration changed") {
		t.Fatalf("error = %v", err)
	}
	raw, _ := os.ReadFile(models)
	if strings.Contains(string(raw), testTarget(t).WorkspaceURL()) {
		t.Fatalf("changed selection was written: %s", raw)
	}
}

func TestPiHonorsDocumentedAgentDirectoryOverride(t *testing.T) {
	dir := t.TempDir()
	service := &Service{homeDir: func() (string, error) { return t.TempDir(), nil }, getenv: func(key string) string {
		if key == "PI_CODING_AGENT_DIR" {
			return dir
		}
		return ""
	}}
	settings, models, err := service.piPaths()
	if err != nil || settings != filepath.Join(dir, "settings.json") || models != filepath.Join(dir, "models.json") {
		t.Fatalf("paths = %q %q, %v", settings, models, err)
	}
}
