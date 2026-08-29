package chatgpt

import (
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func newBackendCodec(_ string) protocolcodec.Codec {
	falseVal := false
	return protocolcodec.Codec{
		Protocol: protocolkind.Responses,
		ResponsesDialect: protocolcodec.ResponsesDialect{
			ForceArrayInput:     true,
			HistoryMessageRole:  lowerHistorySystemRole,
			OmitMaxOutputTokens: true,
			DefaultStore:        &falseVal,
			RequireStreamingSSE: true,
		},
	}
}

// ChatGPT's Codex Responses endpoint rejects system roles in input history.
// Preserve the directive's position and control-message semantics with the
// provider's accepted developer role; request-scoped instructions remain in
// the exact top-level instructions field.
func lowerHistorySystemRole(index int, role canonical.MessageRole) (canonical.MessageRole, []compat.Change, error) {
	if role != canonical.MessageRoleSystem {
		return role, nil, nil
	}
	return canonical.MessageRoleDeveloper, []compat.Change{
		compat.NewApproximation(canonical.RequestItemsMessageRole, canonical.RequestItemOccurrence(uint32(index))),
	}, nil
}
