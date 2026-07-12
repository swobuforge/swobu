package effect

// ReasonCode names the stable reason vocabulary shared by compatibility
// effects and provider usage notices.
type ReasonCode string

const (
	ReasonTargetRejectsUnknownField ReasonCode = "target_rejects_unknown_field"
	ReasonTargetLacksToolForm       ReasonCode = "target_lacks_tool_form"
	ReasonTargetLacksContentPart    ReasonCode = "target_lacks_content_part"
	ReasonDuplicateUsageReport      ReasonCode = "duplicate_usage_report"
	ReasonMissingTerminalEvent      ReasonCode = "missing_terminal_event"
	ReasonOpaqueStateRequired       ReasonCode = "opaque_state_required"
	ReasonInvalidEventLifecycle     ReasonCode = "invalid_event_lifecycle"
	ReasonTransportDeliveryFailed   ReasonCode = "transport_delivery_failed"
)
