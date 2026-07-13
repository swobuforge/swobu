package chatcompletions

import (
	"context"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
)

func emitChatCompletionsToolNameNamespaceDecision(sink effect.Sink, exchangeID string, tool canonical.ToolDecl, outcome compat.Outcome, subject compat.Subject) error {
	if sink == nil || subject == "" {
		return nil
	}
	if tool != nil && !strings.Contains(strings.TrimSpace(tool.ToolID().Path), "/") { // swobu:io-string source=boundary
		return nil
	}
	if err := sink.Commit(context.Background(), exchangeID, []effect.Effect{
		effect.CompatibilityEffect{
			Feature: compat.ToolNameNamespace,
			Outcome: outcome,
			Subject: subject,
		},
	}); err != nil {
		return canonical.InternalError("compatibility effect sink commit failed")
	}
	return nil
}
