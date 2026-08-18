package clientconnect

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func testTarget(t *testing.T) Target {
	t.Helper()
	target, err := NewTarget("work", "http://127.0.0.1:7926/c/work")
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func codexServiceForPath(path string) *Service {
	return &Service{
		homeDir: func() (string, error) { return filepath.Dir(filepath.Dir(path)), nil },
		getenv: func(key string) string {
			if key == "CODEX_HOME" {
				return filepath.Dir(path)
			}
			return ""
		},
		lookPath: func(string) (string, error) { return "", os.ErrNotExist },
	}
}

func TestNewTargetCanonicalizesWorkspaceURLAndClassifiesLoopback(t *testing.T) {
	target := testTarget(t)
	if target.WorkspaceURL() != "http://127.0.0.1:7926/c/work" || !target.IsLocal() {
		t.Fatalf("target = %#v", target)
	}
	remote, err := NewTarget("work", "https://swobu.example/c/work")
	if err != nil || remote.IsLocal() {
		t.Fatalf("remote target = %#v, %v", remote, err)
	}
}

func TestNewTargetAcceptsRootOrV1AndDerivesOneCanonicalSuffix(t *testing.T) {
	for _, input := range []string{
		"http://127.0.0.1:7926/c/work",
		"http://127.0.0.1:7926/c/work/",
		"http://127.0.0.1:7926/c/work/v1",
		"http://127.0.0.1:7926/c/work/v1/",
	} {
		target, err := NewTarget("work", input)
		if err != nil {
			t.Fatalf("NewTarget(%q): %v", input, err)
		}
		if target.WorkspaceURL() != "http://127.0.0.1:7926/c/work" {
			t.Fatalf("NewTarget(%q) = %#v", input, target)
		}
	}
	for _, suffix := range []string{"/v1/v1", "/models", "/responses", "/messages", "/chat/completions"} {
		if _, err := NewTarget("work", "http://127.0.0.1:7926/c/work"+suffix); err == nil {
			t.Fatalf("NewTarget accepted operation/version suffix %q", suffix)
		}
	}
	for _, input := range []string{
		"ftp://127.0.0.1/c/work",
		"http://user@127.0.0.1/c/work",
		"http://127.0.0.1/c/work?version=v1",
		"http://127.0.0.1/c/work#v1",
	} {
		if _, err := NewTarget("work", input); err == nil {
			t.Fatalf("NewTarget accepted non-base address %q", input)
		}
	}
}

func TestCanonicalWorkspaceURLComposesWithAcceptedVersionedAndUnversionedOperations(t *testing.T) {
	target, err := NewTarget("work", "http://127.0.0.1:7926/c/work/v1")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		url  string
		want canonical.NormalizedPath
	}{
		{name: "Responses", url: target.WorkspaceURL() + "/responses", want: canonical.NormalizedPathResponses},
		{name: "versioned Responses", url: target.WorkspaceURL() + "/v1/responses", want: canonical.NormalizedPathResponses},
		{name: "Chat Completions", url: target.WorkspaceURL() + "/chat/completions", want: canonical.NormalizedPathChatCompletions},
		{name: "versioned Chat Completions", url: target.WorkspaceURL() + "/v1/chat/completions", want: canonical.NormalizedPathChatCompletions},
		{name: "Anthropic Messages", url: target.WorkspaceURL() + "/v1/messages", want: canonical.NormalizedPathMessages},
		{name: "bare Messages", url: target.WorkspaceURL() + "/messages", want: canonical.NormalizedPathMessages},
		{name: "Models", url: target.WorkspaceURL() + "/v1/models", want: canonical.NormalizedPathModels},
		{name: "bare Models", url: target.WorkspaceURL() + "/models", want: canonical.NormalizedPathModels},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := url.Parse(tc.url)
			if err != nil {
				t.Fatal(err)
			}
			operationPath := strings.TrimPrefix(parsed.EscapedPath(), "/c/work")
			got, err := canonical.NormalizePath(operationPath)
			if err != nil {
				t.Fatalf("NormalizePath(%q): %v", operationPath, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizePath(%q) = %q, want %q", operationPath, got, tc.want)
			}
		})
	}
}

