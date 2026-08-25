package clientconnect

import (
	"encoding/json"
	"fmt"
	"strings"
)

const ClientOpenClaw ClientID = "openclaw"

var openClawAdapter = adapter{
	id:          ClientOpenClaw,
	name:        "OpenClaw",
	present:     openClawPresent,
	planCurrent: planOpenClawCurrent,
}

func openClawPresent(s *Service) (bool, error) {
	return commandClientPresent("openclaw")(s)
}

func planOpenClawCurrent(s *Service, target Target) (plannedMutation, error) {
	if strings.TrimSpace(s.getenv("OPENCLAW_NIX_MODE")) == "1" {
		return plannedMutation{}, openClawNoChange(fmt.Errorf("Nix mode makes configuration immutable"))
	}
	locusRaw, err := requireCommandOutput(s, "OpenClaw", "openclaw", "config", "file")
	if err != nil {
		return plannedMutation{}, openClawNoChange(err)
	}
	locus := strings.TrimSpace(string(locusRaw))
	if locus == "" {
		return plannedMutation{}, openClawNoChange(fmt.Errorf("config file returned no path"))
	}

	modelRef, source, modelExists, err := readModelRef(s)
	if err != nil {
		return plannedMutation{}, openClawNoChange(err)
	}

	observedProvider, _, err := readOptionalJSON(s, "models.providers.swobu")
	if err != nil {
		return plannedMutation{}, openClawNoChange(err)
	}

	allowlist, allowlistExists, err := readOptionalJSON(s, "agents.defaults.models")
	if err != nil {
		return plannedMutation{}, openClawNoChange(err)
	}

	api, apiExists := observedProvider["api"].(string)
	apiKey, apiKeyExists := observedProvider["apiKey"].(string)
	beforeEndpoint, endpointExists := observedProvider["baseUrl"].(string)
	defaultExists := false
	if models, ok := observedProvider["models"].([]any); ok {
		for _, item := range models {
			if object, ok := item.(map[string]any); ok && object["id"] == "default" {
				defaultExists = true
			}
		}
	}

	allowlistSatisfied := true
	if allowlistExists {
		_, allowlistSatisfied = allowlist["swobu/default"]
	}

	changes := semanticChange("backend", modelRef, modelExists, "swobu/default")
	changes = append(changes, semanticChange("endpoint", beforeEndpoint, endpointExists, target.WorkspaceURL())...)
	changes = append(changes, semanticChange("protocol", api, apiExists, "openai-completions")...)
	if !apiKeyExists || apiKey == "" {
		changes = append(changes, semanticChange("API key placeholder", "", false, "swobu")...)
	}
	changes = append(changes, semanticChange("default model", fmt.Sprint(defaultExists), defaultExists, "true")...)
	if allowlistExists && !allowlistSatisfied {
		changes = append(changes, Change{Field: "allowlist membership", After: "swobu/default"})
	}

	plan := Plan{
		ClientID:   ClientOpenClaw,
		ClientName: "OpenClaw",
		ConfigPath: locus,
		Target:     target,
		Changes:    changes,
	}
	if plan.AlreadyConfigured() {
		return plannedMutation{plan: plan}, nil
	}

	return plannedMutation{
		plan: plan,
		apply: func() error {
			freshProvider, freshProviderExists, freshErr := readOptionalJSON(s, "models.providers.swobu")
			if freshErr != nil {
				return openClawNoChange(freshErr)
			}
			providerValue := map[string]any{
				"baseUrl": target.WorkspaceURL(),
				"api":     "openai-completions",
				"apiKey":  "swobu",
				"models":  []any{map[string]any{"id": "default", "name": "Swobu default"}},
			}
			if freshProviderExists && freshProvider != nil {
				providerValue = cloneMap(freshProvider)
				providerValue["baseUrl"], providerValue["api"] = target.WorkspaceURL(), "openai-completions"
				if k, ok := providerValue["apiKey"].(string); !ok || k == "" {
					providerValue["apiKey"] = "swobu"
				}
				models, ok := providerValue["models"].([]any)
				if providerValue["models"] != nil && !ok {
					return openClawNoChange(fmt.Errorf("models.providers.swobu.models is not an array"))
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

			sets := [][]string{
				{"config", "set", "models.providers.swobu", string(providerJSON), "--json"},
			}

			freshAllowlist, freshAllowlistExists, allowlistErr := readOptionalJSON(s, "agents.defaults.models")
			if allowlistErr != nil {
				return openClawNoChange(allowlistErr)
			}
			if freshAllowlistExists {
				updatedAllowlist := cloneMap(freshAllowlist)
				if updatedAllowlist == nil {
					updatedAllowlist = make(map[string]any)
				}
				if _, present := updatedAllowlist["swobu/default"]; !present {
					updatedAllowlist["swobu/default"] = map[string]any{}
				}
				encoded, _ := json.Marshal(updatedAllowlist)
				sets = append(sets, []string{"config", "set", "agents.defaults.models", string(encoded), "--json"})
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
		},
	}, nil
}

func readModelRef(s *Service) (model string, source string, exists bool, err error) {
	primary, primaryExists, primaryErr := readOptionalString(s, "agents.defaults.model.primary")
	if primaryErr != nil {
		return "", "", false, primaryErr
	}
	if primaryExists {
		return primary, "agents.defaults.model.primary", true, nil
	}

	legacy, legacyExists, legacyErr := readOptionalString(s, "agents.defaults.model")
	if legacyErr != nil {
		return "", "", false, legacyErr
	}
	if legacyExists {
		return legacy, "agents.defaults.model", true, nil
	}

	return "", "agents.defaults.model.primary", false, nil
}

func readOptionalString(s *Service, path string) (value string, exists bool, err error) {
	out, code, runErr := s.run("openclaw", "config", "get", path)
	if runErr != nil {
		return "", false, fmt.Errorf("openclaw config get %s failed: %w", path, runErr)
	}
	trimmed := strings.TrimSpace(string(out))
	if code != 0 {
		if code == 1 && trimmed == "" {
			return "", false, nil
		}
		if trimmed != "" {
			return "", false, fmt.Errorf("%s", trimmed)
		}
		return "", false, fmt.Errorf("openclaw config get %s exited %d", path, code)
	}
	if trimmed == "" || trimmed == "null" || trimmed == "undefined" {
		return "", false, nil
	}
	trimmed = strings.Trim(trimmed, "\"")
	return trimmed, true, nil
}

func readOptionalJSON(s *Service, path string) (value map[string]any, exists bool, err error) {
	out, code, runErr := s.run("openclaw", "config", "get", path, "--json")
	if runErr != nil {
		return nil, false, fmt.Errorf("openclaw config get %s failed: %w", path, runErr)
	}
	trimmed := strings.TrimSpace(string(out))
	if code != 0 {
		if code == 1 && trimmed == "" {
			return nil, false, nil
		}
		if trimmed != "" {
			return nil, false, fmt.Errorf("%s", trimmed)
		}
		return nil, false, fmt.Errorf("openclaw config get %s exited %d", path, code)
	}
	if trimmed == "" || trimmed == "null" || trimmed == "undefined" {
		return nil, false, nil
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(trimmed), &data); err != nil {
		return nil, false, fmt.Errorf("invalid JSON for %s: %w", path, err)
	}
	if data == nil {
		return nil, false, nil
	}
	return data, true, nil
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func openClawNoChange(err error) error {
	return fmt.Errorf("OpenClaw is not automatically wireable: %v. Nothing changed.", err)
}
