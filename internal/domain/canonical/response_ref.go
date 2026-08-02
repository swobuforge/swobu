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

// ResponsesResponseID is an opaque response identity issued by one
// Responses-compatible provider. It is never a Swobu checkpoint key or client ID.
type ResponsesResponseID string

// NewResponsesResponseID preserves one provider-issued identity exactly.
func NewResponsesResponseID(raw string) ResponsesResponseID {
	return ResponsesResponseID(raw)
}

func (id ResponsesResponseID) String() string { return string(id) }
func (id ResponsesResponseID) isBlank() bool {
	return strings.TrimSpace(string(id)) == "" // swobu:io-string source=domain
}

// ResponsesItemID is one provider/dialect-owned item identity carried by a
// Responses output item (for example a web_search_call id beginning with "ws").
// It is not canonical call correlation: a ToolCallID pairs a call with its
// later result, while a ResponsesItemID is the exact provider presentation id
// that must be preserved verbatim on re-encode and may be absent when the
// dialect omits it. It is never a Swobu checkpoint key, client id, or
// correlation token.
type ResponsesItemID string

// NewResponsesItemID preserves one provider-issued item identity exactly. A
// blank value is rejected so the type cannot carry an omitted id.
func NewResponsesItemID(raw string) (ResponsesItemID, error) {
	if raw == "" || strings.TrimSpace(raw) != raw { // swobu:io-string source=boundary
		return "", errors.New("responses item id is blank")
	}
	return ResponsesItemID(raw), nil
}

func (id ResponsesItemID) String() string { return string(id) }
func (id ResponsesItemID) IsZero() bool {
	return strings.TrimSpace(string(id)) == "" // swobu:io-string source=domain
}

// ResponseRef is the shared identity of a completed response and a later
// request that selects it. Provider-native handles remain optional typed children.
type ResponseRef struct {
	SwobuID   SwobuResponseID
	Responses *ResponsesContinuation
}

// ResponsesContinuation is consumed by session target projection to choose
// native Delta continuation only for the exact target generation that created
// the provider response; every other target receives materialized Full history.
type ResponsesContinuation struct {
	ProviderResponseID ResponsesResponseID
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
func (r ResponsesContinuation) ValidateBound() error {
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
func (r ResponsesContinuation) AppliesTo(targetID string, targetVersion uint64) bool {
	return r.ValidateBound() == nil && r.TargetID == targetID && r.TargetVersion == targetVersion
}
