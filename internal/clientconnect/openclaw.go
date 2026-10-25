package clientconnect

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
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

func planOpenClawCurrent(ctx context.Context, s *Service, target Target) (plannedMutation, error) {
	if strings.TrimSpace(s.getenv("OPENCLAW_NIX_MODE")) == "1" {
		return plannedMutation{}, openClawProblem(fmt.Errorf("Nix mode makes configuration immutable"))
	}
	locusRaw, err := requireCommandOutput(ctx, s, "OpenClaw", "openclaw", "config", "file")
	if err != nil {
		return plannedMutation{}, openClawProblem(err)
	}
	locus, err := resolveOpenClawConfigPath(string(locusRaw), s)
	if err != nil {
		return plannedMutation{}, openClawProblem(err)
	}

	modelRef, source, modelExists, err := readModelRef(ctx, s)
	if err != nil {
		return plannedMutation{}, openClawProblem(err)
	}

	observedProvider, _, err := readOptionalJSON(ctx, s, "models.providers.swobu")
	if err != nil {
		return plannedMutation{}, openClawProblem(err)
	}

	allowlist, allowlistExists, err := readOptionalJSON(ctx, s, "agents.defaults.models")
	if err != nil {
		return plannedMutation{}, openClawProblem(err)
	}

	api, apiExists := observedProvider["api"].(string)
	apiKey, apiKeyExists := observedProvider["apiKey"].(string)
	if value, exists := observedProvider["apiKey"]; exists && value != nil && !apiKeyExists {
		return plannedMutation{}, openClawProblem(fmt.Errorf("external credential references require manual configuration; Connect cannot validate this binding"))
	}
	configFile, err := inspectForeignFile(locus, nil)
	if err != nil {
		return plannedMutation{}, openClawProblem(err)
	}
	// OpenClaw deliberately redacts CLI reads. Read only its reported private
	// file for Access evidence; never use the display marker as a credential.
	if apiKey == "__OPENCLAW_REDACTED__" {
		apiKey, apiKeyExists, err = (jsonEditor{allowComments: true}).String(configFile.raw, keyPath{"models", "providers", "swobu", "apiKey"})
		if err != nil {
			return plannedMutation{}, openClawProblem(fmt.Errorf("cannot inspect credential binding in configuration"))
		}
	}
	credentialBinding := apiKey
	if strings.HasPrefix(apiKey, "${") && strings.HasSuffix(apiKey, "}") {
		apiKey = s.getenv(strings.TrimSuffix(strings.TrimPrefix(apiKey, "${"), "}"))
	}
	beforeEndpoint, endpointExists := observedProvider["baseUrl"].(string)
	defaultExists := false
	if models, ok := observedProvider["models"].([]any); ok {
		for _, item := range models {
			if object, ok := item.(map[string]any); ok && object["id"] == target.ModelID() {
				defaultExists = true
			}
		}
	}

	allowlistSatisfied := true
	if allowlistExists {
		_, allowlistSatisfied = allowlist["swobu/"+target.ModelID()]
	}

	changes := semanticChange("backend", modelRef, modelExists, "swobu/"+target.ModelID())
	changes = append(changes, semanticChange("endpoint", beforeEndpoint, endpointExists, target.WorkspaceURL())...)
	changes = append(changes, semanticChange("protocol", api, apiExists, "openai-completions")...)
	changes = append(changes, credentialChange(apiKey, apiKeyExists, target)...)
	changes = append(changes, semanticChange("selected model", fmt.Sprint(defaultExists), defaultExists, "true")...)
	if allowlistExists && !allowlistSatisfied {
		changes = append(changes, Change{Field: "allowlist membership", After: "swobu/" + target.ModelID()})
	}

	plan := Plan{
		ClientID:    ClientOpenClaw,
		ClientName:  "OpenClaw",
		ConfigPaths: []string{locus},
		Target:      target,
		Changes:     changes,
		credential:  apiKey,
	}
	if plan.AlreadyConfigured() {
		return plannedMutation{plan: plan}, nil
	}

	return plannedMutation{
		plan: plan,
		apply: func(ctx context.Context) error {
			freshProvider, freshProviderExists, freshErr := readOptionalJSON(ctx, s, "models.providers.swobu")
			if freshErr != nil {
				return openClawProblem(freshErr)
			}
			providerValue := map[string]any{
				"baseUrl": target.WorkspaceURL(),
				"api":     "openai-completions",
				"apiKey":  target.bearer,
				"models":  []any{map[string]any{"id": target.ModelID(), "name": target.ModelID()}},
			}
			if freshProviderExists && freshProvider != nil {
				providerValue = cloneMap(freshProvider)
				providerValue["baseUrl"], providerValue["api"] = target.WorkspaceURL(), "openai-completions"
				providerValue["apiKey"] = target.bearer
				models, ok := providerValue["models"].([]any)
				if providerValue["models"] != nil && !ok {
					return openClawProblem(fmt.Errorf("models.providers.swobu.models is not an array"))
				}
				found := false
				for _, item := range models {
					if object, ok := item.(map[string]any); ok && object["id"] == target.ModelID() {
						found = true
					}
				}
				if !found {
					models = append(models, map[string]any{"id": target.ModelID(), "name": target.ModelID()})
				}
				providerValue["models"] = models
			}
			if apiKey != "" && apiKey == target.bearer {
				providerValue["apiKey"] = credentialBinding
			}
			providerJSON, _ := json.Marshal(providerValue)

			sets := [][]string{
				{"config", "set", "models.providers.swobu", string(providerJSON), "--json"},
			}

			freshAllowlist, freshAllowlistExists, allowlistErr := readOptionalJSON(ctx, s, "agents.defaults.models")
			if allowlistErr != nil {
				return openClawProblem(allowlistErr)
			}
			if freshAllowlistExists {
				updatedAllowlist := cloneMap(freshAllowlist)
				if updatedAllowlist == nil {
					updatedAllowlist = make(map[string]any)
				}
				if _, present := updatedAllowlist["swobu/"+target.ModelID()]; !present {
					updatedAllowlist["swobu/"+target.ModelID()] = map[string]any{}
				}
				encoded, _ := json.Marshal(updatedAllowlist)
				sets = append(sets, []string{"config", "set", "agents.defaults.models", string(encoded), "--json"})
			}

			sets = append(sets, []string{"config", "set", source, "swobu/" + target.ModelID()})
			// OpenClaw owns writes through its configuration command, so the
			// foreign-file replacement helper cannot publish the recovery copy.
			// Preserve the exact current file before the first command may mutate
			// it; a fresh installation has no prior bytes to recover.
			configFile, err := inspectForeignFile(locus, nil)
			if err != nil {
				return openClawProblem(fmt.Errorf("inspect configuration before mutation: %w", err))
			}
			if configFile.existed {
				if err := backupForeignFile(configFile.physical, configFile.raw, configFile.mode); err != nil {
					return openClawProblem(fmt.Errorf("backup configuration before mutation: %w", err))
				}
			}
			for _, args := range sets {
				_, code, runErr := s.run(ctx, "openclaw", args...)
				if runErr != nil {
					return fmt.Errorf("OpenClaw could not set %s: %w", args[2], runErr)
				}
				if code != 0 {
					return fmt.Errorf("OpenClaw configuration command exited %d", code)
				}
			}
			return nil
		},
	}, nil
}

