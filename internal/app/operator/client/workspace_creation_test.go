package operatorclient

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
)

func TestWorkspaceCreationRetainsOneSendIntentAndChecksOriginalResult(t *testing.T) {
	for _, mode := range []string{"created", "replayed", "lost", "conflict", "missing-name", "null-name", "changed-name", "changed-slug", "changed-revision", "changed-etag", "changed-location", "trailing"} {
		t.Run(mode, func(t *testing.T) {
			cmd := workspaceapi.CreateWorkspace{Slug: "research", DisplayName: "Research"}
			intent, err := NewWorkspaceCreationIntent(cmd, "original-key")
			if err != nil {
				t.Fatal(err)
			}
			cmd.Slug, cmd.DisplayName = "changed", "Changed"
			calls := 0
			client := New(&http.Client{Transport: principalCreationTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				body, _ := io.ReadAll(req.Body)
				if req.Method != "POST" || req.URL.Path != "/api/v1/workspaces" || req.URL.RawQuery != "" || req.GetBody != nil || req.Header.Get("Idempotency-Key") != "original-key" || string(body) != `{"slug":"research","display_name":"Research"}` {
					t.Error("retained request changed")
				}
				if mode == "lost" {
					return nil, errors.New("fixture-secret")
				}
				status := 201
				if mode == "replayed" {
					status = 200
				}
				bodyText := `{"slug":"research","display_name":"Research","management_revision":1,"management_etag":"original-1","routes":[],"readiness":{"llm":"not_configured","mcp":"not_configured"}}`
				header := make(http.Header)
				header.Set("ETag", `"original-1"`)
				header.Set("Location", "/api/v1/workspaces/research")
				switch mode {
				case "null-routes":
					bodyText = strings.Replace(bodyText, `"routes":[]`, `"routes":null`, 1)
				case "conflict":
					status = 409
					bodyText = `{"code":"CONFLICT","message":"changed intent"}`
				case "missing-name":
					bodyText = strings.Replace(bodyText, `"display_name":"Research",`, "", 1)
				case "null-name":
					bodyText = strings.Replace(bodyText, `"display_name":"Research"`, `"display_name":null`, 1)
				case "changed-name":
					bodyText = strings.Replace(bodyText, `"Research"`, `"Changed"`, 1)
				case "changed-slug":
					bodyText = strings.Replace(bodyText, `"research"`, `"changed"`, 1)
				case "changed-revision":
					bodyText = strings.Replace(bodyText, `"management_revision":1`, `"management_revision":2`, 1)
				case "changed-etag":
					header.Set("ETag", `"changed"`)
				case "changed-location":
					header.Set("Location", "/api/v1/workspaces/changed")
				case "trailing":
					bodyText += `{}`
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(bodyText))}, nil
			})}, "http://127.0.0.1:1234")
			got, replay, err := client.ApplyWorkspaceCreation(t.Context(), intent)
			if calls != 1 {
				t.Fatal("retry, readback or fallback")
			}
			if mode == "created" || mode == "replayed" {
				if err != nil || got.Slug != "research" || replay != (mode == "replayed") {
					t.Fatal("valid original result rejected", err)
				}
			} else if err == nil || got.Slug != "" || replay || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("invalid result claimed success or disclosed transport detail")
			}
		})
	}
}

func TestWorkspaceCreationIntentRejectsInvalidInputBeforeContact(t *testing.T) {
	for _, input := range []struct{ slug, name, key string }{{" research", "Research", "key"}, {"research", " padded ", "key"}, {"research", "bad\nname", "key"}, {"research", "Research", ""}, {"research", "Research", "bad key"}} {
		if _, err := NewWorkspaceCreationIntent(workspaceapi.CreateWorkspace{Slug: input.slug, DisplayName: input.name}, input.key); err == nil {
			t.Fatal("invalid retained intent accepted")
		}
	}
	client := New(&http.Client{Transport: principalCreationTransport(func(*http.Request) (*http.Response, error) { t.Fatal("zero intent contacted daemon"); return nil, nil })}, "http://127.0.0.1:1234")
	if _, _, err := client.ApplyWorkspaceCreation(t.Context(), WorkspaceCreationIntent{}); err == nil {
		t.Fatal("zero intent accepted")
	}
}

func TestWorkspaceCreationPreviewRetainsBodyAndRejectsContradictoryImpact(t *testing.T) {
	for _, mode := range []string{"valid", "missing-count", "changed-name", "authority", "secret", "changed-readiness", "trailing", "lost"} {
		t.Run(mode, func(t *testing.T) {
			intent, err := NewWorkspaceCreationIntent(workspaceapi.CreateWorkspace{Slug: "research", DisplayName: "Research"}, "key")
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			client := New(&http.Client{Transport: principalCreationTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				raw, _ := io.ReadAll(req.Body)
				if req.Method != "POST" || req.URL.Path != "/api/v1/workspaces" || req.URL.RawQuery != "dry_run=true" || req.Header.Get("Idempotency-Key") != "" || req.GetBody != nil || string(raw) != string(intent.body) {
					t.Error("preview changed intent or consumed retry key")
				}
				if mode == "lost" {
					return nil, errors.New("fixture-secret")
				}
				body := `{"slug":"research","display_name":"Research","route_count":0,"target_count":0,"readiness":{"llm":"not_configured","mcp":"not_configured"},"approval_class":"confirmation","diff":"Create","warning":"No reservation"}`
				switch mode {
				case "missing-count":
					body = strings.Replace(body, `"route_count":0,`, "", 1)
				case "changed-name":
					body = strings.Replace(body, `"Research"`, `"Changed"`, 1)
				case "authority":
					body = strings.Replace(body, `"slug":`, `"management_revision":1,"slug":`, 1)
				case "secret":
					body = strings.Replace(body, `"slug":`, `"secret":"fixture-secret","slug":`, 1)
				case "changed-readiness":
					body = strings.Replace(body, `"llm":"not_configured"`, `"llm":"ready"`, 1)
				case "trailing":
					body += `{}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}, "http://127.0.0.1:1234")
			preview, err := client.PreviewWorkspaceCreation(t.Context(), intent)
			if calls != 1 {
				t.Fatal("preview retried")
			}
			if mode == "valid" {
				if err != nil || preview.Slug != "research" {
					t.Fatal("valid preview rejected", err)
				}
			} else if err == nil || preview.Slug != "" || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("invalid preview accepted or exposed data")
			}
		})
	}
}
