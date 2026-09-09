package clientconnect

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const ClientHermes ClientID = "hermes"

var hermesAdapter = adapter{id: ClientHermes, name: "Hermes Agent", present: commandClientPresent("hermes"), planCurrent: planHermesCurrent}

func planHermesCurrent(ctx context.Context, s *Service, target Target) (plannedMutation, error) {
	locusRaw, err := requireCommandOutput(ctx, s, "Hermes Agent", "hermes", "config", "path")
	if err != nil {
		return plannedMutation{}, hermesProblem(err)
	}
	locus := strings.TrimSpace(string(locusRaw))
	if locus == "" {
		return plannedMutation{}, hermesProblem(fmt.Errorf("config path is empty"))
	}
	file, err := inspectForeignFile(locus, nil)
	if err != nil {
		return plannedMutation{}, hermesProblem(err)
	}
	model := hermesPersistedModel(file.raw)
	changes := semanticChange("backend", model.provider.value+"/"+model.selected.value, model.provider.exists || model.selected.exists, "custom/default")
	changes = append(changes, semanticChange("endpoint", model.base.value, model.base.exists, target.WorkspaceURL())...)
	plan := Plan{ConfigPaths: []string{locus}, Target: target, Changes: changes}
	if plan.AlreadyConfigured() {
		return plannedMutation{plan: plan}, nil
	}
	next, err := replaceHermesModel(file.raw, target.WorkspaceURL())
	if err != nil {
		return plannedMutation{}, hermesProblem(err)
	}
	return plannedMutation{plan: plan, apply: func(context.Context) error { return file.replace(next) }}, nil
}

type hermesPersistedScalar struct {
	value  string
	exists bool
}

type hermesPersistedModelState struct {
	provider hermesPersistedScalar
	selected hermesPersistedScalar
	base     hermesPersistedScalar
}

func hermesPersistedModel(raw []byte) hermesPersistedModelState {
	if len(raw) == 0 {
		return hermesPersistedModelState{}
	}
	var document yaml.Node
	if yaml.Unmarshal(raw, &document) != nil || len(document.Content) == 0 {
		return hermesPersistedModelState{}
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return hermesPersistedModelState{}
	}
	_, model, count := yamlMappingPair(root, "model")
	if count != 1 || model.Kind != yaml.MappingNode {
		return hermesPersistedModelState{}
	}
	return hermesPersistedModelState{
		provider: hermesPersistedScalarAt(model, "provider"),
		selected: hermesPersistedScalarAt(model, "default"),
		base:     hermesPersistedScalarAt(model, "base_url"),
	}
}

func hermesPersistedScalarAt(model *yaml.Node, key string) hermesPersistedScalar {
	_, value, count := yamlMappingPair(model, key)
	if count != 1 || value.Kind != yaml.ScalarNode {
		return hermesPersistedScalar{}
	}
	return hermesPersistedScalar{value: value.Value, exists: true}
}

// replaceHermesModel performs one source-preserving model-block edit because
// no sequence of model leaf writes has harmless committed prefixes. YAML is
// parsed only to establish structural and line authority; unrelated source is
// never serialized by Swobu.
func replaceHermesModel(raw []byte, endpoint string) ([]byte, error) {
	if len(raw) == 0 {
		return []byte(hermesModelBlock(endpoint)), nil
	}
	var document yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("config.yaml is invalid: %w", err)
	}
	if len(document.Content) == 0 {
		return appendHermesModelBlock(raw, endpoint)
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config.yaml root is not an object")
	}
	modelKey, model, modelCount := yamlMappingPair(root, "model")
	if modelCount == 0 {
		if root.Style&yaml.FlowStyle != 0 {
			return nil, fmt.Errorf("config.yaml root is not a block-style object")
		}
		return appendHermesModelBlock(raw, endpoint)
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

func appendHermesModelBlock(raw []byte, endpoint string) ([]byte, error) {
	ending := preferredLineEnding(raw)
	next := append([]byte(nil), raw...)
	if len(next) > 0 && !strings.HasSuffix(string(next), "\n") {
		next = append(next, ending...)
	}
	next = append(next, hermesModelBlockWithEnding(endpoint, ending)...)
	var documents []yaml.Node
	decoder := yaml.NewDecoder(strings.NewReader(string(next)))
	for {
		var document yaml.Node
		err := decoder.Decode(&document)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("config.yaml cannot safely append model: %w", err)
		}
		if len(document.Content) > 0 {
			documents = append(documents, document)
		}
	}
	if len(documents) != 1 {
		return nil, fmt.Errorf("config.yaml cannot safely append model after a document boundary")
	}
	return next, nil
}

func hermesModelBlock(endpoint string) string {
	return hermesModelBlockWithEnding(endpoint, "\n")
}

func hermesModelBlockWithEnding(endpoint, ending string) string {
	return "model:" + ending +
		"  provider: custom" + ending +
		"  default: default" + ending +
		"  base_url: " + endpoint + ending
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

func hermesProblem(err error) error {
	return fmt.Errorf("Hermes Agent is not automatically wireable: %w", err)
}
