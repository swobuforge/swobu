package protocolcodec

import (
	"encoding/json"
	"fmt"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
)

// ProviderChatReplayForMessage associates one lowered assistant message with
// at most one exact provider Chat opaque replay item. It scans only the
// message's source range, ignores foreign scopes, and rejects duplicate
// matching scopes before provider-specific wire injection.
func ProviderChatReplayForMessage(message chatcompletions.ProviderRequestMessage, items []canonical.CanonicalItem, scope canonical.ProviderChatReplayScope) ([]byte, bool, error) {
	if message.Role != "assistant" || message.SourceStart < 0 || message.SourceStart >= len(items) || message.SourceEnd <= message.SourceStart {
		return nil, false, nil
	}
	end := message.SourceEnd
	if end > len(items) {
		end = len(items)
	}
	var replay []byte
	for source := message.SourceStart; source < end; source++ {
		reasoning, ok := items[source].Reasoning()
		if !ok {
			continue
		}
		raw, ok := reasoning.Opaque().ProviderChat(scope)
		if !ok {
			continue
		}
		if replay != nil {
			return nil, false, canonical.InternalError("checkpoint contains duplicate provider Chat opaque thinking for one assistant message")
		}
		replay = raw
	}
	if replay == nil {
		return nil, false, nil
	}
	return replay, true, nil
}

// ChatOpaqueReplayStringMessageRule decorates the assistant message with a string
// reasoning field extracted from matching opaque thinking in canonical history.
func ChatOpaqueReplayStringMessageRule(scope canonical.ProviderChatReplayScope, fieldName string) chatcompletions.MessageLoweringRule {
	return func(msg *chatcompletions.ProviderRequestMessage, items []canonical.CanonicalItem) error {
		raw, ok, err := ProviderChatReplayForMessage(*msg, items, scope)
		if err != nil || !ok {
			return err
		}
		return msg.SetExtra(fieldName, string(raw))
	}
}

// ChatOpaqueReplayJSONMessageRule decorates the assistant message with a raw JSON
// reasoning field extracted from matching opaque thinking in canonical history.
func ChatOpaqueReplayJSONMessageRule(scope canonical.ProviderChatReplayScope, fieldName string) chatcompletions.MessageLoweringRule {
	return func(msg *chatcompletions.ProviderRequestMessage, items []canonical.CanonicalItem) error {
		raw, ok, err := ProviderChatReplayForMessage(*msg, items, scope)
		if err != nil || !ok {
			return err
		}
		if !json.Valid(raw) {
			return canonical.InternalError(fmt.Sprintf("checkpoint contains invalid %s JSON", scope))
		}
		return msg.SetExtra(fieldName, json.RawMessage(append([]byte(nil), raw...)))
	}
}
