package clientconnect

import (
	"encoding/json"
	"fmt"
	"strings"
)

const ClientOpenClaw ClientID = "openclaw"

const openClawEndpointKey = "models.providers.swobu.baseUrl"

var openClawAdapter = adapter{
	id:          ClientOpenClaw,
	name:        "OpenClaw",
	present:     openClawPresent,
	planCurrent: planOpenClawCurrent,
}

func openClawPresent(s *Service) (bool, error) {
	if strings.TrimSpace(s.getenv("OPENCLAW_NIX_MODE")) == "1" {
		return false, nil
	}
	return commandClientPresent("openclaw")(s)
}

func planOpenClawCurrent(s *Service, target Target) (plannedMutation, error) {
	if strings.TrimSpace(s.getenv("OPENCLAW_NIX_MODE")) == "1" {
		return plannedMutation{}, openClawNoChange(fmt.Errorf("Nix mode makes configuration immutable"))
	}
	modelRef, source, err := readOpenClawModelRef(s)
	if err != nil {
		return plannedMutation{}, openClawNoChange(err)
	}
	locusRaw, err := requireCommandOutput(s, "OpenClaw", "openclaw", "config", "file")
	if err != nil {
		return plannedMutation{}, openClawNoChange(err)
	}
	locus := strings.TrimSpace(string(locusRaw))
	if locus == "" {
		return plannedMutation{}, openClawNoChange(fmt.Errorf("config file returned no path"))
	}

	before, exists, err := readOpenClawOptionalString(s, openClawEndpointKey)
	if err != nil {
		return plannedMutation{}, openClawNoChange(err)
	}
	providerRaw, providerExists, providerErr := readOpenClawOptionalJSON(s, "models.providers.swobu")
	if providerErr != nil {
		return plannedMutation{}, openClawNoChange(providerErr)
	}
	var observedProvider map[string]any
	if providerExists && json.Unmarshal(providerRaw, &observedProvider) != nil {
		return plannedMutation{}, openClawNoChange(fmt.Errorf("models.providers.swobu has incompatible structure"))
	}
	api, apiExists := observedProvider["api"].(string)
	apiKey, apiKeyExists := observedProvider["apiKey"].(string)
	defaultExists := false
	if models, ok := observedProvider["models"].([]any); ok {
		for _, item := range models {
			if object, ok := item.(map[string]any); ok && object["id"] == "default" {
				defaultExists = true
			}
		}
	}
	allowlistRaw, allowlistExists, allowlistErr := readOpenClawOptionalJSON(s, "agents.defaults.models")
	if allowlistErr != nil {
		return plannedMutation{}, openClawNoChange(allowlistErr)
	}
	allowlistSatisfied := true
	if allowlistExists {
		var allowlist map[string]any
		if json.Unmarshal(allowlistRaw, &allowlist) != nil {
			return plannedMutation{}, openClawNoChange(fmt.Errorf("agents.defaults.models has incompatible structure"))
		}
		_, allowlistSatisfied = allowlist["swobu/default"]
	}
	changes := semanticChange("backend", modelRef, true, "swobu/default")
	changes = append(changes, semanticChange("endpoint", before, exists, target.WorkspaceURL())...)
	changes = append(changes, semanticChange("protocol", api, apiExists, "openai-completions")...)
	if !apiKeyExists || apiKey == "" {
		changes = append(changes, semanticChange("API key placeholder", "", false, "swobu")...)
	}
	changes = append(changes, semanticChange("default model", fmt.Sprint(defaultExists), defaultExists, "true")...)
	if allowlistExists && !allowlistSatisfied {
		changes = append(changes, Change{Field: "allowlist membership", After: "swobu/default"})
	}
	plan := Plan{
		ConfigPath: locus,
		Target:     target,
		Changes:    changes,
	}
	if plan.AlreadyConfigured() {
		return plannedMutation{plan: plan}, nil
	}
	providerValue := map[string]any{"baseUrl": target.WorkspaceURL(), "api": "openai-completions", "apiKey": "swobu", "models": []any{map[string]any{"id": "default", "name": "Swobu default"}}}
	if providerExists {
		if err := json.Unmarshal(providerRaw, &providerValue); err != nil {
			return plannedMutation{}, openClawNoChange(fmt.Errorf("models.providers.swobu has incompatible structure"))
		}
		providerValue["baseUrl"], providerValue["api"] = target.WorkspaceURL(), "openai-completions"
		if !apiKeyExists || apiKey == "" {
			providerValue["apiKey"] = "swobu"
		}
		models, ok := providerValue["models"].([]any)
		if providerValue["models"] != nil && !ok {
			return plannedMutation{}, openClawNoChange(fmt.Errorf("models.providers.swobu.models is not an array"))
		}
		found := false
		for _, item := range models {
			if object, ok := item.(map[string]any); ok && object["id"] == "default" {
				found = true
			}
		}
		if !found {
			models = append(models, map[string]any{"id": "default", "name": "Swobu default"})
		}
		providerValue["models"] = models
	}
	providerJSON, _ := json.Marshal(providerValue)
	allowlistJSON := ""
	if allowlistExists {
		var allowlist map[string]any
		if json.Unmarshal(allowlistRaw, &allowlist) != nil {
			return plannedMutation{}, openClawNoChange(fmt.Errorf("agents.defaults.models has incompatible structure"))
		}
		if _, present := allowlist["swobu/default"]; !present {
			allowlist["swobu/default"] = map[string]any{}
		}
		encoded, _ := json.Marshal(allowlist)
		allowlistJSON = string(encoded)
	}
	return plannedMutation{plan: plan, apply: func() error {
		sets := [][]string{
			{"config", "set", "models.providers.swobu", string(providerJSON), "--json"},
		}
		if allowlistJSON != "" {
			sets = append(sets, []string{"config", "set", "agents.defaults.models", allowlistJSON, "--json"})
		}
		sets = append(sets, []string{"config", "set", source, "swobu/default"})
		for _, args := range sets {
			_, code, runErr := s.run("openclaw", args...)
			if runErr != nil {
				return fmt.Errorf("OpenClaw could not start its configuration command")
			}
			if code != 0 {
				return fmt.Errorf("OpenClaw configuration command exited %d", code)
			}
		}
		return nil
	}}, nil
}

