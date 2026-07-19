// swobu:lint ignore test-only-dead-cluster because=compat feature vocabulary is authoritative even when only tests exercise some names.
package compat

// Feature names a compatibility-addressable canonical or approved
// representation path. For canonical data, the value mirrors its typed schema
// path. Optional scoped suffixes are protocol, then provider; collection
// indices are omitted because Subject identifies one occurrence.
type Feature string

const (
	// Canonical request paths.
	RequestModel                     Feature = "request.model"
	RequestInstructions              Feature = "request.instructions"
	RequestItemsKind                 Feature = "request.items.kind"
	RequestItemsAuthor               Feature = "request.items.author"
	RequestItemsText                 Feature = "request.items.text"
	RequestItemsToolUseID            Feature = "request.items.tool_use.use_id"
	RequestItemsToolType             Feature = "request.items.tool_use.tool_type"
	RequestItemsToolName             Feature = "request.items.tool_use.name"
	RequestItemsToolInput            Feature = "request.items.tool_use.input"
	RequestItemsToolResultUseID      Feature = "request.items.tool_result.use_id"
	RequestItemsToolResultText       Feature = "request.items.tool_result.text"
	RequestTools                     Feature = "request.tools"
	RequestToolsKind                 Feature = "request.tools.kind"
	RequestToolsName                 Feature = "request.tools.name"
	RequestToolsNameNamespace        Feature = "request.tools.name_namespace"
	RequestToolsDescription          Feature = "request.tools.description"
	RequestToolsSchema               Feature = "request.tools.schema"
	RequestToolsSchemaStrict         Feature = "request.tools.schema.strict"
	RequestToolPolicy                Feature = "request.tool_policy"
	RequestToolCallBatch             Feature = "request.tool_call_batch"
	RequestOutputFormat              Feature = "request.output_format"
	RequestOutputFormatSchema        Feature = "request.output_format.schema"
	RequestControlsMaxOutputTokens   Feature = "request.controls.max_output_tokens"
	RequestControlsTemperature       Feature = "request.controls.temperature"
	RequestControlsTopP              Feature = "request.controls.top_p"
	RequestControlsStopSequences     Feature = "request.controls.stop_sequences"
	RequestPreviousResponse          Feature = "request.previous_response"
	RequestPreviousResponseResponses Feature = "request.previous_response.responses"

	// Canonical response paths.
	ResponseID                    Feature = "response.id"
	ResponseIDResponses           Feature = "response.id.responses"
	ResponseItemsKind             Feature = "response.items.kind"
	ResponseItemsAuthor           Feature = "response.items.author"
	ResponseItemsText             Feature = "response.items.text"
	ResponseItemsToolUseID        Feature = "response.items.tool_use.use_id"
	ResponseItemsToolType         Feature = "response.items.tool_use.tool_type"
	ResponseItemsToolName         Feature = "response.items.tool_use.name"
	ResponseItemsToolInput        Feature = "response.items.tool_use.input"
	ResponseItemsToolResultUseID  Feature = "response.items.tool_result.use_id"
	ResponseItemsToolResultText   Feature = "response.items.tool_result.text"
	ResponseFinishReason          Feature = "response.finish_reason"
	ResponseUsageInputTokens      Feature = "response.usage.input_tokens"
	ResponseUsageOutputTokens     Feature = "response.usage.output_tokens"
	ResponseUsageReasoningTokens  Feature = "response.usage.reasoning_tokens"
	ResponseUsageCacheReadTokens  Feature = "response.usage.cache_read_tokens"
	ResponseUsageCacheWriteTokens Feature = "response.usage.cache_write_tokens"

	// Approved noncanonical representation roots.
	DeliveryStreaming        Feature = "delivery.streaming"
	DeliveryServerSentEvents Feature = "delivery.server_sent_events"
	DeliveryWebSocket        Feature = "delivery.websocket"
	DeliveryIncremental      Feature = "delivery.incremental"
	DeliveryTerminalEvent    Feature = "delivery.terminal_event"
	WireJSONMode             Feature = "wire.json_mode"
	WireConversation         Feature = "wire.conversation"
	ErrorShape               Feature = "error.shape"
	ErrorClass               Feature = "error.class"
)
