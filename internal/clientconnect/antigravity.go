package clientconnect

import (
	"context"
	"fmt"
	"path/filepath"
)

const ClientAntigravity ClientID = "antigravity"

var antigravityProviderPath = keyPath{"modelProvider"}

const (
	antigravityProfileStart = "# >>> swobu antigravity >>>"
	antigravityProfileEnd   = "# <<< swobu antigravity <<<"
)

var antigravityAdapter = adapter{id: ClientAntigravity, name: "Antigravity CLI", present: antigravityPresent, planCurrent: planAntigravityCurrent}

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
	environment, err := inspectAntigravityEnvironment(s, target)
	if err != nil {
		return plannedMutation{}, antigravityProblem(err)
	}
	return planAntigravity(path, target, environment)
}

func planAntigravity(path string, target Target, environment antigravityEnvironmentPlan) (plannedMutation, error) {
	file, err := inspectForeignFile(path, []byte("{}\n"))
	if err != nil {
		return plannedMutation{}, antigravityProblem(err)
	}
	editor := jsonEditor{}
	provider, exists, err := editor.String(file.raw, antigravityProviderPath)
	if err != nil {
		return plannedMutation{}, antigravityProblem(err)
	}
	providerChanges := semanticChange("model provider", provider, exists, "gemini")
	changes := append([]Change(nil), environment.changes...)
	changes = append(changes, providerChanges...)
	paths := append([]string(nil), environment.configPaths...)
	paths = append(paths, file.logical)
	plan := Plan{ConfigPaths: paths, Target: target, Changes: changes}
	if plan.AlreadyConfigured() {
		return plannedMutation{plan: plan}, nil
	}
	var replaceSettings func() error
	if len(providerChanges) != 0 {
		next, err := setJSONStrings(editor, file.raw, jsonStringChange{path: antigravityProviderPath, value: "gemini"})
		if err != nil {
			return plannedMutation{}, antigravityProblem(err)
		}
		replaceSettings = func() error { return file.replace(next) }
	}
	return plannedMutation{plan: plan, apply: func(ctx context.Context) error {
		// Persist the environment first. Every committed prefix remains usable:
		// Antigravity enters Gemini mode only after its required environment exists.
		if environment.apply != nil {
			if err := environment.apply(ctx); err != nil {
				return err
			}
		}
		if replaceSettings != nil {
			return replaceSettings()
		}
		return nil
	}}, nil
}

type antigravityEnvironmentPlan struct {
	configPaths []string
	changes     []Change
	apply       func(context.Context) error
}

func antigravityProfileBlock(endpoint string) []byte {
	return []byte(antigravityProfileStart + "\n" +
		`export GEMINI_API_KEY="${GEMINI_API_KEY:-swobu-local}"` + "\n" +
		"export GOOGLE_GEMINI_BASE_URL='" + endpoint + "'\n" +
		antigravityProfileEnd + "\n")
}

func antigravityProblem(err error) error { return fmt.Errorf("Antigravity CLI: %w", err) }
