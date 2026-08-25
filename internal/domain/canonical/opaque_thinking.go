package canonical

import "fmt"

type opaqueThinkingKind uint8

const (
	opaqueThinkingMessages opaqueThinkingKind = iota + 1
	opaqueThinkingProviderChat
	opaqueThinkingResponses
	opaqueThinkingInteractions
)

type opaqueReplayOrigin struct {
	targetID      string
	targetVersion uint64
}

// OpaqueThinking is one complete, reasoning-only replay unit. The private tag
// prevents protocol and exact-provider bytes from being cross-converted.
type OpaqueThinking struct {
	kind              opaqueThinkingKind
	raw               []byte
	providerChatScope ProviderChatReplayScope
	responsesItemID   string
	origin            *opaqueReplayOrigin
}

// ProviderChatReplayScope identifies one adapter-owned Chat replay dialect.
// Canonical preserves this opaque exact token but does not assign provider
// identity or meaning to it.
type ProviderChatReplayScope string

// ResponsesReasoningReplay is consumed by stateless Responses request
// encoding to restore provider-hidden reasoning state on a later invocation.
// ItemID is the optional exact Responses wire id paired with EncryptedContent;
// it is preserved verbatim through ingress, decode, clone, and replay.
type ResponsesReasoningReplay struct {
	EncryptedContent string
	ItemID           string
}

func NewResponsesOpaqueThinking(replay ResponsesReasoningReplay) (OpaqueThinking, error) {
	if replay.EncryptedContent == "" {
		return OpaqueThinking{}, BadRequest("responses encrypted reasoning is empty")
	}
	return OpaqueThinking{
		kind:            opaqueThinkingResponses,
		raw:             []byte(replay.EncryptedContent),
		responsesItemID: replay.ItemID,
	}, nil
}

func (o OpaqueThinking) Responses() (ResponsesReasoningReplay, bool) {
	if o.kind != opaqueThinkingResponses || len(o.raw) == 0 {
		return ResponsesReasoningReplay{}, false
	}
	return ResponsesReasoningReplay{EncryptedContent: string(o.raw), ItemID: o.responsesItemID}, true
}

// NewMessagesOpaqueThinking admits one non-empty complete Messages block.
func NewMessagesOpaqueThinking(raw []byte) (OpaqueThinking, error) {
	return newOpaqueThinking(opaqueThinkingMessages, raw, "messages opaque thinking is empty")
}

// NewInteractionsOpaqueThinking admits one exact Interactions thought replay
// unit. Its provider-private grammar remains owned by the Interactions adapter.
func NewInteractionsOpaqueThinking(raw []byte) (OpaqueThinking, error) {
	return newOpaqueThinking(opaqueThinkingInteractions, raw, "interactions opaque thinking is empty")
}

// NewProviderChatOpaqueThinking admits one complete non-empty provider-owned
// Chat replay unit. The opaque scope is the replay boundary: only an adapter
// presenting the exact same scope can retrieve its raw bytes.
func NewProviderChatOpaqueThinking(scope ProviderChatReplayScope, raw []byte) (OpaqueThinking, error) {
	if scope == "" {
		return OpaqueThinking{}, BadRequest("provider Chat opaque thinking scope is empty")
	}
	if len(raw) == 0 {
		return OpaqueThinking{}, BadRequest("provider Chat opaque thinking is empty")
	}
	return OpaqueThinking{
		kind:              opaqueThinkingProviderChat,
		raw:               append([]byte(nil), raw...),
		providerChatScope: scope,
	}, nil
}

func newOpaqueThinking(kind opaqueThinkingKind, raw []byte, emptyMessage string) (OpaqueThinking, error) {
	if len(raw) == 0 {
		return OpaqueThinking{}, BadRequest(emptyMessage)
	}
	return OpaqueThinking{kind: kind, raw: append([]byte(nil), raw...)}, nil
}

