package provider

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// ProviderID identifies one fixed provider implementation.
type ProviderID string

type targetOptionsKind uint8

const (
	targetOptionsNone targetOptionsKind = iota
	targetOptionsCustom
	targetOptionsBedrock
)

type customTargetOptions struct {
	authHeader string
}

type bedrockTargetOptions struct {
	region string
}

type targetOptions struct {
	kind    targetOptionsKind
	custom  customTargetOptions
	bedrock bedrockTargetOptions
}

// TargetSnapshot is the complete execution projection of one configured routing
// target. Provider-specific facts (the custom auth header, the Bedrock signing
// region) are fixed during construction by a provider-specific constructor and
// exposed only through accessors, so no incomplete snapshot can be completed by
// post-construction mutation. It carries no workspace, route, fallback, or
// attempt state.
type TargetSnapshot struct {
	TargetID         string
	TargetVersion    uint64
	ProviderSpec     string
	BaseURL          string
	CredentialRef    string
	options          targetOptions
	Model            string
	ProtocolKind     protocolkind.ProtocolKind
	ProviderProtocol string
	ProviderDelivery delivery.Delivery
}

// AuthHeader returns the custom-endpoint auth header name. It is empty unless
// the target was constructed with NewCustomTargetSnapshot.
func (t TargetSnapshot) AuthHeader() string {
	if t.options.kind != targetOptionsCustom {
		return ""
	}
	return t.options.custom.authHeader
}

// BedrockRegion returns the durable AWS signing region. It is empty unless the
// target was constructed with NewBedrockTargetSnapshot.
func (t TargetSnapshot) BedrockRegion() string {
	if t.options.kind != targetOptionsBedrock {
		return ""
	}
	return t.options.bedrock.region
}

// Clone returns a detached target value.
func (t TargetSnapshot) Clone() TargetSnapshot { return t }

// Equal reports exact target equality. Backend resolution must preserve this
// value so exchange never has two target authorities that can disagree.
func (t TargetSnapshot) Equal(other TargetSnapshot) bool { return t == other }

// ProviderID returns the fixed provider implementation identifier.
func (t TargetSnapshot) ProviderID() string { return t.ProviderSpec }

// ValidateExecutionProtocol proves that the concrete provider protocol names
// the same semantic family and upstream delivery as this target projection.
// Client delivery remains transient Exchange input and is intentionally not
// stored here.
func (t TargetSnapshot) ValidateExecutionProtocol() error {
	if err := t.validateProviderOptions(); err != nil {
		return err
	}
	providerProtocol := strings.TrimSpace(t.ProviderProtocol)
	if providerProtocol == "" || t.ProtocolKind == "" {
		return fmt.Errorf("provider execution protocol is incomplete")
	}
	if err := t.ProviderDelivery.Validate(); err != nil {
		return fmt.Errorf("provider delivery is invalid: %w", err)
	}
	streaming := strings.HasSuffix(providerProtocol, "_stream")
	semantic := strings.TrimSuffix(providerProtocol, "_stream")
	if semantic != t.ProtocolKind.String() {
		return fmt.Errorf("provider protocol %q contradicts protocol kind %q", providerProtocol, t.ProtocolKind)
	}
	if streaming {
		if t.ProviderDelivery != delivery.StreamingDelivery(delivery.FramingSSE) {
			return fmt.Errorf("streaming provider protocol %q requires SSE delivery", providerProtocol)
		}
	} else if t.ProviderDelivery != delivery.BufferedDelivery() {
		return fmt.Errorf("buffered provider protocol %q requires buffered delivery", providerProtocol)
	}
	return nil
}

func (t TargetSnapshot) validateProviderOptions() error {
	switch strings.TrimSpace(t.ProviderSpec) {
	case "bedrock":
		if t.options.kind != targetOptionsBedrock || strings.TrimSpace(t.options.bedrock.region) == "" {
			return fmt.Errorf("bedrock target requires Bedrock options with a signing region")
		}
	case "custom":
		if t.options.kind != targetOptionsCustom {
			return fmt.Errorf("custom target requires Custom options")
		}
	default:
		if t.options.kind != targetOptionsNone {
			return fmt.Errorf("provider %q cannot carry provider-specific target options", t.ProviderSpec)
		}
	}
	return nil
}

// newTargetSnapshot is the shared builder for the common execution fields. The
// provider-specific constructors complete the provider-specific facts before
// returning, so the mutation here is implementation detail inside construction,
// never a lifecycle mutation visible to callers.
func newTargetSnapshot(targetID, providerSpec, baseURL, credentialRef string, protocol protocolkind.ProtocolKind, providerProtocol string, providerDelivery delivery.Delivery) TargetSnapshot {
	return TargetSnapshot{
		TargetID:         targetID,
		TargetVersion:    1,
		ProviderSpec:     providerSpec,
		BaseURL:          baseURL,
		CredentialRef:    credentialRef,
		ProtocolKind:     protocol,
		ProviderProtocol: providerProtocol,
		ProviderDelivery: providerDelivery,
	}
}

// NewTargetSnapshot constructs one provider execution target projection for a
// provider whose execution carries no provider-specific fact. providerProtocol
// is the exact concrete provider protocol name. providerDelivery is the
// delivery selected by that concrete provider contract and must be supplied by
// the profile/exchange boundary; it is never derived here.
func NewTargetSnapshot(targetID, providerSpec, baseURL, credentialRef string, protocol protocolkind.ProtocolKind, providerProtocol string, providerDelivery delivery.Delivery) TargetSnapshot {
	switch strings.TrimSpace(providerSpec) {
	case "bedrock", "custom":
		panic("provider-specific target requires its specialized constructor")
	}
	return newTargetSnapshot(targetID, providerSpec, baseURL, credentialRef, protocol, providerProtocol, providerDelivery)
}

// NewCustomTargetSnapshot constructs a custom-endpoint execution target carrying
// the auth header name as a provider-specific fact fixed at construction.
func NewCustomTargetSnapshot(targetID, baseURL, credentialRef string, protocol protocolkind.ProtocolKind, providerProtocol, authHeader string, providerDelivery delivery.Delivery) TargetSnapshot {
	target := newTargetSnapshot(targetID, "custom", baseURL, credentialRef, protocol, providerProtocol, providerDelivery)
	target.options = targetOptions{kind: targetOptionsCustom, custom: customTargetOptions{authHeader: strings.TrimSpace(authHeader)}}
	return target
}

// NewBedrockTargetSnapshot constructs a Bedrock execution target carrying the
// durable AWS signing region as a provider-specific fact fixed at construction.
// The endpoint host never owns the signing region; region is an authored
// first-class fact threaded here, not parsed from the endpoint URL.
func NewBedrockTargetSnapshot(targetID, baseURL, credentialRef string, protocol protocolkind.ProtocolKind, providerProtocol, region string, providerDelivery delivery.Delivery) TargetSnapshot {
	region = strings.TrimSpace(region)
	if region == "" {
		panic("bedrock target requires a signing region")
	}
	target := newTargetSnapshot(targetID, "bedrock", baseURL, credentialRef, protocol, providerProtocol, providerDelivery)
	target.options = targetOptions{kind: targetOptionsBedrock, bedrock: bedrockTargetOptions{region: region}}
	return target
}
