package clientconnect

import (
	"bytes"
	"os"
	"path/filepath"
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
	adapters = append(append([]adapter(nil), adapters...), adapter{id: fifth, name: "Fifth", present: alwaysPresent, planCurrent: func(_ *Service, target Target) (plannedMutation, error) {
		return plannedMutation{plan: Plan{ClientID: fifth, ClientName: "Fifth", Target: target}}, nil
	}})
	ids := AutomaticClientIDs()
	if ids[len(ids)-1] != fifth {
		t.Fatalf("IDs = %v", ids)
	}
	if parsed, err := ParseClientID("fifth"); err != nil || parsed != fifth {
		t.Fatalf("Parse = %q, %v", parsed, err)
	}
	plan, err := (&Service{}).Plan(fifth, testTarget(t))
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
	plan, err := service.Plan(ClientKilo, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsoncPath, []byte(`{"model":"anthropic/model"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.Apply(plan); err == nil || !strings.Contains(err.Error(), "Open Connect again") {
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
	plan, err := service.Plan(ClientKilo, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	alreadyConfigured := []byte(`{"model":"anthropic/model","provider":{"anthropic":{"options":{"baseURL":"` + testTarget(t).WorkspaceURL() + `"}}}}`)
	if err := os.WriteFile(jsoncPath, alreadyConfigured, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.Apply(plan); err == nil {
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
		{id: "broken-presence", name: "Broken presence", present: func(*Service) (bool, error) { return false, os.ErrPermission }, planCurrent: func(*Service, Target) (plannedMutation, error) {
			t.Fatal("plan after presence error")
			return plannedMutation{}, nil
		}},
		{id: "safe", name: "Safe", present: alwaysPresent, planCurrent: func(_ *Service, target Target) (plannedMutation, error) {
			return plannedMutation{plan: Plan{ClientID: "safe", ClientName: "Safe", Target: target}}, nil
		}},
		{id: "broken-plan", name: "Broken plan", present: alwaysPresent, planCurrent: func(*Service, Target) (plannedMutation, error) { return plannedMutation{}, os.ErrPermission }},
	}
	clients := (&Service{}).Discover(testTarget(t))
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
			planCurrent: func(*Service, Target) (plannedMutation, error) {
				panic("planCurrent must not be called during Discover")
			},
		},
	}
	clients := (&Service{}).Discover(testTarget(t))
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
	if _, err := service.Plan(ClientKilo, testTarget(t)); err != nil {
		t.Fatalf("slash model rejected: %v", err)
	}
	service.getenv = func(key string) string {
		if key == "KILO_CONFIG_DIR" {
			return "/tmp/other"
		}
		return ""
	}
	if _, err := service.Plan(ClientKilo, testTarget(t)); err == nil || !strings.Contains(err.Error(), "KILO_CONFIG_DIR") {
		t.Fatalf("override error = %v", err)
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
		ClientID: ClientCodex, ClientName: "Codex CLI", ConfigPath: "/tmp/config",
		Target: target, Changes: []Change{{Field: "endpoint", Before: "https://old", BeforeExists: true, After: target.WorkspaceURL()}},
	}
	mutations := map[string]func(Plan) Plan{
		"client ID":   func(p Plan) Plan { p.ClientID = ClientClaude; return p },
		"client name": func(p Plan) Plan { p.ClientName = "Changed"; return p },
		"config path": func(p Plan) Plan { p.ConfigPath = "/tmp/other"; return p },
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
	plan, err := service.Plan(ClientKilo, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Apply(plan); err != nil {
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
	if err := mutation.apply(); err != nil {
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
		plan, err := service.Plan(ClientKilo, testTarget(t))
		if err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
		if err := service.Apply(plan); err != nil {
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
	clients := service.Discover(testTarget(t))
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
	plan, err := service.Plan(ClientKilo, testTarget(t))
	if err != nil || plan.ConfigPath != jsonPath {
		t.Fatalf("plan = %#v, %v", plan, err)
	}
	jsoncPath := filepath.Join(dir, "kilo.jsonc")
	_ = os.WriteFile(jsoncPath, []byte(`{"model":"anthropic/model"}`), 0o600)
	plan, err = service.Plan(ClientKilo, testTarget(t))
	if err != nil || plan.ConfigPath != jsoncPath {
		t.Fatalf("precedence plan = %#v, %v", plan, err)
	}
	service.getenv = func(key string) string {
		if key == "KILO_PROVIDER" {
			return "openai"
		}
		return ""
	}
	if _, err := service.Plan(ClientKilo, testTarget(t)); err == nil {
		t.Fatal("KILO_PROVIDER override admitted")
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
	plan, err := service.Plan(ClientPi, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Apply(plan); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(models)
	text := string(got)
	for _, want := range []string{`"swobu"`, `"baseUrl":"http://127.0.0.1:7926/c/work"`, `"api":"openai-completions"`, `"apiKey":"swobu"`, `"id":"default"`, `"baseUrl": "https://old"`, `"other": {"models": [1,2,3]}`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
	settingsRaw, _ := os.ReadFile(settings)
	if !strings.Contains(string(settingsRaw), `"defaultProvider":"swobu"`) || !strings.Contains(string(settingsRaw), `"defaultModel":"default"`) {
		t.Fatalf("settings=%s", settingsRaw)
	}
}

func TestPiReusesExistingSwobuCredentialPresentationAndMetadata(t *testing.T) {
	dir := t.TempDir()
	settings, models := filepath.Join(dir, "settings.json"), filepath.Join(dir, "models.json")
	_ = os.WriteFile(settings, []byte(`{"defaultProvider":"other","defaultModel":"old"}`), 0o600)
	_ = os.WriteFile(models, []byte(`{"providers":{"swobu":{"baseUrl":"http://127.0.0.1:7926/c/old","api":"legacy","apiKey":"user-managed","metadata":{"owner":"human"},"models":[{"id":"default","name":"My route","capabilities":{"custom":true}}]}}}`), 0o600)
	mutation, err := planPi(settings, models, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range mutation.plan.Changes {
		if change.Field == "API key placeholder" || change.Field == "default model" {
			t.Fatalf("existing credential/model was over-owned: %#v", mutation.plan.Changes)
		}
	}
	if err := mutation.apply(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(models)
	for _, want := range []string{`"apiKey":"user-managed"`, `"name":"My route"`, `"owner":"human"`, `"custom":true`, `"baseUrl":"http://127.0.0.1:7926/c/work"`, `"api":"openai-completions"`} {
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
		plan, err := service.Plan(ClientPi, testTarget(t))
		if err != nil {
			t.Fatal(err)
		}
		if err := service.Apply(plan); err != nil {
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
	plan, err := service.Plan(ClientPi, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(settings, []byte(`{"defaultProvider":"openai"}`), 0o600)
	if err := service.Apply(plan); err == nil || !strings.Contains(err.Error(), "configuration changed") {
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
	plan, err := service.Plan(ClientPi, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(models, []byte(`{"providers":{"swobu":{"metadata":{"owner":"after"},"models":[{"id":"other","name":"After"}]}}}`), 0o600)
	if err := service.Apply(plan); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(models)
	for _, want := range []string{`"owner":"after"`, `"id":"other"`, `"name":"After"`, `"id":"default"`} {
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
	plan, err := service.Plan(ClientPi, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(settings, []byte(`{"defaultProvider":"openai"}`), 0o600)
	if err := service.Apply(plan); err == nil || !strings.Contains(err.Error(), "configuration changed") {
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
