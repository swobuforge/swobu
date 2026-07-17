package readmodel

import (
	"strings"
	"testing"
)

func TestDiagnosticsPayload_OmitsHeaders(t *testing.T) {
	payload := diagnosticsPayloadFixture()

	assertDiagnosticsDoesNotContain(t, payload, "Authorization", "Bearer sk-live-secret", "X-Api-Key")
}

func TestDiagnosticsPayload_OmitsBodies(t *testing.T) {
	payload := diagnosticsPayloadFixture()

	assertDiagnosticsDoesNotContain(t, payload, `"messages"`, "prompt body", "completion body")
}

func TestDiagnosticsPayload_OmitsCredentials(t *testing.T) {
	payload := diagnosticsPayloadFixture()

	assertDiagnosticsDoesNotContain(t, payload, "OPENAI_API_KEY", "env:", "credential", "keychain:")
}

func TestDiagnosticsPayload_IncludesSafeNames(t *testing.T) {
	payload := diagnosticsPayloadFixture()
	text := payload.Text()

	for _, want := range []string{"dev", "gpt-4.1", "fast", "deep"} {
		if !strings.Contains(text, want) {
			t.Fatalf("diagnostics payload missing safe name %q:\n%s", want, text)
		}
	}
	if got, want := payload.Summary(), "1 workspaces · 1 routes · 2 targets · 3 activity"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func diagnosticsPayloadFixture() DiagnosticsPayload {
	return DiagnosticsPayload{
		Version:       "swobu dev",
		DaemonVersion: "dev",
		ConfigPath:    "/home/test/.config/swobu/config.yaml",
		ActivityCount: 3,
		Workspaces: []DiagnosticsWorkspacePayload{{
			Name:          "dev",
			ActivityCount: 3,
			Routes: []DiagnosticsRoutePayload{{
				Name:    "gpt-4.1",
				Default: true,
				Targets: []DiagnosticsTargetPayload{
					{Name: "fast"},
					{Name: "deep"},
				},
			}},
		}},
	}
}

func assertDiagnosticsDoesNotContain(t *testing.T, payload DiagnosticsPayload, values ...string) {
	t.Helper()
	text := payload.Text()
	for _, value := range values {
		if value == "" {
			continue
		}
		if strings.Contains(text, value) {
			t.Fatalf("diagnostics payload contains unsafe value %q:\n%s", value, text)
		}
	}
}
