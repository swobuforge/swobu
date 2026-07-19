package chatcompletions

import (
	"context"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func emitChatCompletionsToolNameNamespaceDecision(sink compat.Sink, exchangeID string, tool canonical.ToolDecl, outcome compat.Outcome, subject compat.Subject) error {
	if sink == nil || subject == "" {
		return nil
	}
	if tool != nil && !strings.Contains(strings.TrimSpace(tool.ToolID().Path), "/") { // swobu:io-string source=boundary
		return nil
	}
	if err := sink.Commit(context.Background(), exchangeID, []compat.Decision{
		compat.Decision{
			Feature: compat.ToolNameNamespace,
			Outcome: outcome,
			Subject: subject,
		},
	}); err != nil {
		return canonical.InternalError("compatibility decision sink commit failed")
	}
	return nil
}
