package clientconnect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAntigravityPreservesForeignSettingsAndOwnsOnlyProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := "{\n  \"theme\": \"dark\",\n  \"modelProvider\": \"other\"\n}\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	mutation, err := planAntigravity(path, testTarget(t), antigravityEnvironmentPlan{})
	if err != nil {
		t.Fatal(err)
	}
	if !mutation.plan.RequiresReplace() {
		t.Fatal("replacement was not reported")
	}
	if err := mutation.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"theme": "dark"`) || !strings.Contains(text, `"modelProvider": "gemini"`) {
		t.Fatalf("settings = %s", text)
	}
	for _, forbidden := range []string{"GEMINI_API_KEY", "GOOGLE_GEMINI_BASE_URL"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("settings persisted %s", forbidden)
		}
	}
}

func TestAntigravityDetectionUsesBinaryOrSettingsFile(t *testing.T) {
	dir := t.TempDir()
	service := &Service{homeDir: func() (string, error) { return dir, nil }, lookPath: func(string) (string, error) { return "", os.ErrNotExist }}
	present, err := antigravityPresent(service)
	if err != nil || present {
		t.Fatalf("present = %v, %v", present, err)
	}
	path, _ := service.antigravityPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	present, err = antigravityPresent(service)
	if err != nil || !present {
		t.Fatalf("present = %v, %v", present, err)
	}
}

func TestAntigravityEnvironmentCommitsBeforeProviderMode(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	environmentApplied := false
	mutation, err := planAntigravity(settingsPath, testTarget(t), antigravityEnvironmentPlan{
		configPaths: []string{"profile"},
		changes:     []Change{{Field: "endpoint", After: testTarget(t).WorkspaceURL()}},
		apply: func(context.Context) error {
			environmentApplied = true
			return context.Canceled
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mutation.apply(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("apply error = %v", err)
	}
	settings, _ := os.ReadFile(settingsPath)
	if !environmentApplied || strings.Contains(string(settings), "modelProvider") {
		t.Fatalf("unsafe committed prefix: applied=%t settings=%s", environmentApplied, settings)
	}
}
