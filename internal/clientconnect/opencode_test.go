package clientconnect

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openCodeService(t *testing.T, home string, env map[string]string, binary bool) *Service {
	t.Helper()
	return &Service{
		homeDir: func() (string, error) { return home, nil },
		getenv:  func(key string) string { return env[key] },
		lookPath: func(name string) (string, error) {
			if binary && name == "opencode" {
				return "/bin/opencode", nil
			}
			return "", os.ErrNotExist
		},
	}
}

func openCodeConfigDir(home string, env map[string]string) string {
	if env["XDG_CONFIG_HOME"] != "" {
		return filepath.Join(env["XDG_CONFIG_HOME"], "opencode")
	}
	return filepath.Join(home, ".config", "opencode")
}

func writeOpenCodeConfig(t *testing.T, dir, name, raw string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(raw), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOpenCodeFreshConfigurationIsMinimalAndConverges(t *testing.T) {
	home := t.TempDir()
	service := openCodeService(t, home, nil, true)
	plan, err := service.Plan(context.Background(), ClientOpenCode, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(home, ".config", "opencode", "opencode.jsonc")
	if len(plan.ConfigPaths) != 1 || plan.ConfigPaths[0] != wantPath {
		t.Fatalf("config paths = %#v, want %q", plan.ConfigPaths, wantPath)
	}
	verified, err := service.Apply(context.Background(), plan)
	if err != nil || !verified.AlreadyConfigured() {
		t.Fatalf("verified = %#v, error = %v", verified, err)
	}
	raw, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{`"model":"swobu/default"`, `"npm":"@ai-sdk/openai"`, `"baseURL":"` + testTarget(t).WorkspaceURL() + `"`, `"apiKey":"swobu"`, `"default":{`} {
		if !strings.Contains(text, want) {
			t.Fatalf("fresh configuration missing %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{`"tool_call"`, `"name"`, `"limit"`, `"context"`, `"output"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("fresh configuration contains unowned %q:\n%s", forbidden, text)
		}
	}
}

func TestOpenCodeDiscoveryUsesBinaryRegularFilesAndXDG(t *testing.T) {
	home, xdg := t.TempDir(), t.TempDir()
	env := map[string]string{"XDG_CONFIG_HOME": xdg}
	service := openCodeService(t, home, env, false)
	if parsed, err := ParseClientID("opencode"); err != nil || parsed != ClientOpenCode {
		t.Fatalf("ParseClientID(opencode) = %q, %v", parsed, err)
	}
	if present, err := openCodeAdapter.present(service); err != nil || present {
		t.Fatalf("absent = %v, %v", present, err)
	}
	dir := openCodeConfigDir(home, env)
	if err := os.MkdirAll(filepath.Join(dir, "config.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeOpenCodeConfig(t, dir, "opencode.json", "{}\n")
	if present, err := openCodeAdapter.present(service); err != nil || !present {
		t.Fatalf("file present = %v, %v", present, err)
	}
	paths, err := service.openCodePaths()
	if err != nil || !strings.HasPrefix(paths[0], filepath.Join(xdg, "opencode")) {
		t.Fatalf("paths = %#v, %v", paths, err)
	}
	if present, err := openCodeAdapter.present(openCodeService(t, home, nil, true)); err != nil || !present {
		t.Fatalf("binary present = %v, %v", present, err)
	}
}

func TestOpenCodeReadsOwnedLeavesByPrecedenceAndWritesHighestExistingFile(t *testing.T) {
	home := t.TempDir()
	dir := openCodeConfigDir(home, nil)
	low := writeOpenCodeConfig(t, dir, "config.json", `{"model":"other/model","provider":{"swobu":{"options":{"baseURL":"https://old","apiKey":"operator"},"models":{"default":{"limit":{"context":200000,"output":64000}}}}},"disabled_providers":["swobu"]}`)
	middle := writeOpenCodeConfig(t, dir, "opencode.json", `{"model":"swobu/default","provider":{"swobu":{"npm":"@ai-sdk/openai","options":{"baseURL":"https://middle"}}},"experimental":{"policies":"opaque"}}`)
	high := writeOpenCodeConfig(t, dir, "opencode.jsonc", "{\n  // keep comment\n  \"provider\": {\"swobu\": {\"options\": {\"baseURL\": \"https://high\"}}},\n  \"unrelated\": true,\n}\n")
	service := openCodeService(t, home, map[string]string{"OPENCODE_CONFIG": "ignored", "OPENCODE_CONFIG_DIR": "ignored", "OPENCODE_CONFIG_CONTENT": "ignored"}, false)
	plan, err := service.Plan(context.Background(), ClientOpenCode, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.ConfigPaths) != 1 || plan.ConfigPaths[0] != high || len(plan.Changes) != 1 || plan.Changes[0].Field != "endpoint" || plan.Changes[0].Before != "https://high" {
		t.Fatalf("plan = %#v", plan)
	}
	if _, err := service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	lowAfter, _ := os.ReadFile(low)
	middleAfter, _ := os.ReadFile(middle)
	highAfter, _ := os.ReadFile(high)
	if !strings.Contains(string(lowAfter), `"apiKey":"operator"`) || !strings.Contains(string(lowAfter), `"limit":{"context":200000,"output":64000}`) || !strings.Contains(string(middleAfter), `"policies":"opaque"`) {
		t.Fatal("operator-owned lower-precedence configuration changed")
	}
	for _, want := range []string{"keep comment", `"unrelated": true`, testTarget(t).WorkspaceURL()} {
		if !strings.Contains(string(highAfter), want) {
			t.Fatalf("highest-precedence source missing %q:\n%s", want, highAfter)
		}
	}
	for _, unowned := range []string{`"apiKey"`, `"models"`, `"limit"`} {
		if strings.Contains(string(highAfter), unowned) {
			t.Fatalf("highest-precedence file redundantly shadows lower %s:\n%s", unowned, highAfter)
		}
	}
	v2Home := t.TempDir()
	writeOpenCodeConfig(t, openCodeConfigDir(v2Home, nil), "config.json", `{"providers":{}}`)
	if _, err := openCodeService(t, v2Home, nil, false).Plan(context.Background(), ClientOpenCode, testTarget(t)); err == nil || !strings.Contains(err.Error(), "native V2") {
		t.Fatalf("V2 error = %v", err)
	}
}

func TestOpenCodeApplyRejectsBindingOrWriteLocusChange(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, dir string)
	}{
		{name: "owned value changed", mutate: func(t *testing.T, dir string) { writeOpenCodeConfig(t, dir, "config.json", `{"model":"newer"}`) }},
		{name: "higher priority file appeared", mutate: func(t *testing.T, dir string) { writeOpenCodeConfig(t, dir, "opencode.jsonc", `{}`) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			dir := openCodeConfigDir(home, nil)
			writeOpenCodeConfig(t, dir, "config.json", `{"model":"old"}`)
			service := openCodeService(t, home, nil, false)
			plan, err := service.Plan(context.Background(), ClientOpenCode, testTarget(t))
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(t, dir)
			if _, err := service.Apply(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "Open Connect again") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
