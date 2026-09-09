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
	leaves := openCodeLeaves(target)
	values := make([]openCodeValue, len(leaves))
	var writeFile foreignFile
	editor := jsonEditor{allowComments: true}
	for i, path := range paths {
		file, err := inspectForeignFile(path, []byte("{}\n"))
		if err != nil {
			return plannedMutation{}, openCodeProblem(err)
		}
		if file.existed || (!writeFile.existed && i == len(paths)-1) {
			writeFile = file
		}
		if !file.existed {
			continue
		}
		var raw json.RawMessage
		if exists, err := editor.Value(file.raw, keyPath{"providers"}, &raw); err != nil {
			return plannedMutation{}, openCodeProblem(err)
		} else if exists {
			return plannedMutation{}, openCodeProblem(fmt.Errorf("native V2 providers configuration is not supported"))
		}
		for i, leaf := range leaves {
			if leaf.replace {
				value, exists, err := editor.String(file.raw, leaf.path)
				if err != nil {
					return plannedMutation{}, openCodeProblem(err)
				}
				if exists {
					values[i] = openCodeValue{value: value, exists: true}
				}
				continue
			}
			var value json.RawMessage
			exists, err := editor.Value(file.raw, leaf.path, &value)
			if err != nil {
				return plannedMutation{}, openCodeProblem(err)
			}
			values[i].exists = values[i].exists || exists
		}
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
