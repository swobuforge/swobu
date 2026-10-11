package clientconnect

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
)

const ClientOpenCode ClientID = "opencode"

var openCodeAdapter = adapter{
	id:   ClientOpenCode,
	name: "OpenCode",
	present: func(s *Service) (bool, error) {
		paths, err := s.openCodePaths()
		if err != nil {
			return false, err
		}
		return binaryOrRegularFilePresent(s, "opencode", paths...)
	},
	planCurrent: planOpenCodeCurrent,
}

type openCodeLeaf struct {
	field   string
	path    keyPath
	after   string
	encoded []byte
	replace bool
}

func openCodeLeaves(target Target) []openCodeLeaf {
	return []openCodeLeaf{
		{field: "backend", path: keyPath{"model"}, after: "swobu/default", replace: true},
		{field: "API", path: keyPath{"provider", "swobu", "npm"}, after: "@ai-sdk/openai", replace: true},
		{field: "endpoint", path: keyPath{"provider", "swobu", "options", "baseURL"}, after: target.WorkspaceURL(), replace: true},
		{field: "API key", path: keyPath{"provider", "swobu", "options", "apiKey"}, after: "swobu"},
		{field: "model", path: keyPath{"provider", "swobu", "models", "default"}, after: "default", encoded: []byte(`{}`)},
	}
}

func openCodeV2Leaves(target Target) []openCodeLeaf {
	return []openCodeLeaf{
		{field: "backend", path: keyPath{"model"}, after: "swobu/default", replace: true},
		{field: "provider package", path: keyPath{"providers", "swobu", "package"}, after: "@opencode/ai/providers/openai-compatible"},
		{field: "endpoint", path: keyPath{"providers", "swobu", "settings", "baseURL"}, after: target.WorkspaceURL(), replace: true},
		{field: "model", path: keyPath{"providers", "swobu", "models", "default"}, after: "default", encoded: []byte(`{"name":"Swobu default"}`)},
	}
}

func (s *Service) openCodePaths() ([]string, error) {
	dir := s.getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := s.homeDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(home, ".config")
	}
	dir = filepath.Join(dir, "opencode")
	return []string{filepath.Join(dir, "config.json"), filepath.Join(dir, "opencode.json"), filepath.Join(dir, "opencode.jsonc")}, nil
}

func openCodeProblem(err error) error { return fmt.Errorf("OpenCode: %w", err) }

type openCodeValue struct {
	value  string
	exists bool
}

func planOpenCodeCurrent(_ context.Context, s *Service, target Target) (plannedMutation, error) {
	paths, err := s.openCodePaths()
	if err != nil {
		return plannedMutation{}, openCodeProblem(err)
	}
	editor := jsonEditor{allowComments: true}
	files := make([]foreignFile, len(paths))
	writeIndex := -1
	for i, path := range paths {
		file, err := inspectForeignFile(path, []byte("{}\n"))
		if err != nil {
			return plannedMutation{}, openCodeProblem(err)
		}
		files[i] = file
		if file.existed {
			writeIndex = i
		}
	}
	for index := len(files) - 1; index >= 0; index-- {
		if files[index].existed && openCodeHasProviderSchema(files[index], editor) {
			writeIndex = index
			break
		}
	}
	if writeIndex < 0 {
		writeIndex = len(files) - 1
	}
	writeFile := files[writeIndex]

	leaves, values, err := resolveOpenCodeSchema(files, target, editor)
	if err != nil {
		return plannedMutation{}, openCodeProblem(err)
	}

	var changes []Change
	for i, leaf := range leaves {
		if leaf.replace {
			changes = append(changes, semanticChange(leaf.field, values[i].value, values[i].exists, leaf.after)...)
		} else if !values[i].exists {
			changes = append(changes, semanticChange(leaf.field, "", false, leaf.after)...)
		}
	}
	plan := Plan{ConfigPaths: []string{writeFile.logical}, Target: target, Changes: changes}
	if plan.AlreadyConfigured() {
		return plannedMutation{plan: plan}, nil
	}

	next := writeFile.raw
	for i, leaf := range leaves {
		if values[i].exists && (!leaf.replace || values[i].value == leaf.after) {
			continue
		}
		if leaf.encoded != nil {
			next, err = editor.SetValue(next, leaf.path, leaf.encoded)
		} else {
			next, err = editor.SetString(next, leaf.path, leaf.after)
		}
		if err != nil {
			return plannedMutation{}, openCodeProblem(err)
		}
	}
	return plannedMutation{plan: plan, apply: func(context.Context) error { return writeFile.replace(next) }}, nil
}

func openCodeHasProviderSchema(file foreignFile, editor jsonEditor) bool {
	if !file.existed {
		return false
	}
	for _, key := range []string{"provider", "providers"} {
		var raw json.RawMessage
		exists, err := editor.Value(file.raw, keyPath{key}, &raw)
		if err == nil && exists {
			return true
		}
	}
	return false
}

func resolveOpenCodeSchema(files []foreignFile, target Target, editor jsonEditor) ([]openCodeLeaf, []openCodeValue, error) {
	v1Seen, v2Seen := false, false
	for _, file := range files {
		if !file.existed {
			continue
		}
		for _, key := range []string{"provider", "providers"} {
			var raw json.RawMessage
			exists, err := editor.Value(file.raw, keyPath{key}, &raw)
			if err != nil {
				return nil, nil, err
			}
			if exists {
				if len(raw) == 0 || raw[0] != '{' {
					return nil, nil, fmt.Errorf("OpenCode %s configuration must be an object", key)
				}
				if key == "provider" {
					v1Seen = true
				} else {
					v2Seen = true
				}
			}
		}
	}
	if v1Seen && v2Seen {
		return nil, nil, fmt.Errorf("OpenCode V1 and V2 provider schemas conflict across configuration files")
	}
	if !v2Seen {
		return readOpenCodeLeaves(files, openCodeLeaves(target), editor)
	}
	return readOpenCodeLeaves(files, openCodeV2Leaves(target), editor)
}

func readOpenCodeLeaves(files []foreignFile, leaves []openCodeLeaf, editor jsonEditor) ([]openCodeLeaf, []openCodeValue, error) {
	values := make([]openCodeValue, len(leaves))
	for _, file := range files {
		if !file.existed {
			continue
		}
		for i, leaf := range leaves {
			if leaf.replace {
				value, exists, err := editor.String(file.raw, leaf.path)
				if err != nil {
					return nil, nil, err
				}
				if exists {
					values[i] = openCodeValue{value: value, exists: true}
				}
				continue
			}
			var raw json.RawMessage
			exists, err := editor.Value(file.raw, leaf.path, &raw)
			if err != nil {
				return nil, nil, err
			}
			if exists {
				values[i].exists = true
			}
		}
	}
	return leaves, values, nil
}
