package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/test/e2e/harness"
)

const expectedOllamaReply = "ollama-proof-token-42"

// Traceability: real Codex client request path against Ollama-profile routing.
func TestCodexExecAgainstOllamaProfileEndpoint(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("codex e2e currently targets unix-style CI/dev lanes")
	}

	codexPath, err := exec.LookPath("codex")
	if err != nil {
		t.Fatalf("codex binary is required for this e2e lane: %v", err)
	}

	var chatCalls atomic.Int32
	var responseCalls atomic.Int32
	var lastPath atomic.Value
	ollamaUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath.Store(r.URL.Path)
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"llama3.2","object":"model"}]}`))
		case "/v1/chat/completions":
			chatCalls.Add(1)
			handleOpenAICompatibleChat(w, r)
		case "/v1/responses":
			responseCalls.Add(1)
			handleOpenAICompatibleResponses(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ollamaUpstream.Close()

	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(
				t,
				"jobs",
				"ollama-local",
				mustProviderConfigWithModelID(
					t,
					harness.NewProviderConfig(
						t,
						"ollama-local",
						"ollama",
						ollamaUpstream.URL+"/v1",
						"",
						protocolsurface.Responses,
					),
					"llama3.2",
				),
			),
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	base := daemon.BaseURL + "/c/jobs/"
	codexHome := t.TempDir()
	codexState := filepath.Join(codexHome, ".codex")
	if err := os.MkdirAll(codexState, 0o755); err != nil {
		t.Fatalf("create codex state dir: %v", err)
	}
	cmd := exec.CommandContext(
		ctx,
		codexPath,
		"exec",
		"--color", "never",
		"-c", `model_provider="localtest"`,
		"-c", `model_providers.localtest.name="Local Test"`,
		"-c", `model_providers.localtest.base_url="`+base+`v1/"`,
		"-c", `forced_login_method="api"`,
		"Reply with exactly: "+expectedOllamaReply,
	)
	cmd.Env = append(
		os.Environ(),
		"OPENAI_API_KEY=ollama-local",
		"HOME="+codexHome,
		"XDG_CONFIG_HOME="+codexHome,
		"XDG_STATE_HOME="+codexHome,
		"XDG_CACHE_HOME="+codexHome,
		"CODEX_HOME="+codexState,
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		t.Fatalf("codex exec failed: %v\noutput:\n%s", err, out.String())
	}

	if chatCalls.Load()+responseCalls.Load() == 0 {
		t.Fatalf("expected at least one request to ollama upstream; last_path=%q output=%q", valueOrEmpty(lastPath.Load()), out.String())
	}
	if !strings.Contains(out.String(), expectedOllamaReply) {
		t.Fatalf("expected codex output to include upstream reply token %q; output=%q", expectedOllamaReply, out.String())
	}
}

func handleOpenAICompatibleChat(w http.ResponseWriter, r *http.Request) {
	stream := requestWantsStream(r.Body)
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"llama3.2\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"llama3.2\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"" + expectedOllamaReply + "\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"llama3.2\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","created":1,"model":"llama3.2","choices":[{"index":0,"message":{"role":"assistant","content":"` + expectedOllamaReply + `"},"finish_reason":"stop"}]}`))
}

func handleOpenAICompatibleResponses(w http.ResponseWriter, r *http.Request) {
	stream := requestWantsStream(r.Body)
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"model\":\"llama3.2\",\"status\":\"in_progress\",\"output\":[]}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"status\":\"in_progress\",\"role\":\"assistant\",\"content\":[]}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.content_part.added\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"part\":{\"type\":\"output_text\",\"text\":\"\",\"annotations\":[]}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"" + expectedOllamaReply + "\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.done\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"text\":\"" + expectedOllamaReply + "\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.content_part.done\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"part\":{\"type\":\"output_text\",\"text\":\"" + expectedOllamaReply + "\",\"annotations\":[]}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"" + expectedOllamaReply + "\",\"annotations\":[]}]}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"model\":\"llama3.2\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"" + expectedOllamaReply + "\",\"annotations\":[]}]}]}}\n\n"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"id":"resp_1","model":"llama3.2","output_text":"` + expectedOllamaReply + `","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"` + expectedOllamaReply + `"}]}]}`))
}

func requestWantsStream(body io.ReadCloser) bool {
	raw, err := io.ReadAll(body)
	if err != nil {
		return false
	}
	_ = body.Close()
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}
	value, ok := payload["stream"]
	if !ok {
		return false
	}
	wantsStream, ok := value.(bool)
	return ok && wantsStream
}

func valueOrEmpty(v any) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return strings.TrimSpace(s)
}
