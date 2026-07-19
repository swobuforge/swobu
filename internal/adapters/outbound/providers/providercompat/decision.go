package providercompat

import (
	"context"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// CommitDecisions writes one evidence batch best effort. Compatibility evidence
// persistence is non-authoritative and cannot change a codec decision.
func CommitDecisions(ctx context.Context, sink compat.Sink, exchangeID string, decisions []compat.Decision) error {
	if sink == nil || len(decisions) == 0 {
		return nil
	}
	_ = sink.Commit(ctx, exchangeID, decisions)
	return nil
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
func EmitStructuredOutputDecisions(ctx context.Context, sink compat.Sink, exchangeID string, providerID string, protocol protocolkind.ProtocolKind, outputFormat canonical.OutputFormat) error {
	decisions := StructuredOutputDecisions(providerID, protocol, outputFormat)
	if sink == nil || len(decisions) == 0 {
		return nil
	}
	_ = sink.Commit(ctx, exchangeID, decisions)
	return nil
}

// StructuredOutputDecisions returns exact lowering evidence without recording it.
func StructuredOutputDecisions(providerID string, protocol protocolkind.ProtocolKind, outputFormat canonical.OutputFormat) []compat.Decision {
	if outputFormat.Kind != canonical.OutputFormatJSONSchema {
		return nil
	}
	subject := RouteSubject(providerID, string(protocol))
	if subject == "" {
		return nil
	}
	return []compat.Decision{
		{Feature: compat.RequestOutputFormat, Outcome: compat.Exact, Subject: subject},
		{Feature: compat.RequestOutputFormatSchema, Outcome: compat.Exact, Subject: subject},
		{Feature: compat.WireJSONMode, Outcome: compat.Exact, Subject: subject},
	}
}

// EmitToolSchemaStrictDecision records whether strict function-tool schemas are
// preserved exactly or dropped by the selected provider wire surface.
func EmitToolSchemaStrictDecision(ctx context.Context, sink compat.Sink, exchangeID string, providerID string, protocol protocolkind.ProtocolKind, tools []canonical.ToolDecl, preserved bool) error {
	decision, ok := ToolSchemaStrictDecision(providerID, protocol, tools, preserved)
	if sink == nil || !ok {
		return nil
	}
	_ = sink.Commit(ctx, exchangeID, []compat.Decision{
		{
			Feature: decision.Feature,
			Outcome: decision.Outcome,
			Subject: decision.Subject,
		},
	})
	return nil
}

// ToolSchemaStrictDecision returns strict-schema lowering evidence without recording it.
func ToolSchemaStrictDecision(providerID string, protocol protocolkind.ProtocolKind, tools []canonical.ToolDecl, preserved bool) (compat.Decision, bool) {
	if !hasStrictFunctionTool(tools) {
		return compat.Decision{}, false
	}
	subject := RouteSubject(providerID, string(protocol))
	if subject == "" {
		return compat.Decision{}, false
	}
	outcome := compat.Drop
	if preserved {
		outcome = compat.Exact
	}
	return compat.Decision{Feature: compat.RequestToolsSchemaStrict, Outcome: outcome, Subject: subject}, true
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
