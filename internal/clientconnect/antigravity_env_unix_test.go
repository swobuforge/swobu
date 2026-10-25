//go:build !windows

package clientconnect

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These fixtures exercise Unix shell profiles. Windows uses its registry
// adapter and must never run these fixtures against the user's environment.
func TestAntigravityRepairsProviderOnlyBrokenState(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".gemini", "antigravity-cli", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte("{\n  \"modelProvider\": \"gemini\"\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".zshrc"), []byte("export KEEP=yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		homeDir: func() (string, error) { return dir, nil },
		getenv: func(key string) string {
			if key == "SHELL" {
				return "/bin/zsh"
			}
			return ""
		},
	}
	plan, err := service.Plan(context.Background(), ClientAntigravity, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if plan.AlreadyConfigured() {
		t.Fatal("provider-only state was reported configured")
	}
	if plan.Target != testTarget(t) {
		t.Fatalf("target = %#v", plan.Target)
	}
	if _, err := service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".zshrc"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"export KEEP=yes", "export GEMINI_API_KEY='test-workspace-credential'", "export GOOGLE_GEMINI_BASE_URL='http://127.0.0.1:7926/workspaces/work'"} {
		if !strings.Contains(text, want) {
			t.Fatalf("profile missing %q:\n%s", want, text)
		}
	}
}

func antigravityTestService(home, shell string) *Service {
	return &Service{
		homeDir: func() (string, error) { return home, nil },
		getenv: func(key string) string {
			if key == "SHELL" {
				return shell
			}
			return ""
		},
	}
}

