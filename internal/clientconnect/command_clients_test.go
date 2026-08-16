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
	return &Service{getenv: func(string) string { return "" }, lookPath: func(name string) (string, error) {
		if name == binary {
			return "/bin/" + binary, nil
		}
		return "", os.ErrNotExist
	}, run: func(name string, args ...string) ([]byte, int, error) {
		*calls = append(*calls, commandCall{name, append([]string(nil), args...)})
		key := strings.Join(append([]string{name}, args...), " ")
		reply, ok := replies[key]
		if !ok {
			t.Fatalf("unexpected command: %s", key)
		}
		return []byte(reply.stdout), reply.code, reply.err
	}}
}

func TestOpenClawDeclaresSwobuProviderThenSelectsIt(t *testing.T) {
	var calls []commandCall
	replies := map[string]commandReply{
		"openclaw config get agents.defaults.model.primary --json": {stdout: `"openrouter/e2e"`},
		"openclaw config file": {stdout: "/tmp/openclaw.json\n"},
		"openclaw config get models.providers.swobu.baseUrl --json": {code: 1},
		"openclaw config get models.providers.swobu --json":         {code: 1},
		"openclaw config get agents.defaults.models --json":         {code: 1},
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
		"openclaw config get agents.defaults.model.primary --json": {stdout: `"swobu/default"`},
		"openclaw config file": {stdout: "/tmp/openclaw.json\n"},
		"openclaw config get models.providers.swobu.baseUrl --json": {stdout: `"http://127.0.0.1:7926/c/work"`},
		"openclaw config get models.providers.swobu --json":         {stdout: `{"baseUrl":"http://127.0.0.1:7926/c/work","api":"openai-completions","apiKey":"swobu","models":[{"id":"default","name":"Swobu default"}]}`},
		"openclaw config get agents.defaults.models --json":         {code: 1},
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
	if _, err := service.Plan(ClientOpenClaw, testTarget(t)); err == nil {
		t.Fatal("Nix admitted")
	}
}

func TestOpenClawReusesExistingCredentialPresentationAndMetadata(t *testing.T) {
	var calls []commandCall
	replies := map[string]commandReply{
		"openclaw config get agents.defaults.model.primary --json": {stdout: `"other/model"`},
		"openclaw config file": {stdout: "/tmp/openclaw.json\n"},
		"openclaw config get models.providers.swobu.baseUrl --json": {stdout: `"http://127.0.0.1:7926/c/old"`},
		"openclaw config get models.providers.swobu --json":         {stdout: `{"baseUrl":"http://127.0.0.1:7926/c/old","api":"legacy","apiKey":"user-managed","metadata":{"owner":"human"},"models":[{"id":"default","name":"My route","capabilities":{"custom":true}}]}`},
		"openclaw config get agents.defaults.models --json":         {code: 1},
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
		"openclaw config get agents.defaults.model.primary --json": {stdout: `"openrouter/e2e"`},
		"openclaw config file": {stdout: "/tmp/openclaw.json\n"},
		"openclaw config get models.providers.swobu.baseUrl --json": {stdout: `"https://old"`},
		"openclaw config get models.providers.swobu --json":         {stdout: `{"baseUrl":"https://old","api":"openai-completions","apiKey":"swobu","metadata":{"owner":"before"},"models":[{"id":"default","name":"Swobu default"}]}`},
		"openclaw config get agents.defaults.models --json":         {stdout: `{"other/model":{}}`},
	}
	service := commandService(t, "openclaw", replies, &calls)
	plan, err := service.Plan(ClientOpenClaw, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	replies["openclaw config get models.providers.swobu --json"] = commandReply{stdout: `{"baseUrl":"https://old","api":"openai-completions","apiKey":"swobu","metadata":{"owner":"after"},"models":[{"id":"default","name":"Swobu default"},{"id":"other","name":"After"}]}`}
	replies["openclaw config get agents.defaults.models --json"] = commandReply{stdout: `{"other/model":{},"human/model":{}}`}
	providerJSON := `{"api":"openai-completions","apiKey":"swobu","baseUrl":"http://127.0.0.1:7926/c/work","metadata":{"owner":"after"},"models":[{"id":"default","name":"Swobu default"},{"id":"other","name":"After"}]}`
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
	for _, want := range []string{`"owner":"after"`, `"id":"other"`, `"human/model"`} {
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
