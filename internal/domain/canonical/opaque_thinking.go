package canonical

import "fmt"

type opaqueThinkingKind uint8

const (
	opaqueThinkingMessages opaqueThinkingKind = iota + 1
	opaqueThinkingOpenRouter
	opaqueThinkingResponses
)

// OpaqueThinking is one complete, reasoning-only replay unit. The private tag
// prevents protocol and exact-provider bytes from being cross-converted.
type OpaqueThinking struct {
	kind            opaqueThinkingKind
	raw             []byte
	responsesItemID string
}

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

// NewOpenRouterOpaqueThinking admits one non-empty complete reasoning_details unit.
func NewOpenRouterOpaqueThinking(raw []byte) (OpaqueThinking, error) {
	return newOpaqueThinking(opaqueThinkingOpenRouter, raw, "openrouter opaque thinking is empty")
}

func newOpaqueThinking(kind opaqueThinkingKind, raw []byte, emptyMessage string) (OpaqueThinking, error) {
	if len(raw) == 0 {
		return OpaqueThinking{}, BadRequest(emptyMessage)
	}
	return OpaqueThinking{kind: kind, raw: append([]byte(nil), raw...)}, nil
}

// Messages returns independent bytes only for the Messages branch.
func (o OpaqueThinking) Messages() ([]byte, bool) {
	if o.kind != opaqueThinkingMessages || len(o.raw) == 0 {
		return nil, false
	}
	return append([]byte(nil), o.raw...), true
}

// OpenRouter returns independent bytes only for the OpenRouter branch.
func (o OpaqueThinking) OpenRouter() ([]byte, bool) {
	if o.kind != opaqueThinkingOpenRouter || len(o.raw) == 0 {
		return nil, false
	}
	return append([]byte(nil), o.raw...), true
}

// IsZero reports whether no valid branch is populated.
func (o OpaqueThinking) IsZero() bool { return o.kind == 0 && len(o.raw) == 0 }

// Clone returns an independent replay value.
func (o OpaqueThinking) Clone() OpaqueThinking {
	return OpaqueThinking{kind: o.kind, raw: append([]byte(nil), o.raw...), responsesItemID: o.responsesItemID}
}

func (OpaqueThinking) String() string   { return "<opaque>" }
func (OpaqueThinking) GoString() string { return "<opaque>" }

func (o OpaqueThinking) validate() error {
	if o.IsZero() {
		return nil
	}
	if (o.kind != opaqueThinkingMessages && o.kind != opaqueThinkingOpenRouter && o.kind != opaqueThinkingResponses) || len(o.raw) == 0 {
		return fmt.Errorf("opaque thinking is invalid")
	}
	// A Responses wire id is replay-affecting only for the Responses branch.
	// Messages and OpenRouter branches must never carry one.
	if o.responsesItemID != "" && o.kind != opaqueThinkingResponses {
		return fmt.Errorf("responses reasoning id on a non-responses opaque thinking")
	}
	return nil
}
