package providercatalog

// ProviderID is the canonical provider identity used across runtime seams.
type ProviderID string

// ParseProviderID parses one provider identifier from external input.
// Parsing is strict: callers must pass canonical values already validated at ingress.
func ParseProviderID(raw string) (ProviderID, bool) {
	switch ProviderID(raw) {
	case ProviderSpecOllama,
		ProviderSpecOpenAI,
		ProviderSpecChatGPT,
		ProviderSpecAnthropic,
		ProviderSpecOpenRouter,
		ProviderSpecOpenAICompatible:
		return ProviderID(raw), true
	default:
		return "", false
	}
}
