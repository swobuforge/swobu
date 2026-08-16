package clientconnect

import (
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
	home, err := s.homeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".config", "kilo")
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
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("Kilo Code requires a supported global kilo.jsonc or kilo.json")
}

func planKiloCurrent(s *Service, target Target) (plannedMutation, error) {
	for _, key := range []string{"KILO_PROVIDER", "KILO_CONFIG", "KILO_CONFIG_DIR", "KILO_CONFIG_CONTENT"} {
		if strings.TrimSpace(s.getenv(key)) != "" {
			return plannedMutation{}, kiloNoChange(fmt.Errorf("%s selects another effective configuration", key))
		}
	}
	path, err := s.kiloPath()
	if err != nil {
		return plannedMutation{}, kiloNoChange(err)
	}
	return planKilo(path, target)
}

func planKilo(path string, target Target) (plannedMutation, error) {
	file, err := inspectForeignFile(path, nil)
	if err != nil {
		return plannedMutation{}, kiloNoChange(err)
	}
	editor := jsonEditor{allowComments: true}
	model, modelExists, err := editor.String(file.raw, keyPath{"model"})
	if err != nil {
		return plannedMutation{}, kiloNoChange(err)
	}
	endpoint, endpointExists, err := editor.String(file.raw, keyPath{"provider", "swobu", "options", "baseURL"})
	if err != nil {
		return plannedMutation{}, kiloNoChange(err)
	}
	_, nameExists, err := editor.String(file.raw, keyPath{"provider", "swobu", "name"})
	if err != nil {
		return plannedMutation{}, kiloNoChange(err)
	}
	var defaultModel map[string]any
	defaultModelExists, err := editor.Value(file.raw, keyPath{"provider", "swobu", "models", "default"}, &defaultModel)
	if err != nil {
		return plannedMutation{}, kiloNoChange(err)
	}
	var toolCall bool
	toolCallExists, err := editor.Value(file.raw, keyPath{"provider", "swobu", "models", "default", "tool_call"}, &toolCall)
	if err != nil {
		return plannedMutation{}, kiloNoChange(err)
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
	plan := Plan{ConfigPath: file.logical, Target: target, Changes: changes}
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
		return plannedMutation{}, kiloNoChange(err)
	}
	next, err = setJSONValue(editor, next, keyPath{"provider", "swobu", "models", "default", "tool_call"}, true)
	if err != nil {
		return plannedMutation{}, kiloNoChange(err)
	}
	return plannedMutation{plan: plan, apply: func() error { return file.replace(next) }}, nil
}

func kiloNoChange(err error) error { return fmt.Errorf("Kilo Code %v. Nothing changed.", err) }