// withTargetOrigin returns a copy bound to the specified target generation.
// If already bound to the same target generation, it is returned unchanged.
// Rebinding to a different target generation returns an error.
func (o OpaqueThinking) withTargetOrigin(targetID string, targetVersion uint64) (OpaqueThinking, error) {
	if o.IsZero() {
		return OpaqueThinking{}, nil
	}
	if targetID == "" || targetVersion == 0 {
		return OpaqueThinking{}, fmt.Errorf("target origin requires non-empty targetID and non-zero targetVersion")
	}
	if o.origin != nil {
		if o.origin.targetID == targetID && o.origin.targetVersion == targetVersion {
			return o.Clone(), nil
		}
		return OpaqueThinking{}, fmt.Errorf("opaque thinking cannot be rebound to a different target: existing (%s, %d) vs new (%s, %d)", o.origin.targetID, o.origin.targetVersion, targetID, targetVersion)
	}
	cloned := o.Clone()
	cloned.origin = &opaqueReplayOrigin{
		targetID:      targetID,
		targetVersion: targetVersion,
	}
	return cloned, nil
}

// MatchesTarget reports whether this opaque thinking was produced by and is
// eligible for replay to the exact specified target generation.
func (o OpaqueThinking) MatchesTarget(targetID string, targetVersion uint64) bool {
	if o.IsZero() || o.origin == nil || targetID == "" || targetVersion == 0 {
		return false
	}
	return o.origin.targetID == targetID && o.origin.targetVersion == targetVersion
}

// Messages returns independent bytes only for the Messages branch.
func (o OpaqueThinking) Messages() ([]byte, bool) {
	if o.kind != opaqueThinkingMessages || len(o.raw) == 0 {
		return nil, false
	}
	return append([]byte(nil), o.raw...), true
}

// Interactions returns independent bytes only for the Interactions branch.
func (o OpaqueThinking) Interactions() ([]byte, bool) {
	if o.kind != opaqueThinkingInteractions || len(o.raw) == 0 {
		return nil, false
	}
	return append([]byte(nil), o.raw...), true
}

// ProviderChat returns independent bytes only when scope exactly matches the
// provider-owned Chat replay unit's construction scope.
func (o OpaqueThinking) ProviderChat(scope ProviderChatReplayScope) ([]byte, bool) {
	if o.kind != opaqueThinkingProviderChat || scope == "" || scope != o.providerChatScope || len(o.raw) == 0 {
		return nil, false
	}
	return append([]byte(nil), o.raw...), true
}

// IsZero reports whether no replay state is populated.
func (o OpaqueThinking) IsZero() bool {
	return o.kind == 0 && len(o.raw) == 0 && o.providerChatScope == "" && o.responsesItemID == "" && o.origin == nil
}

// Clone returns an independent replay value.
func (o OpaqueThinking) Clone() OpaqueThinking {
	cloned := OpaqueThinking{
		kind:              o.kind,
		raw:               append([]byte(nil), o.raw...),
		providerChatScope: o.providerChatScope,
		responsesItemID:   o.responsesItemID,
	}
	if o.origin != nil {
		cloned.origin = &opaqueReplayOrigin{
			targetID:      o.origin.targetID,
			targetVersion: o.origin.targetVersion,
		}
	}
	return cloned
}

func (OpaqueThinking) String() string   { return "<opaque>" }
func (OpaqueThinking) GoString() string { return "<opaque>" }

func (o OpaqueThinking) validate() error {
	if o.IsZero() {
		return nil
	}
	if (o.kind != opaqueThinkingMessages && o.kind != opaqueThinkingProviderChat && o.kind != opaqueThinkingResponses && o.kind != opaqueThinkingInteractions) || len(o.raw) == 0 {
		return fmt.Errorf("opaque thinking is invalid")
	}
	// Scope equality is the provider Chat replay boundary. Protocol branches
	// must not carry an adapter-owned scope.
	if (o.kind == opaqueThinkingProviderChat) != (o.providerChatScope != "") {
		return fmt.Errorf("provider Chat opaque thinking scope is invalid")
	}
	// A Responses wire id is replay-affecting only for the Responses branch.
	// Messages and provider Chat branches must never carry one.
	if o.responsesItemID != "" && o.kind != opaqueThinkingResponses {
		return fmt.Errorf("responses reasoning id on a non-responses opaque thinking")
	}
	if o.origin != nil {
		if o.origin.targetID == "" || o.origin.targetVersion == 0 {
			return fmt.Errorf("opaque thinking replay origin is invalid")
		}
	}
	return nil
}
