package providercompat

import (
	"context"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
)

// CommitEffects writes one effect batch through the caller's sink.
// Provider adapters use this after a boundary codec returns exchange.Result.
func CommitEffects(ctx context.Context, sink effect.Sink, exchangeID string, effects []effect.Effect) error {
	if sink == nil || len(effects) == 0 {
		return nil
	}
	return sink.Commit(ctx, exchangeID, effects)
}

// RouteSubject returns the canonical route subject used by provider-edge
// compatibility decisions.
func RouteSubject(providerID string, protocol string) compat.Subject {
	protocol = strings.TrimSpace(protocol) // swobu:io-string source=boundary
	if providerID == "" || protocol == "" {
		return ""
	}
	return compat.Subject("route:provider/" + providerID + "/protocol/" + protocol)
}

// EmitStructuredOutputDecisions records the exact structured-output projection
// bundle when the canonical request asks for JSON-schema output and the route
// is already known to support it.
func EmitStructuredOutputDecisions(ctx context.Context, sink effect.Sink, exchangeID string, providerID string, protocol protocolkind.ProtocolKind, outputFormat canonical.OutputFormat) error {
	if sink == nil || outputFormat.Kind != canonical.OutputFormatJSONSchema {
		return nil
	}
	subject := RouteSubject(providerID, string(protocol))
	if subject == "" {
		return nil
	}
	return sink.Commit(ctx, exchangeID, []effect.Effect{
		effect.CompatibilityEffect{
			Feature: compat.OutputFormat,
			Outcome: compat.Exact,
			Subject: subject,
		},
		effect.CompatibilityEffect{
			Feature: compat.OutputJSONSchema,
			Outcome: compat.Exact,
			Subject: subject,
		},
		effect.CompatibilityEffect{
			Feature: compat.WireJSONMode,
			Outcome: compat.Exact,
			Subject: subject,
		},
	})
}

// EmitToolSchemaStrictDecision records whether strict function-tool schemas are
// preserved exactly or dropped by the selected provider wire surface.
func EmitToolSchemaStrictDecision(ctx context.Context, sink effect.Sink, exchangeID string, providerID string, protocol protocolkind.ProtocolKind, tools []canonical.ToolDecl, preserved bool) error {
	if sink == nil || !hasStrictFunctionTool(tools) {
		return nil
	}
	subject := RouteSubject(providerID, string(protocol))
	if subject == "" {
		return nil
	}
	outcome := compat.Drop
	if preserved {
		outcome = compat.Exact
	}
	return sink.Commit(ctx, exchangeID, []effect.Effect{
		effect.CompatibilityEffect{
			Feature: compat.ToolSchemaStrict,
			Outcome: outcome,
			Subject: subject,
		},
	})
}

func hasStrictFunctionTool(tools []canonical.ToolDecl) bool {
	for _, tool := range tools {
		switch decl := tool.(type) {
		case canonical.FunctionToolDecl:
			if decl.Strict != nil && *decl.Strict {
				return true
			}
		case *canonical.FunctionToolDecl:
			if decl != nil && decl.Strict != nil && *decl.Strict {
				return true
			}
		}
	}
	return false
}
