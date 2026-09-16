package clientconnect

import (
	"context"
	"fmt"
	"path/filepath"
)

const ClientAntigravity ClientID = "antigravity"

var antigravityProviderPath = keyPath{"modelProvider"}

var antigravityAdapter = adapter{id: ClientAntigravity, name: "Antigravity CLI", targetOptional: true, present: antigravityPresent, planCurrent: planAntigravityCurrent}

func (s *Service) antigravityPath() (string, error) {
	home, err := s.homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gemini", "antigravity-cli", "settings.json"), nil
}

func antigravityPresent(s *Service) (bool, error) {
	path, err := s.antigravityPath()
	if err != nil {
		return false, err
	}
	return binaryOrRegularFilePresent(s, "agy", path)
}
func planAntigravityCurrent(_ context.Context, s *Service, target Target) (plannedMutation, error) {
	path, err := s.antigravityPath()
	if err != nil {
		return plannedMutation{}, err
	}
	return planAntigravity(path, target)
}

func planAntigravity(path string, target Target) (plannedMutation, error) {
	file, err := inspectForeignFile(path, []byte("{}\n"))
	if err != nil {
		return plannedMutation{}, antigravityProblem(err)
	}
	editor := jsonEditor{}
	provider, exists, err := editor.String(file.raw, antigravityProviderPath)
	if err != nil {
		return plannedMutation{}, antigravityProblem(err)
	}
	plan := Plan{ConfigPaths: []string{file.logical}, Target: target, Changes: semanticChange("model provider", provider, exists, "gemini")}
	if plan.AlreadyConfigured() {
		return plannedMutation{plan: plan}, nil
	}
	next, err := setJSONStrings(editor, file.raw, jsonStringChange{path: antigravityProviderPath, value: "gemini"})
	if err != nil {
		return plannedMutation{}, antigravityProblem(err)
	}
	return plannedMutation{plan: plan, apply: func(context.Context) error { return file.replace(next) }}, nil
}

func antigravityProblem(err error) error { return fmt.Errorf("Antigravity CLI: %w", err) }
