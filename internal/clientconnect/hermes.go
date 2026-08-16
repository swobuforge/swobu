package clientconnect

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const ClientHermes ClientID = "hermes"

var hermesAdapter = adapter{id: ClientHermes, name: "Hermes Agent", present: commandClientPresent("hermes"), planCurrent: planHermesCurrent}

func planHermesCurrent(s *Service, target Target) (plannedMutation, error) {
	raw, err := requireCommandOutput(s, "Hermes Agent", "hermes", "config", "get", "model", "--json")
	if err != nil {
		return plannedMutation{}, hermesNoChange(err)
	}
	var model map[string]any
	if json.Unmarshal(raw, &model) != nil {
		return plannedMutation{}, hermesNoChange(fmt.Errorf("model config is not an object"))
	}
	provider, _ := model["provider"].(string)
	selected, _ := model["default"].(string)
	base, _ := model["base_url"].(string)
	locusRaw, err := requireCommandOutput(s, "Hermes Agent", "hermes", "config", "path")
	if err != nil {
		return plannedMutation{}, hermesNoChange(err)
	}
	locus := strings.TrimSpace(string(locusRaw))
	if locus == "" {
		return plannedMutation{}, hermesNoChange(fmt.Errorf("config path is empty"))
	}
	changes := semanticChange("backend", provider+"/"+selected, provider != "" || selected != "", "custom/default")
	changes = append(changes, semanticChange("endpoint", base, base != "", target.WorkspaceURL())...)
	plan := Plan{ConfigPath: locus, Target: target, Changes: changes}
	if plan.AlreadyConfigured() {
		return plannedMutation{plan: plan}, nil
	}
	file, err := inspectForeignFile(locus, nil)
	if err != nil || !file.existed {
		return plannedMutation{}, hermesNoChange(fmt.Errorf("config.yaml is required"))
	}
	next, err := replaceHermesModel(file.raw, target.WorkspaceURL())
	if err != nil {
		return plannedMutation{}, hermesNoChange(err)
	}
	return plannedMutation{plan: plan, apply: func() error { return file.replace(next) }}, nil
}

// replaceHermesModel performs one source-preserving model-block edit because
// no sequence of model leaf writes has harmless committed prefixes. YAML is
// parsed only to establish structural and line authority; unrelated source is
// never serialized by Swobu.
func replaceHermesModel(raw []byte, endpoint string) ([]byte, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("config.yaml is invalid: %w", err)
	}
	if len(document.Content) == 0 {
		return nil, fmt.Errorf("config.yaml root is empty")
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config.yaml root is not an object")
	}
	modelKey, model, modelCount := yamlMappingPair(root, "model")
	if modelCount == 0 {
		return nil, fmt.Errorf("model config is missing")
	}
	if modelCount != 1 {
		return nil, fmt.Errorf("model config is duplicated")
	}
	if model.Kind != yaml.MappingNode || model.Style&yaml.FlowStyle != 0 {
		return nil, fmt.Errorf("model config is not a block-style object")
	}
	lines := strings.SplitAfter(string(raw), "\n")
	blockEnd := len(lines)
	for i := 0; i+1 < len(root.Content); i += 2 {
		value := root.Content[i+1]
		if value == model && i+2 < len(root.Content) {
			blockEnd = root.Content[i+2].Line - 1
			break
		}
	}
	indent := "  "
	if len(model.Content) >= 2 && model.Content[0].Column > 1 {
		indent = strings.Repeat(" ", model.Content[0].Column-1)
	}
	values := map[string]string{"provider": "custom", "default": "default", "base_url": endpoint}
	missing := []string(nil)
	ownedLines := map[int]string{}
	for _, key := range []string{"provider", "default", "base_url"} {
		keyNode, valueNode, count := yamlMappingPair(model, key)
		if count == 0 {
			missing = append(missing, key)
			continue
		}
		if count != 1 {
			return nil, fmt.Errorf("model.%s is duplicated", key)
		}
		if valueNode.Kind != yaml.ScalarNode || !admittedYAMLScalarStyle(valueNode.Style) || keyNode.Line != valueNode.Line {
			return nil, fmt.Errorf("model.%s is not a single-line scalar", key)
		}
		if valueNode.Anchor != "" {
			return nil, fmt.Errorf("model.%s has an unsupported anchor", key)
		}
		if previous, exists := ownedLines[keyNode.Line]; exists {
			return nil, fmt.Errorf("model.%s shares a physical line with model.%s", key, previous)
		}
		ownedLines[keyNode.Line] = key
		lineIndex := keyNode.Line - 1
		ending := sourceLineEnding(lines[lineIndex])
		comment := ""
		if valueNode.LineComment != "" {
			comment = " " + valueNode.LineComment
		}
		lines[lineIndex] = indent + key + ": " + renderYAMLScalar(values[key], valueNode.Style) + comment + ending
	}
	if len(missing) > 0 {
		insert := make([]string, 0, len(missing))
		ending := sourceLineEnding(lines[modelKey.Line-1])
		if ending == "" {
			ending = preferredLineEnding(raw)
		}
		if blockEnd > 0 && sourceLineEnding(lines[blockEnd-1]) == "" {
			lines[blockEnd-1] += ending
		}
		for _, key := range missing {
			insert = append(insert, indent+key+": "+values[key]+ending)
		}
		lines = append(lines[:blockEnd], append(insert, lines[blockEnd:]...)...)
	}
	return []byte(strings.Join(lines, "")), nil
}

func yamlMappingPair(mapping *yaml.Node, key string) (keyNode, valueNode *yaml.Node, count int) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			keyNode, valueNode = mapping.Content[i], mapping.Content[i+1]
			count++
		}
	}
	return keyNode, valueNode, count
}

func admittedYAMLScalarStyle(style yaml.Style) bool {
	return style == 0 || style == yaml.SingleQuotedStyle || style == yaml.DoubleQuotedStyle
}

func renderYAMLScalar(value string, style yaml.Style) string {
	switch style {
	case yaml.SingleQuotedStyle:
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	case yaml.DoubleQuotedStyle:
		return strconv.Quote(value)
	default:
		return value
	}
}

func sourceLineEnding(line string) string {
	if strings.HasSuffix(line, "\r\n") {
		return "\r\n"
	}
	if strings.HasSuffix(line, "\n") {
		return "\n"
	}
	return ""
}

func preferredLineEnding(raw []byte) string {
	if strings.Contains(string(raw), "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func hermesNoChange(err error) error {
	return fmt.Errorf("Hermes Agent is not automatically wireable: %v. Nothing changed.", err)
}
