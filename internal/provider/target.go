package provider

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

const (
	executionFrameHTTPJSONBody = "http_json_body"
	executionFrameSSEEvent     = "sse_event"
)

// ProviderID identifies one fixed provider implementation.
type ProviderID string

// TargetSnapshot is the immutable execution projection of one configured
// routing target. It contains no workspace, route, fallback, or attempt state.
type TargetSnapshot struct {
	TargetID         string
	TargetVersion    uint64
	ProviderSpec     string
	BaseURL          string
	CredentialRef    string
	AuthHeader       string
	Model            string
	ProtocolKind     protocolkind.ProtocolKind
	SelectedFrame    string
	ProviderProtocol string
}

// Clone returns a detached target value.
func (t TargetSnapshot) Clone() TargetSnapshot { return t }

// Equal reports exact target equality. Backend resolution must preserve this
// value so exchange never has two target authorities that can disagree.
func (t TargetSnapshot) Equal(other TargetSnapshot) bool { return t == other }

// ProviderID returns the fixed provider implementation identifier.
func (t TargetSnapshot) ProviderID() string { return t.ProviderSpec }

// ValidateExecutionProtocol proves that protocol name, semantic kind, and
// frame describe one catalog-shaped execution mode. Provider admission remains
// profile-owned; this method rejects internally contradictory projections.
func (t TargetSnapshot) ValidateExecutionProtocol() error {
	providerProtocol := strings.TrimSpace(t.ProviderProtocol)
	if providerProtocol == "" || t.ProtocolKind == "" || t.SelectedFrame == "" {
		return fmt.Errorf("provider execution protocol is incomplete")
	}
	baseProtocol := strings.TrimSuffix(providerProtocol, "_stream")
	if baseProtocol != t.ProtocolKind.String() {
		return fmt.Errorf("provider protocol %q contradicts protocol kind %q", providerProtocol, t.ProtocolKind)
	}
	wantFrame := executionFrameHTTPJSONBody
	if strings.HasSuffix(providerProtocol, "_stream") {
		wantFrame = executionFrameSSEEvent
	}
	if t.SelectedFrame != wantFrame {
		return fmt.Errorf("provider protocol %q requires frame %q, got %q", providerProtocol, wantFrame, t.SelectedFrame)
	}
	return nil
}

// NewTargetSnapshot constructs one provider execution target projection.
func NewTargetSnapshot(targetID, providerSpec, baseURL, credentialRef string, protocol protocolkind.ProtocolKind, selectedFrame string, providerProtocol ...string) TargetSnapshot {
	resolvedProviderProtocol := protocol.String()
	if len(providerProtocol) > 0 {
		resolvedProviderProtocol = providerProtocol[0]
	}
	if selectedFrame == "" {
		selectedFrame = executionFrameHTTPJSONBody
		if strings.HasSuffix(resolvedProviderProtocol, "_stream") {
			selectedFrame = executionFrameSSEEvent
		}
	}
	return TargetSnapshot{
		TargetID:         targetID,
		TargetVersion:    1,
		ProviderSpec:     providerSpec,
		BaseURL:          baseURL,
		CredentialRef:    credentialRef,
		ProtocolKind:     protocol,
		SelectedFrame:    selectedFrame,
		ProviderProtocol: resolvedProviderProtocol,
	}
}
