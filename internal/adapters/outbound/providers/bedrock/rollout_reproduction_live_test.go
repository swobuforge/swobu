//go:build integration_live

package bedrock

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/session"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestLiveBedrockMantleGrokProductionCodecToolImageContinuation(t *testing.T) {
	if strings.TrimSpace(os.Getenv("SWOBU_LIVE_BEDROCK_DOGFOOD")) != "1" {
		t.Skip("set SWOBU_LIVE_BEDROCK_DOGFOOD=1")
	}
	region := firstNonEmpty(os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION"), "us-east-1")
	model := firstNonEmpty(os.Getenv("SWOBU_BEDROCK_MODEL"), "xai.grok-4.3")
	endpoint := firstNonEmpty(os.Getenv("SWOBU_BEDROCK_MANTLE_ENDPOINT"), "https://bedrock-mantle."+region+".api.aws/openai/v1")
	credentialRef := ""
	if strings.TrimSpace(os.Getenv("AWS_BEARER_TOKEN_BEDROCK")) != "" {
		credentialRef = "env:AWS_BEARER_TOKEN_BEDROCK"
	}
	exec := NewExecutor(http.DefaultClient)
	exec.credentials = credentials.NewEnvResolver()
	target := provider.NewBedrockTargetSnapshot("live-bedrock-production-continuation", endpoint, credentialRef, protocolkind.Responses, "responses", region, delivery.BufferedDelivery())
	target.Model = model
	backend, err := exec.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}

	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "inspect_image")
	tool := canonicaltest.MustFunctionTool(key, "Inspect an image supplied by the caller", canonicaltest.Schema(t, `{"type":"object","properties":{"action":{"type":"string","enum":["inspect"]}},"required":["action"],"additionalProperties":false}`), canonical.Specify(true))
	reasoningControls, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Compute: canonical.Specify(canonical.NewAutomaticReasoningCompute())})
	if err != nil {
		t.Fatal(err)
	}
	effort := canonical.InferenceEffortLow
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: &effort})
	if err != nil {
		t.Fatal(err)
	}
	turnOne := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify(model),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, tool),
			canonicaltest.Message(t, canonical.MessageRoleUser, "Call inspect_image exactly once. Do not answer in prose."),
		},
		ToolPolicy:    canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil)),
		ToolCallBatch: canonical.Specify(canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne)),
		Controls:      controls, Reasoning: reasoningControls,
	})
	names, _, err := provider.BuildAttemptToolNames(turnOne)
	if err != nil {
		t.Fatal(err)
	}
	d := delivery.StreamingDelivery(delivery.FramingSSE)
	firstRequest := provider.Request{ExchangeID: "live-grok-turn-one", Canonical: turnOne, ToolNames: names, Delivery: d}
	firstDocument, _, err := backend.Codec.Encode(firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	firstIngress, err := backend.Transport.Send(context.Background(), firstDocument)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := backend.Codec.Decode(context.Background(), firstRequest, firstIngress)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(decoded.Stream, canonical.ResponseBinding{
		SwobuID: "swobu_grok_turn_one", TargetID: target.TargetID, TargetVersion: target.TargetVersion,
	}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	turnOneResponse, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	var callIDs []canonical.ToolCallID
	responsesReplay := false
	for _, item := range turnOneResponse.Items() {
		if reasoning, ok := item.Reasoning(); ok {
			_, responsesReplay = reasoning.Opaque().Responses()
		}
		if call, ok := item.ToolCall(); ok && call.Tool().Kind() == canonical.ToolKindFunction {
			callIDs = append(callIDs, call.CallID())
		}
	}
	if len(callIDs) == 0 || !responsesReplay {
		t.Fatalf("turn one items lack function call or Responses replay: %#v", turnOneResponse.Items())
	}

	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	image, err := canonical.NewInlineImage(canonical.ImageMediaPNG, png, canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	results := make([]canonical.CanonicalItem, 0, len(callIDs))
	for _, callID := range callIDs {
		result, err := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewImageToolResultPart(image)}, false)
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, result)
	}
	turnTwo := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify(model), Items: results,
		PreviousResponse: &canonical.ResponseRef{SwobuID: "swobu_grok_turn_one"},
	})
	prepared, err := session.Resume(turnTwo, session.Checkpoint{HistoryScheme: "messages/v1", Request: turnOne, Response: *turnOneResponse})
	if err != nil {
		t.Fatal(err)
	}
	secondNames, _, err := provider.BuildAttemptToolNames(prepared.Request())
	if err != nil {
		t.Fatal(err)
	}
	secondRequest := provider.Request{ExchangeID: "live-grok-turn-two", Canonical: prepared.Request(), ToolNames: secondNames, Delivery: d}
	secondDocument, _, err := backend.Codec.Encode(secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(secondDocument.RawBytes(), []byte(`{"type":"reasoning"}`)) {
		t.Fatalf("production codec emitted an empty reasoning union: %s", secondDocument.RawBytes())
	}
	if !bytes.Contains(secondDocument.RawBytes(), []byte(`"summary":[]`)) {
		t.Fatalf("production codec omitted the required empty reasoning summary: %s", secondDocument.RawBytes())
	}
	secondIngress, err := backend.Transport.Send(context.Background(), secondDocument)
	if err != nil {
		t.Fatalf("production-code tool continuation rejected: %v", err)
	}
	if stream, ok := secondIngress.(provider.StreamIngress); ok {
		defer func() { _ = stream.Stream.Body.Close() }()
	}
}