func TestCodexDeclaresSwobuProviderAndPreservesForeignState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := "# keep this comment\nmodel = \"gpt-test\"\nopenai_base_url = \"https://legacy.example\"\n\n[profiles.work]\nmodel_provider = \"openai\"\n"
	if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	service := codexServiceForPath(path)
	plan, err := service.Plan(ClientCodex, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Apply(plan); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	got := string(raw)
	for _, want := range []string{`model = "default"`, `model_provider = "swobu"`, `[model_providers.swobu]`, `name = "Swobu"`, `base_url = "http://127.0.0.1:7926/c/work"`, `wire_api = "responses"`, `openai_base_url = "https://legacy.example"`, `[profiles.work]`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestCodexReusesExistingSwobuProviderAndPreservesMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := "model = \"old\"\nmodel_provider = \"other\"\n[model_providers.swobu]\nname = \"Old\"\nbase_url = \"https://old.example\"\nwire_api = \"chat\"\nexperimental_bearer_token = \"keep\"\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	service := codexServiceForPath(path)
	plan, err := service.Plan(ClientCodex, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Apply(plan); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	for _, want := range []string{`model = "default"`, `model_provider = "swobu"`, `name = "Old"`, `base_url = "http://127.0.0.1:7926/c/work"`, `wire_api = "responses"`, `experimental_bearer_token = "keep"`} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
}

func TestCodexRejectsInvalidInputWithoutChange(t *testing.T) {
	for _, fixture := range [][]byte{[]byte("[broken"), {0xff, 0xfe}} {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(path, fixture, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := planCodex(path, testTarget(t)); err == nil || !strings.Contains(err.Error(), "Nothing changed") {
			t.Fatalf("error = %v", err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != string(fixture) {
			t.Fatal("invalid Codex source changed")
		}
	}
}

func TestClaudePlanChangesOnlyOwnedSemanticLeaves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte(`{"permissions":{"allow":["Read"]},"env":{"KEEP":"yes","ANTHROPIC_BASE_URL":"https://old.example"}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	service := &Service{homeDir: os.UserHomeDir, getenv: func(key string) string {
		if key == "CLAUDE_CONFIG_DIR" {
			return dir
		}
		return ""
	}}
	plan, err := service.Plan(ClientClaude, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Apply(plan); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	var before, after map[string]any
	_ = json.Unmarshal(original, &before)
	_ = json.Unmarshal(got, &after)
	before["env"].(map[string]any)["ANTHROPIC_BASE_URL"] = testTarget(t).WorkspaceURL()
	before["env"].(map[string]any)["CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY"] = "1"
	if !deepJSONEqual(before, after) {
		t.Fatalf("semantic delta exceeded owned leaves: %s", got)
	}
}

func TestClaudePlanPreservesEverySourceByteOutsideOwnedString(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte("{\n  \"large\": 9007199254740993123456789,\n  \"env\" : { \"KEEP\" : true, \"ANTHROPIC_BASE_URL\" : \"https://old.example\" },\n  \"ordered\": [3, 2, 1]\n}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	service := &Service{homeDir: os.UserHomeDir, getenv: func(key string) string {
		if key == "CLAUDE_CONFIG_DIR" {
			return dir
		}
		return ""
	}}
	plan, err := service.Plan(ClientClaude, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(strings.Replace(string(original), "https://old.example", testTarget(t).WorkspaceURL(), 1))
	want = []byte(strings.Replace(string(want), " },", `,"CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY":"1" },`, 1))
	if err := service.Apply(plan); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, want) {
		t.Fatalf("Claude patch rewrote bytes outside owned string:\n got: %s\nwant: %s", got, want)
	}
}

func TestClaudeRejectsIncompatibleEnvWithoutChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte(`{"env":"wrong","keep":true}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := planClaude(path, testTarget(t)); err == nil || !strings.Contains(err.Error(), "Nothing changed") {
		t.Fatalf("error = %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(original) {
		t.Fatal("Claude source changed")
	}
}

func TestClaudeEndpointLeafDeterminesApplyOrReplaceGrammar(t *testing.T) {
	for _, tc := range []struct {
		name          string
		raw           []byte
		wantOverwrite bool
	}{
		{name: "new leaves apply", raw: []byte(`{"env":{"KEEP":"yes"}}`)},
		{name: "existing endpoint replaces", raw: []byte(`{"env":{"ANTHROPIC_BASE_URL":"https://old.example"}}`), wantOverwrite: true},
		{name: "disabled discovery replaces", raw: []byte(`{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:7926/c/work","CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY":"0"}}`), wantOverwrite: true},
		{name: "enabled discovery and endpoint are unchanged", raw: []byte(`{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:7926/c/work","CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY":"1"}}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "settings.json")
			if err := os.WriteFile(path, tc.raw, 0o600); err != nil {
				t.Fatal(err)
			}
			mutation, err := planClaude(path, testTarget(t))
			if err != nil {
				t.Fatal(err)
			}
			if mutation.plan.RequiresReplace() != tc.wantOverwrite {
				t.Fatalf("RequiresReplace = %v, want %v", mutation.plan.RequiresReplace(), tc.wantOverwrite)
			}
		})
	}
}

func TestClaudeDiscoverySettingUsesExistingReplacementSafeguard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte(`{"env":{"KEEP":"yes","ANTHROPIC_BASE_URL":"http://127.0.0.1:7926/c/work","CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY":"0"}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	service := &Service{homeDir: os.UserHomeDir, getenv: func(key string) string {
		if key == "CLAUDE_CONFIG_DIR" {
			return dir
		}
		return ""
	}}
	plan, err := service.Plan(ClientClaude, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if !plan.RequiresReplace() || len(plan.Changes) != 1 || plan.Changes[0].Field != "model discovery" {
		t.Fatalf("plan changes = %#v, want one replacement-gated discovery change", plan.Changes)
	}
	if err := service.Apply(plan); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := []byte(strings.Replace(string(original), `"CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY":"0"`, `"CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY":"1"`, 1))
	if !bytes.Equal(got, want) {
		t.Fatalf("Claude discovery patch changed unrelated source bytes:\n got: %s\nwant: %s", got, want)
	}
}

func TestClaudeRejectsNonStringOwnedEndpointWithoutChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte(`{"env":{"ANTHROPIC_BASE_URL":42,"KEEP":"yes"}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := planClaude(path, testTarget(t)); err == nil || !strings.Contains(err.Error(), "Nothing changed") {
		t.Fatalf("error = %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(original) {
		t.Fatal("incompatible Claude endpoint changed")
	}
}

func TestClaudeRejectsNonStringOwnedDiscoverySettingWithoutChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte(`{"env":{"ANTHROPIC_BASE_URL":"https://old.example","CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY":false,"KEEP":"yes"}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := planClaude(path, testTarget(t)); err == nil || !strings.Contains(err.Error(), "Nothing changed") {
		t.Fatalf("error = %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(original) {
		t.Fatal("incompatible Claude discovery setting changed")
	}
}

func TestApplyRefusesChangedCodexBackendEvidence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("model = \"keep\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := codexServiceForPath(path)
	plan, err := service.Plan(ClientCodex, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	newer := []byte("model = \"newer\"\n# human edit\n")
	if err := os.WriteFile(path, newer, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.Apply(plan); err == nil {
		t.Fatal("changed owned selection accepted")
	}
}

func TestApplyRefusesChangedCodexSwobuEndpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("model = \"default\"\nmodel_provider = \"swobu\"\n[model_providers.swobu]\nname=\"Swobu\"\nbase_url=\"https://old.example\"\nwire_api=\"responses\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := codexServiceForPath(path)
	plan, err := service.Plan(ClientCodex, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	newer := []byte("model = \"default\"\nmodel_provider = \"swobu\"\n[model_providers.swobu]\nname=\"Swobu\"\nbase_url=\"https://new.example\"\nwire_api=\"responses\"\n")
	if err := os.WriteFile(path, newer, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.Apply(plan); err == nil || !strings.Contains(err.Error(), "Open Connect again") {
		t.Fatalf("error = %v", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, newer) {
		t.Fatalf("same-field human edit overwritten: %s", got)
	}
}

func TestApplySucceedsWhenCodexAlreadyConfigured(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("model = \"keep\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := codexServiceForPath(path)
	plan, err := service.Plan(ClientCodex, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	already := []byte("model = \"default\"\nmodel_provider = \"swobu\"\n[model_providers.swobu]\nname=\"Swobu\"\nbase_url=\"http://127.0.0.1:7926/c/work\"\nwire_api=\"responses\"\n")
	if err := os.WriteFile(path, already, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.Apply(plan); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, already) {
		t.Fatalf("already-configured source changed: %s", got)
	}
}

func TestApplyReplansReviewedCodexConfiguredState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	configured := []byte("model=\"default\"\nmodel_provider=\"swobu\"\n[model_providers.swobu]\nname=\"Swobu\"\nbase_url=\"http://127.0.0.1:7926/c/work\"\nwire_api=\"responses\"\n")
	if err := os.WriteFile(path, configured, 0o600); err != nil {
		t.Fatal(err)
	}
	service := codexServiceForPath(path)
	plan, err := service.Plan(ClientCodex, testTarget(t))
	if err != nil || !plan.AlreadyConfigured() {
		t.Fatalf("plan = %#v, %v", plan, err)
	}
	newer := []byte("model=\"default\"\nmodel_provider=\"swobu\"\n[model_providers.swobu]\nname=\"Swobu\"\nbase_url=\"https://new.example\"\nwire_api=\"responses\"\n")
	if err := os.WriteFile(path, newer, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.Apply(plan); err == nil || !strings.Contains(err.Error(), "Open Connect again") {
		t.Fatalf("error = %v", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, newer) {
		t.Fatalf("changed endpoint overwritten: %s", got)
	}
}

func TestSymlinkedConfigMutatesTargetWithoutReplacingLink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.toml")
	link := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(real, []byte("model = \"keep\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	service := codexServiceForPath(link)
	plan, err := service.Plan(ClientCodex, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Apply(plan); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("logical symlink was replaced")
	}
}

func TestApplyRefusesRetargetedLogicalSymlinkWithChangedSelection(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.toml")
	second := filepath.Join(dir, "second.toml")
	link := filepath.Join(dir, "config.toml")
	firstRaw := []byte("model = \"first\"\n")
	secondRaw := []byte("model = \"second\"\n")
	if err := os.WriteFile(first, firstRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, secondRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(first, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	service := codexServiceForPath(link)
	plan, err := service.Plan(ClientCodex, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, link); err != nil {
		t.Fatal(err)
	}

	if err := service.Apply(plan); err == nil {
		t.Fatal("retargeted changed selection accepted")
	}
	gotFirst, _ := os.ReadFile(first)
	gotSecond, _ := os.ReadFile(second)
	if !bytes.Equal(gotFirst, firstRaw) || !bytes.Equal(gotSecond, secondRaw) {
		t.Fatalf("first=%q second=%q", gotFirst, gotSecond)
	}
}

func TestPlanRejectsForeignConfigurationLargerThan16MiB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	oversized := bytes.Repeat([]byte("#"), 16<<20+1)
	if err := os.WriteFile(path, oversized, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := planCodex(path, testTarget(t)); err == nil || !strings.Contains(err.Error(), "16 MiB") {
		t.Fatalf("oversized plan error = %v", err)
	}
}

func TestPlanRejectsNonRegularForeignConfiguration(t *testing.T) {
	dir := t.TempDir()
	if _, err := planCodex(dir, testTarget(t)); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory plan error = %v", err)
	}
}

func TestDiscoveryUsesBinaryOrResolvedConfiguration(t *testing.T) {
	dir := t.TempDir()
	service := &Service{homeDir: func() (string, error) { return dir, nil }, getenv: func(string) string { return "" }, lookPath: func(name string) (string, error) {
		if name == "codex" {
			return "/bin/codex", nil
		}
		return "", os.ErrNotExist
	}}
	if err := os.Mkdir(filepath.Join(dir, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	clients := service.Discover(testTarget(t))
	if len(clients) != 1 || clients[0].ID != ClientCodex {
		t.Fatalf("clients = %#v", clients)
	}
}

func TestPlanRejectsRemoteAndMalformedTargetsBeforeInspection(t *testing.T) {
	called := false
	service := &Service{
		homeDir: func() (string, error) { called = true; return t.TempDir(), nil },
		getenv:  func(string) string { called = true; return "" },
	}
	remote, err := NewTarget("work", "https://swobu.example/c/work")
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []Target{remote, {}, {workspaceSlug: "work", workspaceURL: "not-canonical", local: true}} {
		if _, err := service.Plan(ClientCodex, target); err == nil || !strings.Contains(err.Error(), "loopback") {
			t.Fatalf("Plan(%#v) error = %v", target, err)
		}
	}
	if called {
		t.Fatal("rejected target reached foreign configuration inspection")
	}
}

func TestApplyRejectsPlanWithoutConstructorValidatedLocalTarget(t *testing.T) {
	service := &Service{}
	if err := service.Apply(Plan{ClientID: ClientCodex}); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("Apply error = %v", err)
	}
}

func TestApplyRejectsEveryChangedExportedReviewedField(t *testing.T) {
	mutations := map[string]struct {
		mutate       func(Plan) Plan
		appliesValue string
	}{
		"client ID":   {mutate: func(p Plan) Plan { p.ClientID = ClientClaude; return p }},
		"client name": {mutate: func(p Plan) Plan { p.ClientName = "Changed"; return p }},
		"config path": {mutate: func(p Plan) Plan { p.ConfigPath += ".other"; return p }},
		"changes": {mutate: func(p Plan) Plan {
			p.Changes = append([]Change(nil), p.Changes...)
			p.Changes[0].Before = "https://review-tampered"
			return p
		}},
		"target": {mutate: func(p Plan) Plan {
			p.Target, _ = NewTarget("other", "http://127.0.0.1:7926/c/other")
			return p
		}},
	}
	for name, tc := range mutations {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			original := []byte("openai_base_url = \"https://old\"\n")
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			service := codexServiceForPath(path)
			plan, err := service.Plan(ClientCodex, testTarget(t))
			if err != nil {
				t.Fatal(err)
			}
			err = service.Apply(tc.mutate(plan))
			if tc.appliesValue == "" && err == nil {
				t.Fatal("changed reviewed field was invisibly accepted")
			}
			if tc.appliesValue != "" && err != nil {
				t.Fatalf("changed reviewed target was not used as current semantics: %v", err)
			}
			got, err := os.ReadFile(path)
			if tc.appliesValue != "" {
				if err != nil || !strings.Contains(string(got), tc.appliesValue) {
					t.Fatalf("changed target did not drive mutation: %v\n%s", err, got)
				}
				return
			}
			if err != nil || !bytes.Equal(got, original) {
				t.Fatalf("rejected review changed file: %v\n%s", err, got)
			}
		})
	}
}

func deepJSONEqual(left, right any) bool {
	l, _ := json.Marshal(left)
	r, _ := json.Marshal(right)
	return string(l) == string(r)
}