func readOpenClawOptionalJSON(s *Service, path string) ([]byte, bool, error) {
	stdout, code, err := s.run("openclaw", "config", "get", path, "--json")
	if err != nil {
		return nil, false, fmt.Errorf("could not inspect %s", path)
	}
	if code == 0 {
		var value any
		if json.Unmarshal(stdout, &value) != nil {
			return nil, false, fmt.Errorf("%s is not JSON", path)
		}
		return stdout, true, nil
	}
	if code == 1 && len(strings.TrimSpace(string(stdout))) == 0 {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("inspection of %s exited %d", path, code)
}

func readOpenClawModelRef(s *Service) (value, source string, err error) {
	const primary = "agents.defaults.model.primary"
	value, exists, err := readOpenClawOptionalString(s, primary)
	if err != nil {
		return "", "", err
	}
	if exists {
		return value, primary, nil
	}
	const legacy = "agents.defaults.model"
	value, exists, err = readOpenClawOptionalString(s, legacy)
	if err != nil {
		return "", "", err
	}
	if !exists {
		return "", "", fmt.Errorf("durable default model is not configured")
	}
	return value, legacy, nil
}

func readOpenClawOptionalString(s *Service, path string) (string, bool, error) {
	stdout, code, err := s.run("openclaw", "config", "get", path, "--json")
	if err != nil {
		return "", false, fmt.Errorf("could not inspect %s", path)
	}
	if code == 0 {
		var value string
		if err := json.Unmarshal(stdout, &value); err != nil {
			return "", false, fmt.Errorf("%s is not a string", path)
		}
		return value, true, nil
	}
	if code == 1 && len(strings.TrimSpace(string(stdout))) == 0 {
		return "", false, nil
	}
	return "", false, fmt.Errorf("inspection of %s exited %d", path, code)
}

func openClawNoChange(err error) error {
	return fmt.Errorf("OpenClaw is not automatically wireable: %v. Nothing changed.", err)
}