func TestLiveBedrockMantleGrokReasoningStreamReplayShape(t *testing.T) {
	if strings.TrimSpace(os.Getenv("SWOBU_LIVE_BEDROCK_DOGFOOD")) != "1" {
		t.Skip("set SWOBU_LIVE_BEDROCK_DOGFOOD=1")
	}
	region := firstNonEmpty(os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION"), "us-east-1")
	model := firstNonEmpty(os.Getenv("SWOBU_BEDROCK_MODEL"), "xai.grok-4.3")
	endpoint := firstNonEmpty(os.Getenv("SWOBU_BEDROCK_MANTLE_ENDPOINT"), "https://bedrock-mantle."+region+".api.aws/openai/v1")
	credentialRef := ""
	if strings.TrimSpace(os.Getenv("AWS_BEARER_TOKEN_BEDROCK")) != "" {
		credentialRef = "env:AWS_BEARER_TOKEN_BEDROCK"
	}
	exec := NewExecutor(http.DefaultClient)
	exec.credentials = credentials.NewEnvResolver()
	target := provider.NewBedrockTargetSnapshot("live-bedrock-reasoning-stream", endpoint, credentialRef, protocolkind.Responses, "responses", region, delivery.BufferedDelivery())
	target.Model = model
	raw := []byte(`{"model":"` + model + `","stream":true,"store":false,"include":["reasoning.encrypted_content"],"reasoning":{"effort":"low","summary":"auto"},"input":"Think briefly, then say pong."}`)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	ingress, err := exec.Send(ctx, target, carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	stream, ok := ingress.(provider.StreamIngress)
	if !ok {
		t.Fatalf("ingress=%T want stream", ingress)
	}
	defer func() { _ = stream.Stream.Body.Close() }()
	scanner := bufio.NewScanner(stream.Stream.Body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	seenReasoning := false
	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.HasPrefix(line, []byte("data: ")) || bytes.Equal(line, []byte("data: [DONE]")) {
			continue
		}
		var frame struct {
			Type string `json:"type"`
			Item struct {
				Type             string `json:"type"`
				ID               string `json:"id"`
				EncryptedContent string `json:"encrypted_content"`
			} `json:"item"`
			Response struct {
				Output []struct {
					Type             string `json:"type"`
					ID               string `json:"id"`
					EncryptedContent string `json:"encrypted_content"`
				} `json:"output"`
			} `json:"response"`
		}
		if err := json.Unmarshal(bytes.TrimPrefix(line, []byte("data: ")), &frame); err != nil {
			t.Fatal(err)
		}
		if frame.Item.Type == "reasoning" {
			seenReasoning = true
			t.Logf("event=%s item_id_present=%t encrypted_content_bytes=%d", frame.Type, frame.Item.ID != "", len(frame.Item.EncryptedContent))
		}
		for _, item := range frame.Response.Output {
			if item.Type == "reasoning" {
				seenReasoning = true
				t.Logf("event=%s terminal_reasoning_id_present=%t encrypted_content_bytes=%d", frame.Type, item.ID != "", len(item.EncryptedContent))
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if !seenReasoning {
		t.Fatal("stream returned no reasoning item")
	}
}

func TestLiveBedrockMantleGrokEncryptedReasoningTraceReplay(t *testing.T) {
	if strings.TrimSpace(os.Getenv("SWOBU_LIVE_BEDROCK_DOGFOOD")) != "1" {
		t.Skip("set SWOBU_LIVE_BEDROCK_DOGFOOD=1")
	}
	region := firstNonEmpty(os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION"), "us-east-1")
	model := firstNonEmpty(os.Getenv("SWOBU_BEDROCK_MODEL"), "xai.grok-4.3")
	endpoint := firstNonEmpty(os.Getenv("SWOBU_BEDROCK_MANTLE_ENDPOINT"), "https://bedrock-mantle."+region+".api.aws/openai/v1")
	credentialRef := ""
	if strings.TrimSpace(os.Getenv("AWS_BEARER_TOKEN_BEDROCK")) != "" {
		credentialRef = "env:AWS_BEARER_TOKEN_BEDROCK"
	}
	exec := NewExecutor(http.DefaultClient)
	exec.credentials = credentials.NewEnvResolver()
	target := provider.NewBedrockTargetSnapshot("live-bedrock-reasoning-trace", endpoint, credentialRef, protocolkind.Responses, "responses", region, delivery.BufferedDelivery())
	target.Model = model
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	first := []byte(`{"model":"` + model + `","stream":false,"store":false,"include":["reasoning.encrypted_content"],"reasoning":{"effort":"low","summary":"auto"},"input":"Think briefly, then say pong."}`)
	ingress, err := exec.Send(ctx, target, carrier.NewDocument(protocolkind.Responses, "application/json", nil, first, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	document, ok := ingress.(provider.DocumentIngress)
	if !ok {
		t.Fatalf("ingress=%T want document", ingress)
	}
	var firstResponse struct {
		Output []map[string]any `json:"output"`
	}
	if err := json.Unmarshal(document.Document.RawBytes(), &firstResponse); err != nil {
		t.Fatal(err)
	}
	var reasoning map[string]any
	for _, item := range firstResponse.Output {
		if item["type"] == "reasoning" {
			reasoning = item
			break
		}
	}
	if reasoning == nil {
		t.Fatal("first response returned no reasoning item")
	}
	encrypted, _ := reasoning["encrypted_content"].(string)
	if encrypted == "" {
		t.Fatal("first response returned no encrypted reasoning")
	}
	reasoning["content"] = []any{map[string]any{"type": "reasoning_text", "text": "portable trace"}}
	second, err := json.Marshal(map[string]any{
		"model": model, "store": false,
		"input": []any{reasoning, map[string]any{"type": "message", "role": "user", "content": "continue"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.Send(ctx, target, carrier.NewDocument(protocolkind.Responses, "application/json", nil, second, carrier.Meta{})); err != nil {
		t.Fatalf("encrypted reasoning plus trace rejected: %v", err)
	}
}

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
	credentialRef := ""
	if strings.TrimSpace(os.Getenv("AWS_BEARER_TOKEN_BEDROCK")) != "" {
		credentialRef = "env:AWS_BEARER_TOKEN_BEDROCK"
	}
	target := provider.NewBedrockTargetSnapshot("live-bedrock-rollout", endpoint, credentialRef, protocolkind.Responses, "responses", region, delivery.BufferedDelivery())
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

func TestLiveBedrockMantleGrokToolImageAndBareReasoningReproduction(t *testing.T) {
	if strings.TrimSpace(os.Getenv("SWOBU_LIVE_BEDROCK_DOGFOOD")) != "1" {
		t.Skip("set SWOBU_LIVE_BEDROCK_DOGFOOD=1")
	}
	region := firstNonEmpty(os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION"), "us-east-1")
	model := firstNonEmpty(os.Getenv("SWOBU_BEDROCK_MODEL"), "xai.grok-4.3")
	endpoint := firstNonEmpty(os.Getenv("SWOBU_BEDROCK_MANTLE_ENDPOINT"), "https://bedrock-mantle."+region+".api.aws/openai/v1")
	exec := NewExecutor(http.DefaultClient)
	exec.credentials = credentials.NewEnvResolver()
	credentialRef := ""
	if strings.TrimSpace(os.Getenv("AWS_BEARER_TOKEN_BEDROCK")) != "" {
		credentialRef = "env:AWS_BEARER_TOKEN_BEDROCK"
	}
	target := provider.NewBedrockTargetSnapshot("live-bedrock-tool-image", endpoint, credentialRef, protocolkind.Responses, "responses", region, delivery.BufferedDelivery())
	target.Model = model

	const imageURL = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	textOutput := "result"
	imageOutput := []any{map[string]any{"type": "input_image", "image_url": imageURL}}
	tests := []struct {
		name      string
		output    any
		reasoning map[string]any
	}{
		{name: "text tool output", output: textOutput},
		{name: "image tool output", output: imageOutput},
		{name: "text tool output with bare reasoning", output: textOutput, reasoning: map[string]any{"type": "reasoning"}},
		{name: "text tool output with reasoning content", output: textOutput, reasoning: map[string]any{
			"type": "reasoning", "content": []any{map[string]any{"type": "reasoning_text", "text": "inspect the image"}},
		}},
		{name: "text tool output with identified completed reasoning content", output: textOutput, reasoning: map[string]any{
			"type": "reasoning", "id": "rs_probe", "status": "completed", "content": []any{map[string]any{"type": "reasoning_text", "text": "inspect the image"}},
		}},
		{name: "text tool output with reasoning summary", output: textOutput, reasoning: map[string]any{
			"type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": "inspect the image"}},
		}},
		{name: "image tool output with bare reasoning", output: imageOutput, reasoning: map[string]any{"type": "reasoning"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := []any{
				map[string]any{"type": "function_call", "call_id": "call_1", "name": "inspect_image", "arguments": `{}`},
				map[string]any{"type": "function_call_output", "call_id": "call_1", "output": test.output},
				map[string]any{"type": "message", "role": "user", "content": "continue"},
			}
			if test.reasoning != nil {
				input = append([]any{test.reasoning}, input...)
			}
			raw, err := json.Marshal(map[string]any{"model": model, "store": false, "input": input})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			ingress, err := exec.Send(ctx, target, carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}))
			if err != nil {
				t.Fatalf("case rejected: %v", err)
			}
			if stream, ok := ingress.(provider.StreamIngress); ok {
				defer func() { _ = stream.Stream.Body.Close() }()
			}
		})
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
			input = append(input, map[string]any{"type": "function_call_output", "call_