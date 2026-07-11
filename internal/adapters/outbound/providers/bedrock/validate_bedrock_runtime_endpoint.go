package bedrock

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/profile"
)

func validateBedrockRuntimeEndpoint(baseURL string) error {
	class, _ := bedrockEndpointClassAndRegion(baseURL)
	if class == "bedrock_runtime_openai_compat" || class == "bedrock_mantle_openai_compat" {
		return nil
	}
	host := trimBedrockInput(mustParseURL(baseURL).Hostname())
	if host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "example.test" {
		return nil
	}
	return canonical.BadEndpoint("bedrock provider requires a Bedrock runtime endpoint host (bedrock-runtime.<region>... or bedrock-mantle.<region>...)")
}

type bedrockDispatchPlan struct {
	invokeModel  bool
	deliveryMode delivery.Mode
}

func resolveBedrockOperation(providerProtocol string) (bedrockDispatchPlan, error) {
	variant := strings.TrimSpace(providerProtocol) // swobu:io-string source=boundary
	if variant == "" || variant == profile.ProviderProtocolAuto {
		return bedrockDispatchPlan{}, canonical.BadEndpoint("bedrock provider protocol must be concrete (converse, converse_stream, invoke_model, invoke_model_stream)")
	}
	if !profile.SupportsProviderProtocolForSpec(string(profile.ProviderSpecBedrock), variant) {
		return bedrockDispatchPlan{}, canonical.BadEndpoint("selected provider protocol is unsupported for bedrock")
	}
	switch variant {
	case "converse":
		return bedrockDispatchPlan{invokeModel: false, deliveryMode: delivery.Buffered}, nil
	case "converse_stream":
		return bedrockDispatchPlan{invokeModel: false, deliveryMode: delivery.Streaming}, nil
	case "invoke_model":
		return bedrockDispatchPlan{invokeModel: true, deliveryMode: delivery.Buffered}, nil
	case "invoke_model_stream":
		return bedrockDispatchPlan{invokeModel: true, deliveryMode: delivery.Streaming}, nil
	default:
		return bedrockDispatchPlan{}, canonical.BadEndpoint("selected provider protocol is unsupported for bedrock")
	}
}

func bedrockMessagesFromRequest(req canonical.CanonicalRequest) ([]bedrocktypes.Message, error) {
	items := req.Items()
	if len(items) == 0 {
		return nil, canonical.BadRequest("request does not contain replayable conversation input")
	}
	out := make([]bedrocktypes.Message, 0, len(items))
	for _, item := range items {
		if item.Kind != canonical.ItemKindText {
			continue
		}
		role := bedrocktypes.ConversationRoleUser
		if item.Author == canonical.ItemAuthorAssistant {
			role = bedrocktypes.ConversationRoleAssistant
		}
		out = append(out, bedrocktypes.Message{Role: role, Content: []bedrocktypes.ContentBlock{&bedrocktypes.ContentBlockMemberText{Value: item.Text}}})
	}
	return out, nil
}

func decodeConverseOutput(out *bedrockruntime.ConverseOutput) (text string, stopReason string, usage canonical.TokenUsage) {
	if out == nil {
		return "", "", canonical.NewUnknownTokenUsage()
	}
	stopReason = string(out.StopReason)
	if msg, ok := out.Output.(*bedrocktypes.ConverseOutputMemberMessage); ok {
		text = bedrockContentText(msg.Value.Content)
	}
	usage = canonical.NewUnknownTokenUsage()
	if out.Usage != nil {
		usage = tokenUsageFromBedrock(out.Usage)
	}
	return text, stopReason, usage
}

func bedrockContentText(content []bedrocktypes.ContentBlock) string {
	var builder strings.Builder
	for _, block := range content {
		switch typed := block.(type) {
		case *bedrocktypes.ContentBlockMemberText:
			if trimBedrockInput(typed.Value) == "" {
				continue
			}
			if builder.Len() > 0 {
				builder.WriteString("\n")
			}
			builder.WriteString(typed.Value)
		}
	}
	return builder.String()
}

func collectConverseStream(out *bedrockruntime.ConverseStreamOutput) (text string, usage canonical.TokenUsage, stopReason string) {
	usage = canonical.NewUnknownTokenUsage()
	if out == nil || out.GetStream() == nil {
		return "", usage, ""
	}
	stream := out.GetStream()
	defer func() { _ = stream.Close() }()
	var builder strings.Builder
	for event := range stream.Events() {
		switch typed := event.(type) {
		case *bedrocktypes.ConverseStreamOutputMemberContentBlockDelta:
			switch delta := typed.Value.Delta.(type) {
			case *bedrocktypes.ContentBlockDeltaMemberText:
				builder.WriteString(delta.Value)
			}
		case *bedrocktypes.ConverseStreamOutputMemberMetadata:
			if typed.Value.Usage != nil {
				usage = tokenUsageFromBedrock(typed.Value.Usage)
			}
		case *bedrocktypes.ConverseStreamOutputMemberMessageStop:
			stopReason = string(typed.Value.StopReason)
		}
	}
	return builder.String(), usage, stopReason
}

