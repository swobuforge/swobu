package bootstrap

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	executionevidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

// This is assembled daemon evidence using a synthetic upstream. It proves the
// public streaming cancellation boundary, not qualification of a live provider
// or its client SDK.
func TestDaemonProviderStreamCancellationSettlesOneAttempt(t *testing.T) {
	t.Setenv("SWOBU_LEDGER", "sqlite")
	for _, testCase := range []struct {
		name     string
		path     string
		protocol string
		request  string
		headers  map[string]string
		prefix   string
		stream   string
	}{
		{
			name: "openai-responses", path: "/responses", protocol: "responses_stream",
			request: `{"model":"chat","input":"cancel me","stream":true}`,
			prefix:  "response.output_text.delta", stream: "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"cancel-1\",\"model\":\"fixture\",\"status\":\"in_progress\",\"output\":[]}}\n\nevent: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg-1\",\"output_index\":0,\"content_index\":0,\"delta\":\"partial\"}\n\n",
		},
		{
			name: "openai-chat-completions", path: "/chat/completions", protocol: "chat_completions_stream",
			request: `{"model":"chat","messages":[{"role":"user","content":"cancel me"}],"stream":true}`,
			prefix:  `"content":"partial"`, stream: "data: {\"id\":\"cancel-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n",
		},
		{
			name: "anthropic-messages", path: "/messages", protocol: "messages_stream",
			request: `{"model":"chat","max_tokens":100,"messages":[{"role":"user","content":"cancel me"}],"stream":true}`,
			headers: map[string]string{"Anthropic-Version": "2023-06-01"},
			prefix:  "content_block_delta", stream: "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"cancel-1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"fixture\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			var providerCalls atomic.Int32
			upstreamCanceled := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if providerCalls.Add(1) != 1 {
					t.Errorf("provider received duplicate attempt")
				}
				if r.URL.Path != "/v1"+testCase.path {
					t.Errorf("provider path = %q", r.URL.Path)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, testCase.stream)
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				<-r.Context().Done()
				close(upstreamCanceled)
			}))
			defer upstream.Close()

			configPath := filepath.Join(root, "swobu.yaml")
			writeCurrentWorkspaceFixture(t, configPath, root, "cancellation", upstream.URL, testCase.protocol, false)
			daemon, err := Start(t.Context(), StartInput{ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
			if err != nil {
				t.Fatal(err)
			}
			defer daemon.Close()
			workspace, err := routing.ParseWorkspaceSlug("cancellation")
			if err != nil {
				t.Fatal(err)
			}
			_, issued, err := daemon.accessStore.Create("stream cancellation", workspace)
			if err != nil {
				t.Fatal(err)
			}

			requestContext, cancelRequest := context.WithCancel(t.Context())
			defer cancelRequest()
			request, err := http.NewRequestWithContext(requestContext, http.MethodPost, daemon.BaseURL()+"/workspaces/cancellation"+testCase.path, strings.NewReader(testCase.request))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer "+issued.Token)
			request.Header.Set("Content-Type", "application/json")
			for name, value := range testCase.headers {
				request.Header.Set(name, value)
			}
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(response.Body)
			for {
				line, readErr := reader.ReadString('\n')
				if strings.Contains(line, testCase.prefix) {
					cancelRequest()
					break
				}
				if readErr != nil {
					t.Fatalf("stream ended before partial event: %v", readErr)
				}
			}
			_ = response.Body.Close()

			select {
			case <-upstreamCanceled:
			case <-time.After(3 * time.Second):
				t.Fatal("upstream did not observe client cancellation")
			}
			deadline := time.Now().Add(3 * time.Second)
			for {
				projection := daemon.executionEvidenceStore.ProjectActivity(executionevidence.ProjectionInput{Scope: executionevidence.ProjectionScope{Kind: executionevidence.ProjectionScopeWorkspace, Workspace: "cancellation"}})
				if len(projection.Exchanges) == 1 {
					row := projection.Exchanges[0]
					if row.State == "cancelled" {
						if row.AttemptCount != 1 {
							t.Fatalf("attempt count = %d, want one", row.AttemptCount)
						}
						client := operatorclient.New(http.DefaultClient, daemon.BaseURL())
						before, err := client.Activity(t.Context(), "cancellation")
						if err != nil {
							t.Fatal(err)
						}
						if err := daemon.Close(); err != nil {
							t.Fatal(err)
						}
						if health := daemon.ledgerWriter.Health(); health.CommittedExchangesTotal != 1 || health.DroppedExchangesTotal != 0 {
							t.Fatalf("stream capture=%#v", health)
						}
						restarted, err := Start(t.Context(), StartInput{ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
						if err != nil {
							t.Fatal(err)
						}
						defer restarted.Close()
						after, err := operatorclient.New(http.DefaultClient, restarted.BaseURL()).Activity(t.Context(), "cancellation")
						if err != nil || len(after.Exchanges) != 1 || !reflect.DeepEqual(before.Exchanges, after.Exchanges) {
							t.Fatalf("restart changed model cancellation: before=%#v after=%#v err=%v", before, after, err)
						}
						if len(restarted.executionEvidenceStore.Settled()) != 0 {
							t.Fatal("restart model proof used memory history")
						}
						other, err := operatorclient.New(http.DefaultClient, restarted.BaseURL()).Activity(t.Context(), "other")
						if err != nil || len(other.Exchanges) != 0 {
							t.Fatalf("durable model history crossed Workspace: %#v, %v", other, err)
						}
						return
					}
				}
				if time.Now().After(deadline) {
					t.Fatalf("terminal cancellation projection = %#v", projection)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}
