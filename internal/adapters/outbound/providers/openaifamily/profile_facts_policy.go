package openaifamily

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/report"
)

// ProviderUsageDecoder decodes provider-specific usage semantics from a raw
// protocol usage envelope into canonical token usage.
type ProviderUsageDecoder interface {
	DecodeToCanonical(raw RawUsageEnvelope, current canonical.TokenUsage) (canonical.TokenUsage, []report.Notice)
}

// ProviderRoutePolicy owns provider route semantics while openaifamily owns shared
// transport/protocol execution mechanics.
type ProviderRoutePolicy interface {
	ProviderID() profile.ProviderID
	Facts(req canonical.CanonicalRequest) ProfileFactRecord
	UsageDecoder() ProviderUsageDecoder
	AuthStrategy() authStrategy
}

// ProfileFactRecord captures route facts for one provider policy. It is not a
// toggle bundle.
type ProfileFactRecord struct {
	CacheAffinityKey       string
	CacheAffinityRetention string
}

type openAIProviderRoutePolicy struct{}
type ollamaProviderRoutePolicy struct{}
type openAICompatibleProviderRoutePolicy struct{}
type openRouterProviderRoutePolicy struct{}
type passthroughProviderUsageDecoder struct{}

func (passthroughProviderUsageDecoder) DecodeToCanonical(_ RawUsageEnvelope, current canonical.TokenUsage) (canonical.TokenUsage, []report.Notice) {
	return current, nil
}

func (openAIProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecOpenAI
}

func (openAIProviderRoutePolicy) Facts(req canonical.CanonicalRequest) ProfileFactRecord {
	return routeProfileFactRecord(req)
}
func (openAIProviderRoutePolicy) UsageDecoder() ProviderUsageDecoder {
	return passthroughProviderUsageDecoder{}
}
func (openAIProviderRoutePolicy) AuthStrategy() authStrategy { return bearerAuthStrategy() }

func (ollamaProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecOllama
}

func (ollamaProviderRoutePolicy) Facts(req canonical.CanonicalRequest) ProfileFactRecord {
	return routeProfileFactRecord(req)
}
func (ollamaProviderRoutePolicy) UsageDecoder() ProviderUsageDecoder {
	return passthroughProviderUsageDecoder{}
}
func (ollamaProviderRoutePolicy) AuthStrategy() authStrategy { return noAuthStrategy() }

func (openAICompatibleProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecOpenAICompatible
}

func (openAICompatibleProviderRoutePolicy) Facts(req canonical.CanonicalRequest) ProfileFactRecord {
	return routeProfileFactRecord(req)
}
func (openAICompatibleProviderRoutePolicy) UsageDecoder() ProviderUsageDecoder {
	return passthroughProviderUsageDecoder{}
}
func (openAICompatibleProviderRoutePolicy) AuthStrategy() authStrategy { return xAPIKeyAuthStrategy() }

func (openRouterProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecOpenRouter
}

func (openRouterProviderRoutePolicy) Facts(req canonical.CanonicalRequest) ProfileFactRecord {
	return routeProfileFactRecord(req)
}
func (openRouterProviderRoutePolicy) UsageDecoder() ProviderUsageDecoder {
	return passthroughProviderUsageDecoder{}
}
func (openRouterProviderRoutePolicy) AuthStrategy() authStrategy { return bearerAuthStrategy() }
func routeProfileFactRecord(req canonical.CanonicalRequest) ProfileFactRecord {
	return ProfileFactRecord{
		CacheAffinityKey:       strings.TrimSpace(req.CacheIntent().Key()),               // swobu:io-string source=boundary
		CacheAffinityRetention: strings.TrimSpace(string(req.CacheIntent().Retention())), // swobu:io-string source=boundary
	}
}

func NewOpenAIPolicy() ProviderRoutePolicy { return openAIProviderRoutePolicy{} }

func NewOllamaPolicy() ProviderRoutePolicy { return ollamaProviderRoutePolicy{} }

func NewOpenAICompatiblePolicy() ProviderRoutePolicy {
	return openAICompatibleProviderRoutePolicy{}
}

func NewOpenRouterPolicy() ProviderRoutePolicy { return openRouterProviderRoutePolicy{} }
