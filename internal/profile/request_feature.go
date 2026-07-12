package profile

// RequestFeature names one request-side capability that a provider protocol
// can carry truthfully. The catalog owns the support matrix so provider
// dispatch can fail closed before protocol encoding.
type RequestFeature string

const (
	RequestFeatureFunctionTools        RequestFeature = "function_tools"
	RequestFeatureToolChoiceNone       RequestFeature = "tool_choice_none"
	RequestFeatureToolChoiceRequired   RequestFeature = "tool_choice_required"
	RequestFeatureToolChoiceSpecific   RequestFeature = "tool_choice_specific"
	RequestFeatureToolBatchAtMostOne   RequestFeature = "tool_batch_at_most_one"
	RequestFeatureMaxOutputTokens      RequestFeature = "max_output_tokens"
	RequestFeatureStopSequences        RequestFeature = "stop_sequences"
	RequestFeatureTemperature          RequestFeature = "temperature"
	RequestFeatureTopP                 RequestFeature = "top_p"
	RequestFeatureJSONSchemaOutput     RequestFeature = "json_schema_output"
	RequestFeatureUsageReasoningTokens RequestFeature = "usage_reasoning_tokens"
)
