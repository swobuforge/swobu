// swobu:lint ignore test-only-dead-cluster because=compat feature vocabulary is authoritative even when only tests exercise some names.
package compat

// Feature names one semantic capability that a route may need to preserve.
type Feature string

const (
	// Request-level semantics.
	RequestInputShape       Feature = "request.input_shape"
	RequestModel            Feature = "request.model"
	RequestRole             Feature = "request.role"
	RequestToolChoice       Feature = "request.tool_choice"
	RequestParallelTools    Feature = "request.parallel_tools"
	RequestStructuredOutput Feature = "request.structured_output"
	RequestContinuation     Feature = "request.continuation"
	RequestConversation     Feature = "request.conversation"

	// Message/content semantics.
	MessageRole     Feature = "message.role"
	MessageAuthor   Feature = "message.author"
	ContentPartKind Feature = "content.part_kind"
	ContentText     Feature = "content.text"
	ContentImage    Feature = "content.image"
	ContentAudio    Feature = "content.audio"
	ContentFile     Feature = "content.file"
	ContentRefusal  Feature = "content.refusal"

	// Tool declaration and execution.
	ToolDeclaration   Feature = "tool.declaration"
	ToolKind          Feature = "tool.kind"
	ToolName          Feature = "tool.name"
	ToolNameNamespace Feature = "tool.name_namespace"
	ToolDescription   Feature = "tool.description"
	ToolSchema        Feature = "tool.schema"
	ToolSchemaStrict  Feature = "tool.schema_strict"
	ToolCallID        Feature = "tool.call_id"
	ToolCallKind      Feature = "tool.call_kind"
	ToolCallArguments Feature = "tool.call_arguments"
	ToolResultID      Feature = "tool.result_id"
	ToolResultBody    Feature = "tool.result_body"

	// Generation controls.
	GenerationMaxTokens     Feature = "generation.max_tokens"
	GenerationTemperature   Feature = "generation.temperature"
	GenerationTopP          Feature = "generation.top_p"
	GenerationStopSequences Feature = "generation.stop_sequences"
	GenerationSeed          Feature = "generation.seed"
	GenerationMultiplicity  Feature = "generation.multiplicity"

	// Output/response semantics.
	OutputFormat      Feature = "output.format"
	OutputJSONSchema  Feature = "output.json_schema"
	OutputTextFormat  Feature = "output.text_format"
	OutputItemKind    Feature = "output.item_kind"
	ResponseReasoning Feature = "response.reasoning"
	ResponseFinish    Feature = "response.finish"
	ResponseError     Feature = "response.error"

	// Usage accounting.
	UsageInputTokens      Feature = "usage.input_tokens"
	UsageOutputTokens     Feature = "usage.output_tokens"
	UsageReasoningTokens  Feature = "usage.reasoning_tokens"
	UsageCacheReadTokens  Feature = "usage.cache_read_tokens"
	UsageCacheWriteTokens Feature = "usage.cache_write_tokens"

	// Delivery/framing.
	DeliveryStreaming        Feature = "delivery.streaming"
	DeliveryServerSentEvents Feature = "delivery.server_sent_events"
	DeliveryWebSocket        Feature = "delivery.websocket"
	DeliveryIncremental      Feature = "delivery.incremental"
	DeliveryTerminalEvent    Feature = "delivery.terminal_event"

	// Wire/state.
	WireJSONMode      Feature = "wire.json_mode"
	WireRawPayload    Feature = "wire.raw_payload"
	WireNativePayload Feature = "wire.native_payload"
	StateTurnSnapshot Feature = "state.turn_snapshot"

	// Errors.
	ErrorShape Feature = "error.shape"
	ErrorClass Feature = "error.class"
)
