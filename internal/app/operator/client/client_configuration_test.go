package operatorclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientConfigurationPreviewExactCanonicalReadback(t *testing.T) {
	for _, workspace := range []string{"research", "other"} {
		t.Run(workspace, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				var body map[string]string
				if request.Method != http.MethodPost || request.URL.Path != "/api/v1/client-configurations" || request.URL.RawQuery != "dry_run=true" || json.NewDecoder(request.Body).Decode(&body) != nil || body["workspace"] != "research" || body["client"] != "codex" || body["model_id"] != "coding" {
					t.Error("preview escaped canonical explicit dry run")
				}
				fmt.Fprintf(writer, `{"workspace":%q,"model_id":"coding","client":{"id":"codex","name":"Codex"},"authentication":{"principal":"acc_AAAAAAAAAAAAAAAAAAAAAA","permission_revision":"permission-fixture","issue_credential":false,"credential_id":"acred_AAAAAAAAAAAAAAAAAAAAAA"},"config_paths":[],"changes":[],"already_configured":true,"requires_replace":false}`, workspace)
			}))
			defer server.Close()
			client := New(server.Client(), server.URL)
			plan, err := client.PreviewClientConfiguration(t.Context(), "research", "codex", "acc_AAAAAAAAAAAAAAAAAAAAAA", "coding")
			if calls != 1 || (workspace == "research" && (err != nil || !plan.AlreadyConfigured)) || (workspace != "research" && err == nil) {
				t.Fatal("preview accepted changed identity or retried")
			}
			for _, invalid := range []struct{ workspace, client string }{{"Research", "codex"}, {"research", "unsupported"}} {
				if _, err := client.PreviewClientConfiguration(t.Context(), invalid.workspace, invalid.client, "acc_AAAAAAAAAAAAAAAAAAAAAA", "coding"); err == nil || calls != 1 {
					t.Fatal("invalid preview reached Node")
				}
			}
		})
	}
}

func TestClientConfigurationPreviewRejectsMissingOrContradictoryDecisions(t *testing.T) {
	const valid = `{"workspace":"research","model_id":"coding","client":{"id":"codex","name":"Codex"},"authentication":{"principal":"acc_AAAAAAAAAAAAAAAAAAAAAA","permission_revision":"permission-fixture","issue_credential":false,"credential_id":"acred_AAAAAAAAAAAAAAAAAAAAAA"},"config_paths":["fixture/config.toml"],"changes":[{"field":"endpoint","before":"old","before_exists":true,"after":"new"}],"already_configured":false,"requires_replace":true}`
	for _, body := range []string{
		strings.Replace(valid, `,"already_configured":false`, "", 1),
		strings.Replace(valid, `false,"requires_replace"`, `null,"requires_replace"`, 1),
		strings.Replace(valid, `,"requires_replace":true`, "", 1),
		strings.Replace(valid, `"requires_replace":true`, `"requires_replace":null`, 1),
		strings.Replace(valid, `"already_configured":false`, `"already_configured":true`, 1),
		strings.Replace(valid, `"requires_replace":true`, `"requires_replace":false`, 1),
	} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				fmt.Fprint(writer, body)
			}))
			defer server.Close()
			plan, err := New(server.Client(), server.URL).PreviewClientConfiguration(t.Context(), "research", "codex", "acc_AAAAAAAAAAAAAAAAAAAAAA", "coding")
			if err == nil || plan.Client.ID != "" || calls != 1 {
				t.Fatal("malformed preview decisions acknowledged or retried")
			}
		})
	}
}

func TestClientConfigurationPreviewRejectsIncompleteChanges(t *testing.T) {
	const valid = `{"workspace":"research","model_id":"coding","client":{"id":"codex","name":"Codex"},"authentication":{"principal":"acc_AAAAAAAAAAAAAAAAAAAAAA","permission_revision":"permission-fixture","issue_credential":false,"credential_id":"acred_AAAAAAAAAAAAAAAAAAAAAA"},"config_paths":["fixture/config.toml"],"changes":[{"field":"endpoint","before":"","before_exists":false,"after":"new"}],"already_configured":false,"requires_replace":false}`
	for _, member := range []string{`"field":"endpoint"`, `"before":""`, `"before_exists":false`, `"after":"new"`} {
		for _, replacement := range []string{"missing", "null"} {
			t.Run(member+replacement, func(t *testing.T) {
				body := strings.Replace(valid, member, strings.SplitN(member, ":", 2)[0]+`:null`, 1)
				if replacement == "missing" {
					body = strings.Replace(valid, member+",", "", 1)
					if body == valid {
						body = strings.Replace(valid, ","+member, "", 1)
					}
				}
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					calls++
					fmt.Fprint(writer, body)
				}))
				defer server.Close()
				plan, err := New(server.Client(), server.URL).PreviewClientConfiguration(t.Context(), "research", "codex", "acc_AAAAAAAAAAAAAAAAAAAAAA", "coding")
				if err == nil || plan.Client.ID != "" || calls != 1 {
					t.Fatal("incomplete preview change acknowledged or retried")
				}
			})
		}
	}
}

func TestClientConfigurationPreviewPreservesExplicitEmptyChangeValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fmt.Fprint(writer, `{"workspace":"research","model_id":"coding","client":{"id":"codex","name":"Codex"},"authentication":{"principal":"acc_AAAAAAAAAAAAAAAAAAAAAA","permission_revision":"permission-fixture","issue_credential":false,"credential_id":"acred_AAAAAAAAAAAAAAAAAAAAAA"},"config_paths":[],"changes":[{"field":"endpoint","before":"","before_exists":false,"after":""}],"already_configured":false,"requires_replace":false}`)
	}))
	defer server.Close()
	plan, err := New(server.Client(), server.URL).PreviewClientConfiguration(t.Context(), "research", "codex", "acc_AAAAAAAAAAAAAAAAAAAAAA", "coding")
	if err != nil || len(plan.Changes) != 1 || plan.Changes[0].Before != "" || plan.Changes[0].After != "" || plan.Changes[0].BeforeExists || plan.AlreadyConfigured || plan.RequiresReplace {
		t.Fatal("explicit empty/false change evidence was lost")
	}
}