func TestAntigravityFreshInstallIsCompleteAndIdempotent(t *testing.T) {
	home := t.TempDir()
	service := antigravityTestService(home, "/bin/bash")
	plan, err := service.Plan(context.Background(), ClientAntigravity, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if plan.AlreadyConfigured() || len(plan.ConfigPaths) != 2 {
		t.Fatalf("fresh plan = %#v", plan)
	}
	verified, err := service.Apply(context.Background(), plan)
	if err != nil || !verified.AlreadyConfigured() {
		t.Fatalf("verified = %#v, error = %v", verified, err)
	}
	profile, _ := os.ReadFile(filepath.Join(home, ".bashrc"))
	if strings.Count(string(profile), antigravityProfileStart) != 1 {
		t.Fatalf("managed block count:\n%s", profile)
	}
	settings, _ := os.ReadFile(filepath.Join(home, ".gemini", "antigravity-cli", "settings.json"))
	if !strings.Contains(string(settings), `"modelProvider":"gemini"`) {
		t.Fatalf("settings = %s", settings)
	}
	second, err := service.Plan(context.Background(), ClientAntigravity, testTarget(t))
	if err != nil || !second.AlreadyConfigured() {
		t.Fatalf("second plan = %#v, error = %v", second, err)
	}
}

func TestAntigravityProfilePreservesRealKeyAndUnrelatedBytes(t *testing.T) {
	home := t.TempDir()
	original := "# user config\nexport GEMINI_API_KEY='real-user-key'\nexport KEEP=yes\n"
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	service := antigravityTestService(home, "/usr/bin/zsh")
	plan, err := service.Plan(context.Background(), ClientAntigravity, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	text := string(raw)
	if !strings.HasPrefix(text, original) || strings.Count(text, "real-user-key") != 1 {
		t.Fatalf("foreign profile state changed:\n%s", text)
	}
	info, _ := os.Stat(filepath.Join(home, ".zshrc"))
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("existing shell profile mode changed: %o", info.Mode().Perm())
	}
}

func TestAntigravityMovesManagedBlockLastWithoutReorderingForeignBytes(t *testing.T) {
	home := t.TempDir()
	before := "# before\n\n"
	after := "\nsome_command_that_reads_GOOGLE_GEMINI_BASE_URL\n\n\n"
	profile := before + strings.TrimSuffix(string(antigravityProfileBlock("http://127.0.0.1:7926/workspaces/old", testTarget(t).bearer)), "\n") + after
	path := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(path, []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}
	service := antigravityTestService(home, "/bin/zsh")
	plan, err := service.Plan(context.Background(), ClientAntigravity, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	text := string(raw)
	want := before + strings.TrimPrefix(after, "\n") + string(antigravityProfileBlock(testTarget(t).WorkspaceURL(), testTarget(t).bearer))
	if text != want {
		t.Fatalf("profile bytes changed outside managed block:\ngot  %q\nwant %q", text, want)
	}
}

func TestAntigravityMovingManagedBlockAcrossLaterSourceRequiresReplacement(t *testing.T) {
	home := t.TempDir()
	block := string(antigravityProfileBlock(testTarget(t).WorkspaceURL(), testTarget(t).bearer))
	foreign := "\nexport GOOGLE_GEMINI_BASE_URL='https://other.example'\n"
	path := filepath.Join(home, ".bashrc")
	if err := os.WriteFile(path, []byte(block+foreign), 0o600); err != nil {
		t.Fatal(err)
	}
	service := antigravityTestService(home, "/bin/bash")
	plan, err := service.Plan(context.Background(), ClientAntigravity, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if plan.AlreadyConfigured() {
		t.Fatalf("non-final managed block reported configured: %#v", plan)
	}
	if !plan.RequiresReplace() {
		t.Fatalf("managed block relocation did not require replacement: %#v", plan)
	}
	for _, change := range plan.Changes {
		if change.Field == "profile precedence" && change.BeforeExists && change.Before == "non-final" && change.After == "final" {
			goto reviewed
		}
	}
	t.Fatalf("profile precedence replacement evidence missing: %#v", plan)

reviewed:
	if _, err := service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	want := foreign + block
	if string(raw) != want {
		t.Fatalf("managed block lacks final precedence:\ngot  %q\nwant %q", raw, want)
	}
}

func TestAntigravityIgnoresMarkerTextInsideShellCommands(t *testing.T) {
	home := t.TempDir()
	original := "echo \"# >>> swobu antigravity >>>\"\necho \"# <<< swobu antigravity <<<\"\n"
	path := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	service := antigravityTestService(home, "/bin/zsh")
	plan, err := service.Plan(context.Background(), ClientAntigravity, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(raw), original) || strings.Count(string(raw), antigravityProfileStart) != 2 || strings.Count(string(raw), antigravityProfileEnd) != 2 {
		t.Fatalf("quoted marker text was treated as managed syntax:\n%s", raw)
	}
}

func TestAntigravityCanonicalizesFinalManagedBlockBeforeReportingConfigured(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".bashrc")
	blockWithoutFinalNewline := strings.TrimSuffix(string(antigravityProfileBlock(testTarget(t).WorkspaceURL(), testTarget(t).bearer)), "\n")
	if err := os.WriteFile(path, []byte(blockWithoutFinalNewline), 0o600); err != nil {
		t.Fatal(err)
	}
	service := antigravityTestService(home, "/bin/bash")
	plan, err := service.Plan(context.Background(), ClientAntigravity, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if plan.AlreadyConfigured() {
		t.Fatalf("noncanonical managed block reported configured: %#v", plan)
	}
	verified, err := service.Apply(context.Background(), plan)
	if err != nil || !verified.AlreadyConfigured() {
		t.Fatalf("verified = %#v, error = %v", verified, err)
	}
}

func TestAntigravityInheritedForeignEndpointRequiresReplacement(t *testing.T) {
	home := t.TempDir()
	service := &Service{
		homeDir: func() (string, error) { return home, nil },
		getenv: func(key string) string {
			switch key {
			case "SHELL":
				return "/bin/zsh"
			case "GOOGLE_GEMINI_BASE_URL":
				return "https://foreign.example"
			}
			return ""
		},
	}
	plan, err := service.Plan(context.Background(), ClientAntigravity, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if !plan.RequiresReplace() {
		t.Fatalf("foreign endpoint did not require replacement: %#v", plan)
	}
	for _, change := range plan.Changes {
		if change.Field == "endpoint" && change.BeforeExists && change.Before == "https://foreign.example" {
			return
		}
	}
	t.Fatalf("foreign endpoint evidence missing: %#v", plan)
}

func TestAntigravityWorkspaceSwitchRequiresReplacement(t *testing.T) {
	home := t.TempDir()
	service := antigravityTestService(home, "/bin/zsh")
	initial, err := service.Plan(context.Background(), ClientAntigravity, testTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	next, err := NewTarget("new", "http://127.0.0.1:7926/workspaces/new")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.Plan(context.Background(), ClientAntigravity, next.WithModelID("coding").WithAuthentication("test-principal", testTarget(t).bearer))
	if err != nil {
		t.Fatal(err)
	}
	if !plan.RequiresReplace() || len(plan.Changes) != 1 || plan.Changes[0].Field != "endpoint" {
		t.Fatalf("workspace replacement plan = %#v", plan)
	}
}

func TestAntigravityRejectsMalformedManagedProfileMarkers(t *testing.T) {
	for _, profile := range []string{
		antigravityProfileStart + "\n",
		antigravityProfileStart + "\n" + antigravityProfileEnd + "\n" + antigravityProfileStart + "\n" + antigravityProfileEnd + "\n",
	} {
		home := t.TempDir()
		path := filepath.Join(home, ".bashrc")
		if err := os.WriteFile(path, []byte(profile), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := antigravityTestService(home, "/bin/bash").Plan(context.Background(), ClientAntigravity, testTarget(t))
		if err == nil || !strings.Contains(err.Error(), "markers") {
			t.Fatalf("profile %q error = %v", profile, err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != profile {
			t.Fatal("malformed profile changed")
		}
	}
}

func TestAntigravityRejectsUnsupportedShell(t *testing.T) {
	_, err := antigravityTestService(t.TempDir(), "/usr/bin/fish").Plan(context.Background(), ClientAntigravity, testTarget(t))
	if err == nil || !strings.Contains(err.Error(), "supports bash and zsh") {
		t.Fatalf("error = %v", err)
	}
}
