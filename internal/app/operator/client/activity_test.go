package operatorclient

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestActivityClientRejectsInvalidScopeAndTrailingAcknowledgement(t *testing.T) {
	const valid = `{"scope":{"kind":"workspace","workspace":"research"},"exchanges":[],"mcp_operations":[]}`
	for _, body := range []string{
		strings.Replace(valid, `"kind":"workspace"`, `"kind":"all"`, 1),
		strings.Replace(valid, `"workspace":"research"`, `"workspace":"other"`, 1),
		strings.Replace(valid, `"exchanges":[]`, `"exchanges":null`, 1),
		strings.Replace(valid, `"exchanges":[]`, `"exchanges":[{"workspace":"other"}]`, 1),
		strings.Replace(valid, `"mcp_operations":[]`, `"mcp_operations":[{"workspace":"other"}]`, 1),
		strings.TrimSuffix(valid, "}") + `,"history":{"state":"disabled"}}`,
		valid + ` {}`,
	} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				fmt.Fprint(writer, body)
			}))
			defer server.Close()
			projection, err := New(server.Client(), server.URL).Activity(t.Context(), "research")
			if err == nil || calls != 1 || projection.Scope.Kind != "" {
				t.Fatal("invalid Activity acknowledged or retried")
			}
		})
	}
}

func TestActivityClientRejectsInexactWorkspaceBeforeContact(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		fmt.Fprint(writer, `{"scope":{"kind":"all"},"exchanges":[]}`)
	}))
	defer server.Close()
	client := New(server.Client(), server.URL)
	for _, workspace := range []string{"Research", " research", "research ", "../research"} {
		if _, err := client.Activity(t.Context(), workspace); err == nil || calls != 0 {
			t.Fatal("inexact Activity scope reached Node")
		}
	}
}

func TestActivityClientPreservesExactScopeAndCaptureUncertainty(t *testing.T) {
	for _, workspace := range []string{"", "research"} {
		t.Run(workspace, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				if request.Method != http.MethodGet || request.URL.Path != "/api/v1/activity" || request.URL.Query().Get("workspace") != workspace {
					t.Error("Activity escaped canonical exact read")
				}
				if workspace == "" {
					fmt.Fprint(writer, `{"scope":{"kind":"all"},"exchanges":[],"history":{"state":"disabled","dropped_mcp_operations_total":2}}`)
				} else {
					fmt.Fprint(writer, `{"scope":{"kind":"workspace","workspace":"research"},"exchanges":[],"mcp_operations":[{"operation_id":"mcp-1","workspace":"research","execution_possibility":"may_have_occurred"}],"private":"not-projected"}`)
				}
			}))
			defer server.Close()
			projection, err := New(server.Client(), server.URL).Activity(t.Context(), workspace)
			if err != nil || calls != 1 || projection.Exchanges == nil {
				t.Fatal("valid exact Activity rejected", err)
			}
			if workspace == "" && (projection.History == nil || projection.History.DroppedMCPOperationsTotal != 2) {
				t.Fatal("capture gap lost")
			}
			if workspace != "" && (projection.History != nil || len(projection.MCPOperations) != 1 || string(projection.MCPOperations[0].Occurrence) != "may_have_occurred") {
				t.Fatal("scoped uncertainty lost")
			}
		})
	}
}

func TestActivityKindUsesCanonicalFilterAndRejectsContradictoryReadback(t *testing.T) {
	for _, kind := range []string{"exchange", "mcp_operation"} {
		for _, contradictory := range []bool{false, true} {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				if request.URL.RequestURI() != "/api/v1/activity?workspace=research&kind="+kind {
					t.Error("kind escaped exact canonical query")
				}
				body := `{"scope":{"kind":"workspace","workspace":"research"},"exchanges":[]}`
				if contradictory {
					body = `{"scope":{"kind":"workspace","workspace":"research"},"exchanges":[{"workspace":"research"}],"mcp_operations":[{"workspace":"research"}]}`
				}
				fmt.Fprint(writer, body)
			}))
			client := New(server.Client(), server.URL)
			_, err := client.ActivityByKind(t.Context(), "research", kind)
			if (err != nil) != contradictory || calls != 1 {
				t.Fatal("kind acknowledgement accepted contradiction or retried")
			}
			if _, err := client.ActivityByKind(t.Context(), "research", "unknown"); err == nil || calls != 1 {
				t.Fatal("unsupported kind contacted Node")
			}
			server.Close()
		}
	}
}
