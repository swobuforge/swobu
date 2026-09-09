package clientconnect

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const ClientKilo ClientID = "kilo"

var kiloAdapter = adapter{
	id:          ClientKilo,
	name:        "Kilo Code",
	present:     kiloPresent,
	planCurrent: planKiloCurrent,
}

func (s *Service) kiloConfigCandidates() ([]string, error) {
	dir := s.getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := s.homeDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(home, ".config")
	}
	dir = filepath.Join(dir, "kilo")
	return []string{filepath.Join(dir, "kilo.jsonc"), filepath.Join(dir, "kilo.json")}, nil
}

func kiloPresent(s *Service) (bool, error) {
	paths, err := s.kiloConfigCandidates()
	if err != nil {
		return false, err
	}
	return binaryOrRegularFilePresent(s, "kilo", paths...)
}

func (s *Service) kiloPath() (string, error) {
	paths, err := s.kiloConfigCandidates()
	if err != nil {
		return "", err
	}
	for _, basename := range []string{"opencode.jsonc", "opencode.json"} {
		path := filepath.Join(filepath.Dir(paths[0]), basename)
		if _, err := os.Stat(path); err == nil {
			return "", fmt.Errorf("legacy global %s can override %s; migrate it to Kilo's current config before connecting", basename, filepath.Base(paths[0]))
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	return paths[0], nil
}

func planKiloCurrent(_ context.Context, s *Service, target Target) (plannedMutation, error) {
	for _, key := range []string{"KILO_PROVIDER", "KILO_CONFIG", "KILO_CONFIG_DIR", "KILO_CONFIG_CONTENT"} {
		if strings.TrimSpace(s.getenv(key)) != "" {
			return plannedMutation{}, kiloProblem(fmt.Errorf("%s selects another effective configuration", key))
		}
	}
	path, err := s.kiloPath()
	if err != nil {
		return plannedMutation{}, kiloProblem(err)
	}
	return planKilo(path, target)
}

func planKilo(path string, target Target) (plannedMutation, error) {
	file, err := inspectForeignFile(path, []byte("{}\n"))
	if err != nil {
		return plannedMutation{}, kiloProblem(err)
	}
	editor := jsonEditor{allowComments: true}
	model, modelExists, err := editor.String(file.raw, keyPath{"model"})
	if err != nil {
		return plannedMutation{}, kiloProblem(err)
	}
	endpoint, endpointExists, err := editor.String(file.raw, keyPath{"provider", "swobu", "options", "baseURL"})
	if err != nil {
		return plannedMutation{}, kiloProblem(err)
	}
	_, nameExists, err := editor.String(file.raw, keyPath{"provider", "swobu", "name"})
	if err != nil {
		return plannedMutation{}, kiloProblem(err)
	}
	var defaultModel map[string]any
	defaultModelExists, err := editor.Value(file.raw, keyPath{"provider", "swobu", "models", "default"}, &defaultModel)
	if err != nil {
		return plannedMutation{}, kiloProblem(err)
	}
	var toolCall bool
	toolCallExists, err := editor.Value(file.raw, keyPath{"provider", "swobu", "models", "default", "tool_call"}, &toolCall)
	if err != nil {
		return plannedMutation{}, kiloProblem(err)
	}
	changes := semanticChange("backend", model, modelExists, "swobu/default")
	changes = append(changes, semanticChange("endpoint", endpoint, endpointExists, target.WorkspaceURL())...)
	if !nameExists {
		changes = append(changes, semanticChange("provider name", "", false, "Swobu")...)
	}
	if !defaultModelExists {
		changes = append(changes, semanticChange("default model", "", false, "default")...)
	}
	changes = append(changes, semanticChange("tool calls", fmt.Sprint(toolCall), toolCallExists, "true")...)
	plan := Plan{ConfigPaths: []string{file.logical}, Target: target, Changes: changes}
	if plan.AlreadyConfigured() {
		return plannedMutation{plan: plan}, nil
	}
	stringChanges := []jsonStringChange{
		jsonStringChange{keyPath{"provider", "swobu", "options", "baseURL"}, target.WorkspaceURL()},
		jsonStringChange{keyPath{"model"}, "swobu/default"},
	}
	if !nameExists {
		stringChanges = append(stringChanges, jsonStringChange{keyPath{"provider", "swobu", "name"}, "Swobu"})
	}
	if !defaultModelExists {
		stringChanges = append(stringChanges, jsonStringChange{keyPath{"provider", "swobu", "models", "default", "name"}, "Swobu default"})
	}
	next, err := setJSONStrings(editor, file.raw, stringChanges...)
	if err != nil {
		return plannedMutation{}, kiloProblem(err)
	}
	next, err = setJSONValue(editor, next, keyPath{"provider", "swobu", "models", "default", "tool_call"}, true)
	if err != nil {
		return plannedMutation{}, kiloProblem(err)
	}
	return plannedMutation{plan: plan, apply: func(context.Context) error { return file.replace(next) }}, nil
}

func kiloProblem(err error) error { return fmt.Errorf("Kilo Code: %w", err) }
