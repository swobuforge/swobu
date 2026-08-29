package messages

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
)

// swobu:lint ignore string-switch because=protocol boundary decodes Messages tool_choice variants.
func decodeMessagesToolChoice(raw json.RawMessage, tools []canonical.ToolDeclaration, changeLog *[]compat.Change, exchangeID string) (canonical.ToolPolicy, error) {
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
		toolType := canonical.ToolTypeFunction
		if name == canonical.WebSearchToolKey().Name() {
			toolType = canonical.ToolTypeWebSearch
		}
		resolved, _, err := canonical.ResolveToolDeclarationByName(tools, name, toolType)
		if err != nil {
			return canonical.ToolPolicy{}, err
		}
		specificID := resolved.Key()
		return canonical.NewToolPolicy(canonical.ToolPolicySpecific, &specificID), nil
	default:
		return canonical.ToolPolicy{}, canonical.BadRequest("messages request tool_choice is invalid")
	}
}

func encodeMessagesToolChoice(policy canonical.ToolPolicy, lowered wire.LoweredToolSet, names wire.ToolNames, changeLog *[]compat.Change, exchangeID string) (any, error) {
	if lowered.TotalFragments() == 0 {
		if policy.Mode == canonical.ToolPolicyRequired || policy.Mode == canonical.ToolPolicySpecific {
			if changeLog != nil {
				*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestToolPolicy, canonical.Occurrence{}))
			}
		}
		// Empty surviving surfaces are inert for soft policies. Omit the
		// backend-visible field rather than emitting a no-op choice.
		return nil, nil
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
		key, ok := policy.SpecificID()
		if !ok {
			return nil, canonical.BadRequest("specific tool policy requires a tool id")
		}
		record, ok := lowered.FindSource(key)
		if !ok || record.FragmentCount != 1 {
			if changeLog != nil {
				*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestToolPolicy, canonical.Occurrence{}))
			}
			return nil, nil
		}
		switch record.Kind {
		case canonical.ToolKindFunction, canonical.ToolKindDiscovery:
			name, err := wire.EncodeToolName(names, record.Key)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"type": "tool",
				"name": strings.TrimSpace(name),
			}, nil
		default:
			if changeLog != nil {
				*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestToolPolicy, canonical.Occurrence{}))
			}
			return nil, nil
		}
	default:
		return nil, canonical.BadRequest("messages request tool_choice is invalid")
	}
}
