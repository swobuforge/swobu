package operatorclient

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
)

func TestEmptyWorkspaceCreationEncodesOnlyIdentityAndDisplayMetadata(t *testing.T) {
	calls := 0
	client := New(&http.Client{Transport: principalCreationTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		var body map[string]json.RawMessage
		if req.Method != http.MethodPost || req.URL.Path != "/api/v1/workspaces" || req.Header.Get("Idempotency-Key") != "original-key" || req.GetBody != nil || json.NewDecoder(req.Body).Decode(&body) != nil || len(body) != 2 || string(body["slug"]) != `"research"` || string(body["display_name"]) != `"Research 東京"` {
			t.Error("empty creation acquired a fabricated target or lost metadata")
		}
		header := make(http.Header)
		header.Set("Location", "/api/v1/workspaces/research")
		header.Set("ETag", `"original-1"`)
		return &http.Response{StatusCode: 201, Header: header, Body: io.NopCloser(strings.NewReader(`{"slug":"research","display_name":"Research 東京","management_etag":"original-1","management_revision":1,"routes":[],"readiness":{"llm":"disabled","mcp":"disabled"}}`))}, nil
	})}, "http://127.0.0.1:1234")
	intent, err := NewWorkspaceCreationIntent(workspaceapi.CreateWorkspace{Slug: "research", DisplayName: "Research 東京"}, "original-key")
	if err != nil {
		t.Fatal(err)
	}
	result, replayed, err := client.ApplyWorkspaceCreation(t.Context(), intent)
	if err != nil || replayed || calls != 1 || result.DisplayName != "Research 東京" || len(result.Routes) != 0 {
		t.Fatal("valid empty creation failed", err)
	}
}

func TestWorkspaceDisplayNameClientOneSendAndExactAcknowledgement(t *testing.T) {
	for _, mode := range []string{"success", "clear", "lost", "stale", "missing-name", "null-name", "wrong-name", "wrong-slug", "wrong-etag", "missing-revision", "trailing"} {
		t.Run(mode, func(t *testing.T) {
			name := "Research 東京"
			if mode == "clear" {
				name = ""
			}
			calls := 0
			client := New(&http.Client{Transport: principalCreationTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				var input map[string]string
				if req.Method != http.MethodPatch || req.URL.Path != "/api/v1/workspaces/research" || req.URL.RawQuery != "" || req.GetBody != nil || req.Header.Get("If-Match") != `"original"` || json.NewDecoder(req.Body).Decode(&input) != nil || len(input) != 1 || input["display_name"] != name {
					t.Error("reviewed metadata intent changed")
				}
				if mode == "lost" {
					return nil, errors.New("fixture-secret")
				}
				status := 200
				body := `{"slug":"research","display_name":"` + name + `","management_etag":"current","management_revision":2,"routes":[],"readiness":{"llm":"not_configured","mcp":"not_configured"}}`
				switch mode {
				case "stale":
					status = 412
					body = `{"code":"CONFLICT","message":"stale"}`
				case "missing-name":
					body = strings.Replace(body, `"display_name":"`+name+`",`, "", 1)
				case "null-name":
					body = strings.Replace(body, `"display_name":"`+name+`"`, `"display_name":null`, 1)
				case "wrong-name":
					body = strings.Replace(body, name, "Other", 1)
				case "wrong-slug":
					body = strings.Replace(body, `"slug":"research"`, `"slug":"renamed"`, 1)
				case "missing-revision":
					body = strings.Replace(body, `"management_revision":2,`, "", 1)
				case "trailing":
					body += `{}`
				}
				header := make(http.Header)
				header.Set("ETag", `"current"`)
				if mode == "wrong-etag" {
					header.Set("ETag", `"other"`)
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}, "http://127.0.0.1:1234")
			result, err := client.UpdateWorkspaceDisplayName(t.Context(), "research", name, "original")
			if calls != 1 {
				t.Fatal("automatic retry/refresh/fallback")
			}
			if mode == "success" || mode == "clear" {
				if err != nil || result.DisplayName != name {
					t.Fatal("valid acknowledgement rejected", err)
				}
			} else if err == nil || result.Slug != "" || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("invalid response claimed partial success or leaked transport error")
			}
		})
	}
}

func TestWorkspaceDisplayNameClientInvalidIntentDoesNotContactDaemon(t *testing.T) {
	calls := 0
	client := New(&http.Client{Transport: principalCreationTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected") })}, "http://127.0.0.1:1234")
	for _, input := range []struct{ slug, name, revision string }{
		{" research", "Research", "reviewed"}, {"../research", "Research", "reviewed"}, {"research", " padded ", "reviewed"}, {"research", "bad\nname", "reviewed"}, {"research", strings.Repeat("界", 129), "reviewed"}, {"research", "Research", ""}, {"research", "Research", `unsafe"revision`},
	} {
		if _, err := client.UpdateWorkspaceDisplayName(t.Context(), input.slug, input.name, input.revision); err == nil {
			t.Fatal("unsafe intent accepted")
		}
	}
	if calls != 0 {
		t.Fatal("invalid intent contacted daemon")
	}
}
