package canonical

import (
	"regexp"
	"strings"
	"testing"
)

var capabilitySegment = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func TestCapabilityPathsUseCanonicalGrammar(t *testing.T) {
	paths := []CapabilityPath{
		RequestModel, RequestInstructions, RequestItemsKind,
		RequestItemsMessageRole, RequestItemsMessageText, RequestItemsMessageCitations, RequestItemsMessageImage,
		RequestItemsMessageImageSourceURL, RequestItemsMessageImageSourceInline,
		RequestItemsMessageImageMediaType, RequestItemsMessageImageDetail,
		RequestItemsToolResultImage, RequestItemsToolResultImageSourceURL,
		RequestItemsToolResultImageSourceInline, RequestItemsToolResultImageMediaType,
		RequestItemsToolResultImageDetail, RequestItemsToolCallCallID,
		RequestItemsToolCallTool, RequestItemsToolCallName, RequestItemsToolCallInput,
		RequestItemsToolResultCallID, RequestItemsToolResultContent,
		RequestItemsToolResultContentBoundaries, RequestItemsToolResultIsError,
		RequestItemsResponsesReasoningReplay, RequestTools, RequestToolsKind, RequestToolsDiscovery,
		RequestToolsName, RequestToolsDescription, RequestToolsSchema, RequestToolsVisibility,
		RequestToolsSchemaStrict, RequestToolPolicy, RequestToolCallBatch,
		RequestOutputFormat, RequestOutputFormatSchema, RequestControlsMaxOutputTokens,
		RequestControlsTemperature, RequestControlsTopP, RequestControlsStopSequences,
		RequestControlsEffort, RequestReasoning, RequestReasoningContext,
		RequestReasoningContextResponses, RequestPreviousResponse,
		RequestPreviousResponseResponses, ResponseID, ResponseIDResponses,
		ResponseItemsKind, ResponseItemsMessageRole, ResponseItemsMessageText,
		ResponseItemsMessageCitations, ResponseItemsToolCallCallID,
		ResponseItemsToolCallTool, ResponseItemsToolCallName, ResponseItemsToolCallInput,
		ResponseItemsReasoning, ResponseFinishReason, ResponseUsageInputTokens,
		ResponseUsageOutputTokens, ResponseUsageReasoningTokens,
		ResponseUsageCacheReadTokens, ResponseUsageCacheWriteTokens,
	}
	seen := make(map[CapabilityPath]struct{}, len(paths))
	for _, path := range paths {
		if _, duplicate := seen[path]; duplicate {
			t.Fatalf("duplicate capability path %q", path)
		}
		seen[path] = struct{}{}
		parts := strings.Split(path.String(), ".")
		if len(parts) < 2 || (parts[0] != "request" && parts[0] != "response") {
			t.Fatalf("capability path %q has invalid root", path)
		}
		for _, part := range parts {
			if !capabilitySegment.MatchString(part) {
				t.Fatalf("capability path %q has invalid segment %q", path, part)
			}
		}
		for _, forbidden := range []string{"delivery.", "wire.", "error.", "[", "]"} {
			if strings.Contains(path.String(), forbidden) {
				t.Fatalf("capability path %q contains forbidden vocabulary %q", path, forbidden)
			}
		}
	}
}
