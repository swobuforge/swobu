package clientconnect

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
)

type commandCall struct {
	name string
	args []string
}
type commandReply struct {
	stdout string
	code   int
	err    error
}

func commandService(t *testing.T, binary string, replies map[string]commandReply, calls *[]commandCall) *Service {
	t.Helper()
	return &Service{
		homeDir: func() (string, error) { return t.TempDir(), nil },
		getenv:  func(string) string { return "" },
		lookPath: func(name string) (string, error) {
			if name == binary {
				return "/bin/" + binary, nil
			}
			return "", os.ErrNotExist
		},
		run: func(name string, args ...string) ([]byte, int, error) {
			*calls = append(*calls, commandCall{name, append([]string(nil), args...)})
			key := strings.Join(append([]string{name}, args...), " ")
			reply, ok := replies[key]
			if !ok {
				t.Fatalf("unexpected command: %s", key)
			}
			return []byte(reply.stdout), reply.code, reply.err
		},
	}
}

func TestOpenClawDeclaresSwobuProviderThenSelectsIt(t *testing.T) {
	var calls []commandCall
	replies := map[string]commandReply{
		"openclaw config file":                              {stdout: "/path/to/config.json\n"},
		"openclaw config get agents.defaults.model.primary": {stdout: "openrouter/e2e\n"},
		"openclaw config get models.providers.swobu --json": {code: 1},
		"openclaw config get agents.defaults.models --json": {code: 1},
	}
	providerJSON := `{"api":"openai-completions","apiKey":"swobu","baseUrl":"http://127.0.0.1:7926/c/work","models":[{"id":"default","name":"Swobu default"}]}`
	replies["openclaw config set models.providers.swobu "+providerJSON+" --json"] = commandReply{}
	replies["openclaw config set agents.defaults.model.primary swobu/default"] = commandReply{}
	service := commandService(t, "openclaw", replies, &calls)
	plan, err := service.Plan(ClientOpenClaw, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Apply(plan); err != nil {
		t.Fatal(err)
	}
	want := []commandCall{{"openclaw", []string{"config", "set", "models.providers.swobu", providerJSON, "--json"}}, {"openclaw", []string{"config", "set", "agents.defaults.model.primary", "swobu/default"}}}
	if !reflect.DeepEqual(calls[len(calls)-2:], want) {
		t.Fatalf("apply calls=%#v", calls)
	}
}

func TestOpenClawConfiguredAndNixStates(t *testing.T) {
	var calls []commandCall
	replies := map[string]commandReply{
		"openclaw config file":                              {stdout: "/path/to/config.json\n"},
		"openclaw config get agents.defaults.model.primary": {stdout: "swobu/default\n"},
		"openclaw config get models.providers.swobu --json": {stdout: `{"baseUrl":"http://127.0.0.1:7926/c/work","api":"openai-completions","apiKey":"swobu","models":[{"id":"default","name":"Swobu default"}]}` + "\n"},
		"openclaw config get agents.defaults.models --json": {code: 1},
	}
	service := commandService(t, "openclaw", replies, &calls)
	plan, err := service.Plan(ClientOpenClaw, testTarget(t))
	if err != nil || !plan.AlreadyConfigured() {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	service.getenv = func(key string) string {
		if key == "OPENCLAW_NIX_MODE" {
			return "1"
		}
		return ""
	}
	clients := service.Discover(testTarget(t))
	found := false
	for _, c := range clients {
		if c.ID == ClientOpenClaw {
			found = true
		}
	}
	if !found {
		t.Fatal("Nix mode OpenClaw omitted from discovery")
	}
	if _, err := service.Plan(ClientOpenClaw, testTarget(t)); err == nil || !strings.Contains(err.Error(), "Nix mode") {
		t.Fatalf("Nix admitted: %v", err)
	}
}

func TestOpenClawDiscoverSurvivesInspectionCommandFailure(t *testing.T) {
	var calls []commandCall
	replies := map[string]commandReply{
		"openclaw config file": {code: 1, err: os.ErrPermission},
	}
	service := commandService(t, "openclaw", replies, &calls)
	clients := service.Discover(testTarget(t))
	if len(calls) != 0 {
		t.Fatalf("Discover spawned %d processes, want 0", len(calls))
	}
	found := false
	for _, c := range clients {
		if c.ID == ClientOpenClaw {
			found = true
		}
	}
	if !found {
		t.Fatalf("OpenClaw with failing inspection omitted from discovery: %v", clients)
	}
}

func TestOpenClawDiscoverySpawnsZeroProcessesAndPlanSurfacesInspectionError(t *testing.T) {
	var calls []commandCall
	replies := map[string]commandReply{
		"openclaw config file":                              {stdout: "/path/to/config.json\n"},
		"openclaw config get agents.defaults.model.primary": {code: 1, stdout: "SyntaxError: Unexpected token in JSON at position 42\n"},
	}
	service := commandService(t, "openclaw", replies, &calls)

	// 1. Discover finds OpenClaw and spawns ZERO processes
	clients := service.Discover(testTarget(t))
	if len(calls) != 0 {
		t.Fatalf("Discover spawned %d processes, want 0: %#v", len(calls), calls)
	}
	found := false
	for _, c := range clients {
		if c.ID == ClientOpenClaw {
			found = true
		}
	}
	if !found {
		t.Fatalf("OpenClaw not found in discovery: %v", clients)
	}

	// 2. Plan encounters failing config get (corrupt config / runtime failure)
	// It must surface that exact failure rather than planning missing fields
	_, err := service.Plan(ClientOpenClaw, testTarget(t))
	if err == nil {
		t.Fatal("Plan succeeded on corrupt config / failing config get")
	}
	if !strings.Contains(err.Error(), "SyntaxError") && !strings.Contains(err.Error(), "position 42") {
		t.Fatalf("Plan did not surface exact CLI failure: %v", err)
	}
	if !strings.Contains(err.Error(), "OpenClaw is not automatically wireable") || !strings.Contains(err.Error(), "Nothing changed.") {
		t.Fatalf("Plan error missing wireable / nothing changed envelope: %v", err)
	}
}

func TestOpenClawPlanSurfacesMalformedJSONError(t *testing.T) {
	var calls []commandCall
	replies := map[string]commandReply{
		"openclaw config file":                              {stdout: "/path/to/config.json\n"},
		"openclaw config get agents.defaults.model.primary": {code: 1},
		"openclaw config get agents.defaults.model":         {code: 1},
		"openclaw config get models.providers.swobu --json": {stdout: "{\ninvalid json\n"},
	}
	service := commandService(t, "openclaw", replies, &calls)
	_, err := service.Plan(ClientOpenClaw, testTarget(t))
	if err == nil {
		t.Fatal("Plan succeeded on malformed JSON")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("Plan error did not describe invalid JSON: %v", err)
	}
}

func TestOpenClawUnsetDefaultModelPlansInsertionAndAppliesPrimary(t *testing.T) {
	var calls []commandCall
	replies := map[string]commandReply{
		"openclaw config file":                              {stdout: "/path/to/config.json\n"},
		"openclaw config get agents.defaults.model.primary": {code: 1},
		"openclaw config get agents.defaults.model":         {code: 1},
		"openclaw config get models.providers.swobu --json": {code: 1},
		"openclaw config get agents.defaults.models --json": {code: 1},
	}
	providerJSON := `{"api":"openai-completions","apiKey":"swobu","baseUrl":"http://127.0.0.1:7926/c/work","models":[{"id":"default","name":"Swobu default"}]}`
	replies["openclaw config set models.providers.swobu "+providerJSON+" --json"] = commandReply{}
	replies["openclaw config set agents.defaults.model.primary swobu/default"] = commandReply{}
	service := commandService(t, "openclaw", replies, &calls)

	plan, err := service.Plan(ClientOpenClaw, testTarget(t))
	if err != nil {
		t.Fatalf("Plan with unset default model failed: %v", err)
	}
	var backendChange *Change
	for i := range plan.Changes {
		if plan.Changes[i].Field == "backend" {
			backendChange = &plan.Changes[i]
			break
		}
	}
	if backendChange == nil {
		t.Fatalf("missing backend change in plan: %#v", plan.Changes)
	}
	if backendChange.BeforeExists {
		t.Fatalf("backend change marked existing before: %#v", backendChange)
	}
	if backendChange.After != "swobu/default" {
		t.Fatalf("backend change after = %q, want swobu/default", backendChange.After)
	}

	if err := service.Apply(plan); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	want := []commandCall{
		{"openclaw", []string{"config", "set", "models.providers.swobu", providerJSON, "--json"}},
		{"openclaw", []string{"config", "set", "agents.defaults.model.primary", "swobu/default"}},
	}
	if !reflect.DeepEqual(calls[len(calls)-2:], want) {
		t.Fatalf("apply calls = %#v, want %#v", calls, want)
	}
}

func TestOpenClawReusesExistingCredentialPresentationAndMetadata(t *testing.T) {
	var calls []commandCall
	replies := map[string]commandReply{
		"openclaw config file":                              {stdout: "/path/to/config.json\n"},
		"openclaw config get agents.defaults.model.primary": {stdout: "other/model\n"},
		"openclaw config get models.providers.swobu --json": {stdout: `{"baseUrl":"http://127.0.0.1:7926/c/old","api":"legacy","apiKey":"user-managed","metadata":{"owner":"human"},"models":[{"id":"default","name":"My route","capabilities":{"custom":true}}]}` + "\n"},
		"openclaw config get agents.defaults.models --json": {code: 1},
	}
	providerJSON := `{"api":"openai-completions","apiKey":"user-managed","baseUrl":"http://127.0.0.1:7926/c/work","metadata":{"owner":"human"},"models":[{"capabilities":{"custom":true},"id":"default","name":"My route"}]}`
	replies["openclaw config set models.providers.swobu "+providerJSON+" --json"] = commandReply{}
	replies["openclaw config set agents.defaults.model.primary swobu/default"] = commandReply{}
	service := commandService(t, "openclaw", replies, &calls)
	plan, err := service.Plan(ClientOpenClaw, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range plan.Changes {
		if change.Field == "API key placeholder" || change.Field == "default model" {
			t.Fatalf("existing credential/model was over-owned: %#v", plan.Changes)
		}
	}
	if err := service.Apply(plan); err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, call := range calls {
		joined += strings.Join(call.args, " ") + "\n"
	}
	for _, want := range []string{`"apiKey":"user-managed"`, `"name":"My route"`, `"owner":"human"`, `"custom":true`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q:\n%s", want, joined)
		}
	}
}

func TestOpenClawApplyMergesUnrelatedProviderAndAllowlistEdits(t *testing.T) {
	var calls []commandCall
	replies := map[string]commandReply{
		"openclaw config file":                              {stdout: "/path/to/config.json\n"},
		"openclaw config get agents.defaults.model.primary": {stdout: "openrouter/e2e\n"},
		"openclaw config get models.providers.swobu --json": {stdout: `{"baseUrl":"https://old","api":"openai-completions","apiKey":"swobu","metadata":{"owner":"before"},"models":[{"id":"default","name":"Swobu default"}]}` + "\n"},
		"openclaw config get agents.defaults.models --json": {stdout: `{"other/model":{}}` + "\n"},
	}
	service := commandService(t, "openclaw", replies, &calls)
	plan, err := service.Plan(ClientOpenClaw, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}

	// Unrelated edits arrive between Plan and Apply:
	replies["openclaw config get models.providers.swobu --json"] = commandReply{
		stdout: `{"baseUrl":"https://old","api":"openai-completions","apiKey":"swobu","metadata":{"owner":"after"},"models":[{"id":"default","name":"Swobu default"},{"id":"extra","name":"Extra"}]}` + "\n",
	}
	replies["openclaw config get agents.defaults.models --json"] = commandReply{
		stdout: `{"other/model":{},"human/model":{}}` + "\n",
	}

	providerJSON := `{"api":"openai-completions","apiKey":"swobu","baseUrl":"http://127.0.0.1:7926/c/work","metadata":{"owner":"after"},"models":[{"id":"default","name":"Swobu default"},{"id":"extra","name":"Extra"}]}`
	allowlistJSON := `{"human/model":{},"other/model":{},"swobu/default":{}}`
	replies["openclaw config set models.providers.swobu "+providerJSON+" --json"] = commandReply{}
	replies["openclaw config set agents.defaults.models "+allowlistJSON+" --json"] = commandReply{}
	replies["openclaw config set agents.defaults.model.primary swobu/default"] = commandReply{}
	if err := service.Apply(plan); err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, call := range calls {
		joined += strings.Join(call.args, " ") + "\n"
	}
	for _, want := range []string{`"owner":"after"`, `"id":"extra"`, `"human/model"`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("fresh edit missing %q:\n%s", want, joined)
		}
	}
}

func TestHermesDeclaresCustomBackendWithOneAtomicFileReplacement(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	unrelatedPrefix := "# human header\nkeep:  [one,  two] # spacing stays\n"
	unrelatedSuffix := "\nother:\n    nested:   'quoted' # untouched\n"
	original := unrelatedPrefix + "model:\n    provider: openrouter # provider comment\n    default: vendor/model\n    base_url: https://openrouter.ai/api/v1\n" + unrelatedSuffix
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls []commandCall
	replies := map[string]commandReply{
		"hermes config get model --json": {stdout: `{"provider":"openrouter","default":"vendor/model","base_url":"https://openrouter.ai/api/v1"}`},
		"hermes config path":             {stdout: path + "\n"},
	}
	service := commandService(t, "hermes", replies, &calls)
	plan, err := service.Plan(ClientHermes, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Apply(plan); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 4 { // Plan and Apply each inspect model and path; no write command.
		t.Fatalf("calls=%#v", calls)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"    provider: custom # provider comment", "    default: default", "    base_url: http://127.0.0.1:7926/c/work", unrelatedPrefix, unrelatedSuffix} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
}

func TestHermesRefusesUnsupportedYAMLSyntaxWithoutChange(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{name: "flow-style model", raw: "model: {provider: openrouter, default: gpt-4o, base_url: https://example.com}\nkeep: true\n"},
		{name: "literal owned scalar", raw: "model:\n  provider: openrouter\n  default: gpt-4o\n  base_url: |\n    https://example.com\nkeep: true\n"},
		{name: "folded owned scalar", raw: "model:\n  provider: openrouter\n  default: >\n    gpt-4o\n  base_url: https://example.com\n"},
		{name: "duplicate top-level model", raw: "model:\n  provider: openrouter\n  default: gpt-4o\n  base_url: https://example.com\nmodel:\n  provider: custom\n  default: other\n  base_url: https://other.example\n"},
		{name: "duplicate owned key", raw: "model:\n  provider: openrouter\n  provider: custom\n  default: gpt-4o\n  base_url: https://example.com\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := dir + "/config.yaml"
			if err := os.WriteFile(path, []byte(tc.raw), 0o600); err != nil {
				t.Fatal(err)
			}
			var calls []commandCall
			replies := map[string]commandReply{
				"hermes config get model --json": {stdout: `{"provider":"openrouter","default":"gpt-4o","base_url":"https://example.com"}`},
				"hermes config path":             {stdout: path + "\n"},
			}
			service := commandService(t, "hermes", replies, &calls)
			if _, err := service.Plan(ClientHermes, testTarget(t)); err == nil || !strings.Contains(err.Error(), "Nothing changed") {
				t.Fatalf("error = %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != tc.raw {
				t.Fatalf("source changed: %v\n%s", err, got)
			}
		})
	}
}

func TestHermesPreservesCRLFInChangedAndInsertedLines(t *testing.T) {
	raw := []byte("model:\r\n  provider: 'openrouter'\r\n  default: \"gpt-4o\"\r\nkeep: true\r\n")
	next, err := replaceHermesModel(raw, testTarget(t).WorkspaceURL())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(next, []byte("\n")) && bytes.Contains(bytes.ReplaceAll(next, []byte("\r\n"), nil), []byte("\n")) {
		t.Fatalf("introduced LF line endings:\n%q", next)
	}
	for _, want := range []string{"  provider: 'custom'\r\n", "  default: \"default\"\r\n", "  base_url: http://127.0.0.1:7926/c/work\r\n", "keep: true\r\n"} {
		if !bytes.Contains(next, []byte(want)) {
			t.Fatalf("missing %q:\n%q", want, next)
		}
	}
}

func TestHermesInsertionAtEOFFirstTerminatesExistingLine(t *testing.T) {
	raw := []byte("model:\n  provider: openrouter\n  default: gpt-4o")
	next, err := replaceHermesModel(raw, testTarget(t).WorkspaceURL())
	if err != nil {
		t.Fatal(err)
	}
	want := "model:\n  provider: custom\n  default: default\n  base_url: http://127.0.0.1:7926/c/work\n"
	if string(next) != want {
		t.Fatalf("next:\n%q\nwant:\n%q", next, want)
	}
}

func TestOpenClawPlanningFailurePreservesDetailedErrorMessage(t *testing.T) {
	var calls []commandCall
	replies := map[string]commandReply{
		"openclaw config file": {code: 1, stdout: "SyntaxError: Unexpected token in JSON at position 42"},
	}
	service := commandService(t, "openclaw", replies, &calls)
	_, err := service.Plan(ClientOpenClaw, testTarget(t))
	if err == nil {
		t.Fatal("expected planning error")
	}
	want := "SyntaxError: Unexpected token in JSON at position 42"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain detail %q", err.Error(), want)
	}
}
