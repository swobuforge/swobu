package canonical

import (
	"errors"
	"strings"
)

// SwobuResponseID is the Swobu-owned public identity for one completed response.
// The same value addresses a session checkpoint and appears in a later previous_response_id.
type SwobuResponseID string

// NewSwobuResponseID preserves one client-visible response identity exactly.
// Validation rejects blank values at checkpoint lookup and persistence boundaries.
func NewSwobuResponseID(raw string) SwobuResponseID {
	return SwobuResponseID(raw)
}

func (id SwobuResponseID) String() string { return string(id) }
func (id SwobuResponseID) IsZero() bool {
	return strings.TrimSpace(string(id)) == "" // swobu:io-string source=domain
}

// ResponsesNativeResponseID is an opaque response identity issued by one
// Responses-compatible provider. It is never a Swobu checkpoint key or client ID.
type ResponsesNativeResponseID string

// NewResponsesNativeResponseID preserves one provider-issued identity exactly.
func NewResponsesNativeResponseID(raw string) ResponsesNativeResponseID {
	return ResponsesNativeResponseID(raw)
}

func (id ResponsesNativeResponseID) String() string { return string(id) }
func (id ResponsesNativeResponseID) isBlank() bool {
	return strings.TrimSpace(string(id)) == "" // swobu:io-string source=domain
}

// ResponseRef is the shared identity of a completed response and a later
// request that selects it. Provider-native handles remain optional typed children.
type ResponseRef struct {
	SwobuID   SwobuResponseID
	Responses *ResponsesNativeRef
}

// ResponsesNativeRef preserves a Responses-native provider handle and the
// exact routing target generation for which that handle is valid.
type ResponsesNativeRef struct {
	ProviderResponseID ResponsesNativeResponseID
	TargetID           string
	TargetVersion      uint64
}

// ValidatePreviousResponseSelector requires the public Swobu identity used for
// checkpoint lookup.
func (r ResponseRef) ValidatePreviousResponseSelector() error {
	if r.SwobuID.IsZero() {
		return errors.New("response reference requires a Swobu response ID")
	}
	return nil
}

// ValidateCommittedResponse requires checkpoint identity and a fully bound native
// handle when one was captured.
func (r ResponseRef) ValidateCommittedResponse() error {
	if r.SwobuID.IsZero() {
		return errors.New("committed response requires a Swobu response ID")
	}
	if r.Responses != nil {
		return r.Responses.ValidateBound()
	}
	return nil
}

// ValidateBound requires every fact needed to safely reuse a native provider
// response ID for one exact target generation.
func (r ResponsesNativeRef) ValidateBound() error {
	if r.ProviderResponseID.isBlank() {
		return errors.New("Responses native reference requires a provider response ID")
	}
	if strings.TrimSpace(r.TargetID) == "" { // swobu:io-string source=domain
		return errors.New("Responses native reference requires a target ID")
	}
	if r.TargetVersion == 0 {
		return errors.New("Responses native reference requires a target version")
	}
	return nil
}

func (r ResponseRef) Clone() ResponseRef {
	cloned := ResponseRef{SwobuID: r.SwobuID}
	if r.Responses != nil {
		responses := *r.Responses
		cloned.Responses = &responses
	}
	return cloned
}

// AppliesTo reports whether the Responses-native handle is valid for one
// exact routing-owned target generation.
func (r ResponsesNativeRef) AppliesTo(targetID string, targetVersion uint64) bool {
	return r.ValidateBound() == nil && r.TargetID == targetID && r.TargetVersion == targetVersion
}
