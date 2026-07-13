package messages

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
)

// swobu:lint ignore string-switch because=protocol boundary decodes Messages tool_choice variants.
func decodeMessagesToolChoice(raw json.RawMessage, tools []canonical.ToolDecl, sink effect.Sink, exchangeID string) (canonical.ToolPolicy, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		if len(tools) > 0 {
			return canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil), nil
		}
		return canonical.NewToolPolicy(canonical.ToolPolicyNone, nil), nil
	}

	var stringMode string
	if err := json.Unmarshal([]byte(trimmed), &stringMode); err == nil {
		switch strings.ToLower(strings.TrimSpace(stringMode)) { // swobu:io-string source=domain
		case "auto":
			return canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil), nil
		case "none":
			return canonical.NewToolPolicy(canonical.ToolPolicyNone, nil), nil
		case "any":
			return canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil), nil
		default:
			return canonical.ToolPolicy{}, canonical.BadRequest("messages request tool_choice is invalid")
		}
	}

	var objectMode struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(trimmed), &objectMode); err != nil {
		return canonical.ToolPolicy{}, canonical.BadRequest("messages request tool_choice is invalid")
	}
	switch strings.ToLower(strings.TrimSpace(objectMode.Type)) { // swobu:io-string source=domain
	case "auto":
		return canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil), nil
	case "none":
		return canonical.NewToolPolicy(canonical.ToolPolicyNone, nil), nil
	case "any":
		return canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil), nil
	case "tool":
		name := strings.TrimSpace(objectMode.Name) // swobu:io-string source=provider-wire
		if name == "" {
			return canonical.ToolPolicy{}, canonical.BadRequest("messages request tool_choice specific requires a tool name")
		}
		projected := strings.Contains(name, "__")
		resolved, _, err := canonical.ResolveToolDeclByName(tools, name, canonical.ToolTypeFunction)
		if err != nil {
			if projected {
				if emitErr := emitMessagesToolNameNamespaceDecision(sink, exchangeID, nil, compat.Reject, compat.Subject("wire:/tool_choice/name")); emitErr != nil {
					return canonical.ToolPolicy{}, emitErr
				}
			}
			return canonical.ToolPolicy{}, err
		}
		if projected {
			if err := emitMessagesToolNameNamespaceDecision(sink, exchangeID, nil, compat.Exact, compat.Subject("wire:/tool_choice/name")); err != nil {
				return canonical.ToolPolicy{}, err
			}
		}
		specificID := resolved.ToolID()
		return canonical.NewToolPolicy(canonical.ToolPolicySpecific, &specificID), nil
	default:
		return canonical.ToolPolicy{}, canonical.BadRequest("messages request tool_choice is invalid")
	}
}

func encodeMessagesToolChoice(policy canonical.ToolPolicy, tools []canonical.ToolDecl, sink effect.Sink, exchangeID string) (any, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if len(tools) == 0 {
		switch policy.Mode {
		case canonical.ToolPolicyRequired:
			return nil, canonical.BadRequest("messages request tool_choice required requires at least one tool")
		case canonical.ToolPolicySpecific:
			return nil, canonical.BadRequest("messages request tool_choice specific requires a tool id")
		default:
			// Empty tool surfaces are inert here. Omit the backend-visible
			// field rather than emitting a no-op choice some backends reject.
			return nil, nil
		}
	}
	switch policy.Mode {
	case canonical.ToolPolicyNone:
		return map[string]any{
			"type": "none",
		}, nil
	case canonical.ToolPolicyAuto:
		return map[string]any{
			"type": "auto",
		}, nil
	case canonical.ToolPolicyRequired:
		return map[string]any{
			"type": "any",
		}, nil
	case canonical.ToolPolicySpecific:
		specific, ok := policy.SpecificID()
		if !ok {
			return nil, canonical.BadRequest("messages request tool_choice specific requires a tool id")
		}
		decl, _, err := canonical.ResolveToolDeclByID(tools, specific, canonical.ToolTypeFunction)
		if err != nil {
			return nil, err
		}
		name, err := canonical.ProjectedToolName(decl)
		if err != nil {
			return nil, err
		}
		if err := emitMessagesToolNameNamespaceDecision(sink, exchangeID, decl, compat.Approx, compat.Subject("wire:/tool_choice/name")); err != nil {
			return nil, err
		}
		return map[string]any{
			"type": "tool",
			"name": name,
		}, nil
	default:
		return nil, canonical.BadRequest("messages request tool_choice is invalid")
	}
}
