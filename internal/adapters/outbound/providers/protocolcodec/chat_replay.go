package protocolcodec

import (
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
