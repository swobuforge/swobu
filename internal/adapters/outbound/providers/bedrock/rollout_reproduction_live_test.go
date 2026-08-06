//go:build integration_live

package bedrock

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestLiveBedrockMantleGrokCodexRolloutReproduction(t *testing.T) {
	if strings.TrimSpace(os.Getenv("SWOBU_LIVE_BEDROCK_DOGFOOD")) != "1" {
		t.Skip("set SWOBU_LIVE_BEDROCK_DOGFOOD=1")
	}
	rolloutPath := strings.TrimSpace(os.Getenv("SWOBU_BEDROCK_ROLLOUT"))
	if rolloutPath == "" {
		t.Skip("set SWOBU_BEDROCK_ROLLOUT to a Codex rollout JSONL file")
	}
	instructions, input := loadCodexRolloutForBedrockProbe(t, rolloutPath)
	region := firstNonEmpty(os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION"), "us-east-1")
	model := firstNonEmpty(os.Getenv("SWOBU_BEDROCK_MODEL"), "xai.grok-4.3")
	endpoint := firstNonEmpty(os.Getenv("SWOBU_BEDROCK_MANTLE_ENDPOINT"), "https://bedrock-mantle."+region+".api.aws/openai/v1")
	exec := NewExecutor(http.DefaultClient)
	exec.credentials = credentials.NewEnvResolver()
	target := provider.NewBedrockTargetSnapshot("live-bedrock-rollout", endpoint, "", protocolkind.Responses, profile.FrameHTTPJSONBody, "responses", region)
	target.Model = model
	raw, err := json.Marshal(map[string]any{"model": model, "store": false, "instructions": instructions, "input": input})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("request_bytes=%d input_items=%d instructions_bytes=%d", len(raw), len(input), len(instructions))
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	ingress, err := exec.Send(ctx, target, carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatalf("live rollout reproduction rejected: %v", err)
	}
	if stream, ok := ingress.(provider.StreamIngress); ok {
		defer func() { _ = stream.Stream.Body.Close() }()
	}
}

func loadCodexRolloutForBedrockProbe(t *testing.T, path string) (string, []any) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	var instructions strings.Builder
	input := make([]any, 0)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var record struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		if record.Type != "response_item" {
			continue
		}
		var item struct {
			Type      string          `json:"type"`
			Role      string          `json:"role,omitempty"`
			Content   json.RawMessage `json:"content,omitempty"`
			CallID    string          `json:"call_id,omitempty"`
			Name      string          `json:"name,omitempty"`
			Arguments string          `json:"arguments,omitempty"`
			Output    json.RawMessage `json:"output,omitempty"`
		}
		if err := json.Unmarshal(record.Payload, &item); err != nil {
			t.Fatal(err)
		}
		switch item.Type {
		case "message":
			var parts []map[string]any
			if err := json.Unmarshal(item.Content, &parts); err != nil {
				t.Fatal(err)
			}
			if item.Role == "developer" {
				for _, part := range parts {
					if text, ok := part["text"].(string); ok {
						instructions.WriteString(text)
					}
				}
				continue
			}
			message := map[string]any{"type": "message", "role": item.Role, "content": parts}
			if item.Role == "assistant" {
				message["status"] = "completed"
			}
			input = append(input, message)
		case "function_call":
			input = append(input, map[string]any{"type": "function_call", "call_id": item.CallID, "name": item.Name, "arguments": item.Arguments})
		case "function_call_output":
			var output any
			if err := json.Unmarshal(item.Output, &output); err != nil {
				t.Fatal(err)
			}
			input = append(input, map[string]any{"type": "function_call_output", "call_id": item.CallID, "output": output})
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return instructions.String(), input
}
