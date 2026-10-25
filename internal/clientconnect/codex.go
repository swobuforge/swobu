package clientconnect

import (
	"context"
	"fmt"
	"github.com/pelletier/go-toml/v2"
	"path/filepath"
	"strings"
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

func planCodexCurrent(_ context.Context, s *Service, target Target) (plannedMutation, error) {
	path, err := s.codexPath()
	if err != nil {
		return plannedMutation{}, err
	}
	return planCodex(path, target, s.getenv)
}

func planCodex(path string, target Target, getenv func(string) string) (plannedMutation, error) {
	file, err := inspectForeignFile(path, nil)
	if err != nil {
		return plannedMutation{}, codexProblem(err)
	}
	editor := tomlEditor{}
	model, modelExists, err := editor.String(file.raw, keyPath{"model"})
	if err != nil {
		return plannedMutation{}, codexProblem(err)
	}
	provider, providerExists, err := editor.String(file.raw, keyPath{"model_provider"})
	if err != nil {
		return plannedMutation{}, codexProblem(err)
	}
	endpoint, endpointExists, err := editor.String(file.raw, codexEndpointPath)
	if err != nil {
		return plannedMutation{}, codexProblem(err)
	}
	var decoded struct {
		Providers map[string]struct {
			RequiresOpenAI bool           `toml:"requires_openai_auth"`
			Auth           map[string]any `toml:"auth"`
			AWS            map[string]any `toml:"aws"`
		} `toml:"model_providers"`
	}
	if err := toml.Unmarshal(file.raw, &decoded); err != nil {
		return plannedMutation{}, codexProblem(fmt.Errorf("invalid provider configuration"))
	}
	binding := decoded.Providers["swobu"]
	if binding.RequiresOpenAI || binding.Auth != nil || binding.AWS != nil {
		return plannedMutation{}, codexProblem(fmt.Errorf("Swobu provider uses external authentication; remove requires_openai_auth, auth or aws before reviewing Connect"))
	}
	authPath := keyPath{"model_providers", "swobu", "experimental_bearer_token"}
	credential, credentialExists, err := editor.String(file.raw, authPath)
	if err != nil {
		return plannedMutation{}, codexProblem(err)
	}
	bearerExists := credentialExists
	if !credentialExists {
		credential, credentialExists, err = editor.String(file.raw, keyPath{"model_providers", "swobu", "http_headers", "Authorization"})
		if err != nil {
			return plannedMutation{}, codexProblem(err)
		}
		credential = strings.TrimPrefix(credential, "Bearer ")
	}
	envKey, envExists, err := editor.String(file.raw, keyPath{"model_providers", "swobu", "env_key"})
	if err != nil {
		return plannedMutation{}, codexProblem(err)
	}
	if envExists {
		credential, credentialExists = getenv(envKey), true
	}
	envHeader, headerExists, err := editor.String(file.raw, keyPath{"model_providers", "swobu", "env_http_headers", "Authorization"})
	if err != nil {
		return plannedMutation{}, codexProblem(err)
	}
	if headerExists && !envExists && !binding.RequiresOpenAI {
		if value := getenv(envHeader); value != "" && !bearerExists {
			credential, credentialExists = strings.TrimPrefix(value, "Bearer "), true
		}
	}
	_, nameExists, err := editor.String(file.raw, keyPath{"model_providers", "swobu", "name"})
	if err != nil {
		return plannedMutation{}, codexProblem(err)
	}
	wire, wireExists, err := editor.String(file.raw, keyPath{"model_providers", "swobu", "wire_api"})
	if err != nil {
		return plannedMutation{}, codexProblem(err)
	}
	changes := semanticChange("backend", provider+"/"+model, providerExists || modelExists, "swobu/"+target.ModelID())
	changes = append(changes, semanticChange("endpoint", endpoint, endpointExists, target.WorkspaceURL())...)
	if !nameExists {
		changes = append(changes, semanticChange("provider name", "", false, "Swobu")...)
	}
	changes = append(changes, semanticChange("protocol", wire, wireExists, "responses")...)
	changes = append(changes, credentialChange(credential, credentialExists, target)...)
	plan := Plan{ConfigPaths: []string{file.logical}, Target: target, Changes: changes, credential: credential}
	if plan.AlreadyConfigured() {
		return plannedMutation{plan: plan}, nil
	}
	next := file.raw
	if credential != target.bearer || target.bearer == "" {
		// Replace the owned provider's competing sources together. A valid
		// user-managed source is left intact when Access accepts its value.
		for _, path := range []keyPath{
			{"model_providers", "swobu", "env_key"},
			{"model_providers", "swobu", "http_headers", "Authorization"},
			{"model_providers", "swobu", "env_http_headers", "Authorization"},
		} {
			next, err = editor.DeleteString(next, path)
			if err != nil {
				return plannedMutation{}, codexProblem(err)
			}
		}
		next, err = editor.SetString(next, authPath, target.bearer)
		if err != nil {
			return plannedMutation{}, codexProblem(err)
		}
	}

	edits := []struct {
		path  keyPath
		value string
	}{
		{keyPath{"model"}, target.ModelID()}, {keyPath{"model_provider"}, "swobu"},
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
			return plannedMutation{}, codexProblem(err)
		}
	}
	return plannedMutation{plan: plan, apply: func(context.Context) error { return file.replace(next) }}, nil
}

func codexProblem(err error) error {
	return fmt.Errorf("Codex configuration: %w", err)
}
