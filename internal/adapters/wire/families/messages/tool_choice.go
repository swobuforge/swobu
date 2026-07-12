package messages

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// swobu:lint ignore string-switch because=protocol boundary decodes Messages tool_choice variants.
func decodeMessagesToolChoice(raw json.RawMessage, tools []canonical.ToolDecl) (canonical.ToolPolicy, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		if len(tools) > 0 {
			return canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil), nil
		}
		return canonical.NewToolPolicy(canonical.ToolPolicyNone, nil), nil
	}

	var stringMode string
	if err := json.Unmarshal([]byte(trimmed), &stringMode); err == nil {
		switch strings.ToLower(strings.TrimSpace(stringMode)) {
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
	switch strings.ToLower(strings.TrimSpace(objectMode.Type)) {
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
		resolved, _, err := canonical.ResolveToolDeclByName(tools, name, canonical.ToolTypeFunction)
		if err != nil {
			return canonical.ToolPolicy{}, err
		}
		specificID := resolved.ToolID()
		return canonical.NewToolPolicy(canonical.ToolPolicySpecific, &specificID), nil
	default:
		return canonical.ToolPolicy{}, canonical.BadRequest("messages request tool_choice is invalid")
	}
}

func encodeMessagesToolChoice(policy canonical.ToolPolicy, tools []canonical.ToolDecl) (any, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
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
		if len(tools) == 0 {
			return nil, canonical.BadRequest("messages request tool_choice required requires at least one tool")
		}
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
		return map[string]any{
			"type": "tool",
			"name": name,
		}, nil
	default:
		return nil, canonical.BadRequest("messages request tool_choice is invalid")
	}
}