func collectInvokeModelResponseStream(out *bedrockruntime.InvokeModelWithResponseStreamOutput) []byte {
	if out == nil || out.GetStream() == nil {
		return nil
	}
	stream := out.GetStream()
	defer func() { _ = stream.Close() }()
	var all []byte
	for event := range stream.Events() {
		switch typed := event.(type) {
		case *bedrocktypes.ResponseStreamMemberChunk:
			all = append(all, typed.Value.Bytes...)
		}
	}
	return all
}

func tokenUsageFromBedrock(in *bedrocktypes.TokenUsage) canonical.TokenUsage {
	if in == nil {
		return canonical.NewUnknownTokenUsage()
	}
	var inputPtr, outputPtr *int
	if in.InputTokens != nil {
		v := int(*in.InputTokens)
		inputPtr = &v
	}
	if in.OutputTokens != nil {
		v := int(*in.OutputTokens)
		outputPtr = &v
	}
	usage, err := canonical.NewTokenUsageWithOptional(inputPtr, outputPtr, nil, nil)
	if err != nil {
		return canonical.NewUnknownTokenUsage()
	}
	return usage
}

func mustConversationOutput(modelID string, text string, stop string, usage canonical.TokenUsage) canonical.CanonicalOutputValue {
	return canonical.NewConversationOutputWithUsage("", modelID, []canonical.OutputItem{canonical.NewTextOutputItem("text_0", text)}, stop, usage)
}

func decodeInvokeModelBuffered(raw []byte) (canonical.CanonicalOutputValue, error) {
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return canonical.CanonicalOutputValue{}, canonical.InternalError("bedrock invoke_model response is invalid JSON")
	}
	text := bedrockExtractInvokeText(parsed)
	return canonical.NewPromptOutput("", "", []canonical.OutputItem{canonical.NewTextOutputItem("text_0", text)}, ""), nil
}

func bedrockExtractInvokeText(root map[string]any) string {
	if value := bedrockTextAt(root, "results", "0", "outputText"); value != "" {
		return value
	}
	if value := bedrockTextAt(root, "completion"); value != "" {
		return value
	}
	if value := bedrockTextAt(root, "outputText"); value != "" {
		return value
	}
	if value := bedrockTextAt(root, "generation"); value != "" {
		return value
	}
	return ""
}

func bedrockTextAt(root map[string]any, path ...string) string {
	var current any = root
	for _, segment := range path {
		switch typed := current.(type) {
		case map[string]any:
			current = typed[segment]
		case []any:
			if segment != "0" || len(typed) == 0 {
				return ""
			}
			current = typed[0]
		default:
			return ""
		}
	}
	value, _ := current.(string)
	return trimBedrockInput(value)
}

func bedrockModelFromRequest(req canonical.CanonicalRequest) string {
	return req.Model()
}

func classifyBedrockSDKError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(err.Error()) // swobu:io-string source=boundary
	lower := strings.ToLower(msg)         // swobu:io-string source=boundary
	if strings.Contains(lower, "accessdenied") || strings.Contains(lower, "forbidden") || strings.Contains(lower, "statuscode: 403") {
		return canonical.NewBackendError("bedrock", http.StatusForbidden, msg, "")
	}
	if strings.Contains(lower, "unauthorized") || strings.Contains(lower, "expiredtoken") || strings.Contains(lower, "statuscode: 401") {
		return canonical.NewBackendError("bedrock", http.StatusUnauthorized, msg, "")
	}
	if strings.Contains(lower, "throttl") || strings.Contains(lower, "statuscode: 429") {
		return canonical.NewBackendError("bedrock", http.StatusTooManyRequests, msg, "")
	}
	if strings.Contains(lower, "validation") || strings.Contains(lower, "statuscode: 400") {
		return canonical.NewBackendError("bedrock", http.StatusBadRequest, msg, "")
	}
	if strings.Contains(lower, "statuscode: 404") {
		return canonical.NewBackendError("bedrock", http.StatusNotFound, msg, "")
	}
	if strings.Contains(lower, "statuscode: 500") {
		return canonical.NewBackendError("bedrock", http.StatusBadGateway, msg, "")
	}
	if strings.Contains(lower, "statuscode: 503") {
		return canonical.NewBackendError("bedrock", http.StatusServiceUnavailable, msg, "")
	}
	if strings.Contains(lower, "timeout") {
		return canonical.NewBackendError("bedrock", http.StatusGatewayTimeout, msg, "")
	}
	return canonical.NewBackendError("bedrock", http.StatusBadGateway, msg, "")
}
