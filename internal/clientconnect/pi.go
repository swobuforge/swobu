package clientconnect

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

const ClientPi ClientID = "pi"

var piAdapter = adapter{
	id:          ClientPi,
	name:        "pi",
	present:     piPresent,
	planCurrent: planPiCurrent,
}

func (s *Service) piPaths() (settings, models string, err error) {
	dir := strings.TrimSpace(s.getenv("PI_CODING_AGENT_DIR"))
	if dir == "" {
		home, err := s.homeDir()
		if err != nil {
			return "", "", err
		}
		dir = filepath.Join(home, ".pi", "agent")
	}
	return filepath.Join(dir, "settings.json"), filepath.Join(dir, "models.json"), nil
}

func piPresent(s *Service) (bool, error) {
	settings, models, err := s.piPaths()
	if err != nil {
		return false, err
	}
	return binaryOrRegularFilePresent(s, "pi", settings, models)
}

func planPiCurrent(_ context.Context, s *Service, target Target) (plannedMutation, error) {
	settings, models, err := s.piPaths()
	if err != nil {
		return plannedMutation{}, piProblem(err)
	}
	return planPi(settings, models, target)
}

func planPi(settingsPath, modelsPath string, target Target) (plannedMutation, error) {
	settings, err := inspectForeignFile(settingsPath, []byte("{}\n"))
	if err != nil {
		return plannedMutation{}, piProblem(err)
	}
	settingsEditor := jsonEditor{}
	provider, providerExists, err := settingsEditor.String(settings.raw, keyPath{"defaultProvider"})
	if err != nil {
		return plannedMutation{}, piProblem(err)
	}
	model, modelExists, err := settingsEditor.String(settings.raw, keyPath{"defaultModel"})
	if err != nil {
		return plannedMutation{}, piProblem(err)
	}
	models, err := inspectForeignFile(modelsPath, []byte("{}\n"))
	if err != nil {
		return plannedMutation{}, piProblem(err)
	}
	modelsEditor := jsonEditor{allowComments: true}
	endpoint, endpointExists, err := modelsEditor.String(models.raw, keyPath{"providers", "swobu", "baseUrl"})
	if err != nil {
		return plannedMutation{}, piProblem(err)
	}
	api, apiExists, err := modelsEditor.String(models.raw, keyPath{"providers", "swobu", "api"})
	if err != nil {
		return plannedMutation{}, piProblem(err)
	}
	apiKey, apiKeyExists, err := modelsEditor.String(models.raw, keyPath{"providers", "swobu", "apiKey"})
	if err != nil {
		return plannedMutation{}, piProblem(err)
	}
	var existingModels json.RawMessage
	modelsExist, err := modelsEditor.Value(models.raw, keyPath{"providers", "swobu", "models"}, &existingModels)
	if err != nil {
		return plannedMutation{}, piProblem(err)
	}
	canonicalModels, _ := json.Marshal([]map[string]string{{"id": "default", "name": "Swobu default"}})
	changes := semanticChange("backend", provider+"/"+model, providerExists || modelExists, "swobu/default")
	changes = append(changes, semanticChange("endpoint", endpoint, endpointExists, target.WorkspaceURL())...)
	changes = append(changes, semanticChange("protocol", api, apiExists, "openai-responses")...)
	if !apiKeyExists || apiKey == "" {
		changes = append(changes, semanticChange("API key placeholder", apiKey, apiKeyExists, "swobu")...)
	}
	changes = append(changes, semanticChange("model catalog", semanticJSON(existingModels), modelsExist, string(canonicalModels))...)
	plan := Plan{ConfigPaths: []string{models.logical, settings.logical}, Target: target, Changes: changes}
	if plan.AlreadyConfigured() {
		return plannedMutation{plan: plan}, nil
	}
	nextModels, err := setJSONValue(modelsEditor, models.raw, keyPath{"providers", "swobu", "baseUrl"}, target.WorkspaceURL())
	if err != nil {
		return plannedMutation{}, piProblem(err)
	}
	nextModels, err = setJSONValue(modelsEditor, nextModels, keyPath{"providers", "swobu", "api"}, "openai-responses")
	if err != nil {
		return plannedMutation{}, piProblem(err)
	}
	if !apiKeyExists || apiKey == "" {
		nextModels, err = setJSONValue(modelsEditor, nextModels, keyPath{"providers", "swobu", "apiKey"}, "swobu")
		if err != nil {
			return plannedMutation{}, piProblem(err)
		}
	}
	nextModels, err = modelsEditor.SetValue(nextModels, keyPath{"providers", "swobu", "models"}, canonicalModels)
	if err != nil {
		return plannedMutation{}, piProblem(err)
	}
	nextSettings, err := setJSONStrings(settingsEditor, settings.raw,
		jsonStringChange{keyPath{"defaultProvider"}, "swobu"}, jsonStringChange{keyPath{"defaultModel"}, "default"})
	if err != nil {
		return plannedMutation{}, piProblem(err)
	}
	apply := func(context.Context) error {
		if err := models.replace(nextModels); err != nil {
			return err
		}
		return settings.replace(nextSettings)
	}
	return plannedMutation{plan: plan, apply: apply}, nil
}

func semanticJSON(raw json.RawMessage) string {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return string(raw)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return string(raw)
	}
	return string(canonical)
}

func piProblem(err error) error { return fmt.Errorf("pi: %w", err) }