// resolveOpenClawConfigPath expands only the home markers OpenClaw's config
// file command uses for display. Treating those markers as literal paths makes
// the preview disagree with the file the same OpenClaw process reads and writes.
func resolveOpenClawConfigPath(raw string, service *Service) (string, error) {
	locus := strings.TrimSpace(raw)
	if locus == "" {
		return "", fmt.Errorf("config file returned no path")
	}
	var home string
	switch {
	case locus == "$OPENCLAW_HOME", strings.HasPrefix(locus, "$OPENCLAW_HOME/"), strings.HasPrefix(locus, "$OPENCLAW_HOME\\"):
		home = strings.TrimSpace(service.getenv("OPENCLAW_HOME"))
		if home == "" {
			return "", fmt.Errorf("config file returned an OPENCLAW_HOME path but OPENCLAW_HOME is unset")
		}
		locus = strings.TrimLeft(strings.TrimPrefix(locus, "$OPENCLAW_HOME"), `/\\`)
	case locus == "~", strings.HasPrefix(locus, "~/"), strings.HasPrefix(locus, "~\\"):
		home = strings.TrimSpace(service.getenv("OPENCLAW_HOME"))
		if home == "" {
			var err error
			home, err = service.homeDir()
			if err != nil {
				return "", fmt.Errorf("resolve home for OpenClaw config path: %w", err)
			}
		}
		locus = strings.TrimLeft(strings.TrimPrefix(locus, "~"), `/\\`)
	default:
		return filepath.Abs(filepath.Clean(locus))
	}
	return filepath.Abs(filepath.Join(home, locus))
}

func readModelRef(ctx context.Context, s *Service) (model string, source string, exists bool, err error) {
	primary, primaryExists, primaryErr := readOptionalString(ctx, s, "agents.defaults.model.primary")
	if primaryErr != nil {
		return "", "", false, primaryErr
	}
	if primaryExists {
		return primary, "agents.defaults.model.primary", true, nil
	}

	legacy, legacyExists, legacyErr := readOptionalString(ctx, s, "agents.defaults.model")
	if legacyErr != nil {
		return "", "", false, legacyErr
	}
	if legacyExists {
		return legacy, "agents.defaults.model", true, nil
	}

	return "", "agents.defaults.model.primary", false, nil
}

func readOptionalString(ctx context.Context, s *Service, path string) (value string, exists bool, err error) {
	out, code, runErr := s.run(ctx, "openclaw", "config", "get", path)
	if runErr != nil {
		return "", false, fmt.Errorf("openclaw config get %s failed: %w", path, runErr)
	}
	trimmed := strings.TrimSpace(string(out))
	if code != 0 {
		if openClawPathMissing(code, trimmed, path) {
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

func readOptionalJSON(ctx context.Context, s *Service, path string) (value map[string]any, exists bool, err error) {
	out, code, runErr := s.run(ctx, "openclaw", "config", "get", path, "--json")
	if runErr != nil {
		return nil, false, fmt.Errorf("openclaw config get %s failed: %w", path, runErr)
	}
	trimmed := strings.TrimSpace(string(out))
	if code != 0 {
		if openClawPathMissing(code, trimmed, path) {
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

func openClawPathMissing(code int, output string, path string) bool {
	if code != 1 {
		return false
	}
	if output == "" {
		return true
	}
	return strings.HasPrefix(output, "Config path not found: "+path+".")
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

func openClawProblem(err error) error {
	return fmt.Errorf("OpenClaw is not automatically wireable: %w", err)
}
