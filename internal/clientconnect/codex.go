package clientconnect

import (
	"fmt"
	"path/filepath"
)

const ClientCodex ClientID = "codex"

var codexEndpointPath = keyPath{"model_providers", "swobu", "base_url"}

var codexAdapter = adapter{
	id:          ClientCodex,
	name:        "Codex CLI",
	present:     codexPresent,
	planCurrent: planCodexCurrent,
}

func (s *Service) codexPath() (string, error) {
	dir := s.getenv("CODEX_HOME")
	if dir == "" {
		home, err := s.homeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".codex")
	}
	return filepath.Join(dir, "config.toml"), nil
}

func codexPresent(s *Service) (bool, error) {
	path, err := s.codexPath()
	if err != nil {
		return false, err
	}
	return binaryOrRegularFilePresent(s, "codex", path)
}

func planCodexCurrent(s *Service, target Target) (plannedMutation, error) {
	path, err := s.codexPath()
	if err != nil {
		return plannedMutation{}, err
	}
	return planCodex(path, target)
}

func planCodex(path string, target Target) (plannedMutation, error) {
	file, err := inspectForeignFile(path, nil)
	if err != nil {
		return plannedMutation{}, codexNoChange(err)
	}
	editor := tomlEditor{}
	model, modelExists, err := editor.String(file.raw, keyPath{"model"})
	if err != nil {
		return plannedMutation{}, codexNoChange(err)
	}
	provider, providerExists, err := editor.String(file.raw, keyPath{"model_provider"})
	if err != nil {
		return plannedMutation{}, codexNoChange(err)
	}
	endpoint, endpointExists, err := editor.String(file.raw, codexEndpointPath)
	if err != nil {
		return plannedMutation{}, codexNoChange(err)
	}
	_, nameExists, err := editor.String(file.raw, keyPath{"model_providers", "swobu", "name"})
	if err != nil {
		return plannedMutation{}, codexNoChange(err)
	}
	wire, wireExists, err := editor.String(file.raw, keyPath{"model_providers", "swobu", "wire_api"})
	if err != nil {
		return plannedMutation{}, codexNoChange(err)
	}
	changes := semanticChange("backend", provider+"/"+model, providerExists || modelExists, "swobu/default")
	changes = append(changes, semanticChange("endpoint", endpoint, endpointExists, target.WorkspaceURL())...)
	if !nameExists {
		changes = append(changes, semanticChange("provider name", "", false, "Swobu")...)
	}
	changes = append(changes, semanticChange("protocol", wire, wireExists, "responses")...)
	plan := Plan{ConfigPath: file.logical, Target: target, Changes: changes}
	if plan.AlreadyConfigured() {
		return plannedMutation{plan: plan}, nil
	}
	next := file.raw
	edits := []struct {
		path  keyPath
		value string
	}{
		{keyPath{"model"}, "default"}, {keyPath{"model_provider"}, "swobu"},
		{codexEndpointPath, target.WorkspaceURL()},
		{keyPath{"model_providers", "swobu", "wire_api"}, "responses"},
	}
	if !nameExists {
		edits = append(edits, struct {
			path  keyPath
			value string
		}{keyPath{"model_providers", "swobu", "name"}, "Swobu"})
	}
	for _, change := range edits {
		next, err = editor.SetString(next, change.path, change.value)
		if err != nil {
			return plannedMutation{}, codexNoChange(err)
		}
	}
	return plannedMutation{plan: plan, apply: func() error { return file.replace(next) }}, nil
}

func codexNoChange(err error) error {
	return fmt.Errorf("Codex configuration %v. Nothing changed.", err)
}
