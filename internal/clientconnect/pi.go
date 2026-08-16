package clientconnect

import (
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

func planPiCurrent(s *Service, target Target) (plannedMutation, error) {
	settings, models, err := s.piPaths()
	if err != nil {
		return plannedMutation{}, piNoChange(err)
	}
	return planPi(settings, models, target)
}

func planPi(settingsPath, modelsPath string, target Target) (plannedMutation, error) {
	settings, err := inspectForeignFile(settingsPath, nil)
	if err != nil || !settings.existed {
		return plannedMutation{}, piNoChange(fmt.Errorf("global settings.json with defaultProvider is required"))
	}
	editor := jsonEditor{}
	provider, providerExists, err := editor.String(settings.raw, keyPath{"defaultProvider"})
	if err != nil {
		return plannedMutation{}, piNoChange(err)
	}
	model, modelExists, err := editor.String(settings.raw, keyPath{"defaultModel"})
	if err != nil {
		return plannedMutation{}, piNoChange(err)
	}
	models, err := inspectForeignFile(modelsPath, []byte("{}\n"))
	if err != nil {
		return plannedMutation{}, piNoChange(err)
	}
	endpoint, endpointExists, err := editor.String(models.raw, keyPath{"providers", "swobu", "baseUrl"})
	if err != nil {
		return plannedMutation{}, piNoChange(err)
	}
	api, apiExists, err := editor.String(models.raw, keyPath{"providers", "swobu", "api"})
	if err != nil {
		return plannedMutation{}, piNoChange(err)
	}
	apiKey, apiKeyExists, err := editor.String(models.raw, keyPath{"providers", "swobu", "apiKey"})
	if err != nil {
		return plannedMutation{}, piNoChange(err)
	}
	var existingModels []map[string]any
	modelsExist, err := editor.Value(models.raw, keyPath{"providers", "swobu", "models"}, &existingModels)
	if err != nil {
		return plannedMutation{}, piNoChange(err)
	}
	defaultExists := false
	for _, item := range existingModels {
		if item["id"] == "default" {
			defaultExists = true
		}
	}
	changes := semanticChange("backend", provider+"/"+model, providerExists || modelExists, "swobu/default")
	changes = append(changes, semanticChange("endpoint", endpoint, endpointExists, target.WorkspaceURL())...)
	changes = append(changes, semanticChange("protocol", api, apiExists, "openai-completions")...)
	if !apiKeyExists || apiKey == "" {
		changes = append(changes, semanticChange("API key placeholder", "", false, "swobu")...)
	}
	changes = append(changes, semanticChange("default model", fmt.Sprint(defaultExists), modelsExist && defaultExists, "true")...)
	plan := Plan{ConfigPath: models.logical + ", " + settings.logical, Target: target, Changes: changes}
	if plan.AlreadyConfigured() {
		return plannedMutation{plan: plan}, nil
	}
	var swobu map[string]any
	exists, err := editor.Value(models.raw, keyPath{"providers", "swobu"}, &swobu)
	if err != nil {
		return plannedMutation{}, piNoChange(err)
	}
	if !exists {
		swobu = map[string]any{}
	}
	swobu["baseUrl"], swobu["api"] = target.WorkspaceURL(), "openai-completions"
	if !apiKeyExists || apiKey == "" {
		swobu["apiKey"] = "swobu"
	}
	modelList, ok := swobu["models"].([]any)
	if swobu["models"] != nil && !ok {
		return plannedMutation{}, piNoChange(fmt.Errorf("providers.swobu.models is not an array"))
	}
	foundDefault := false
	for _, item := range modelList {
		if object, ok := item.(map[string]any); ok && object["id"] == "default" {
			foundDefault = true
		}
	}
	if !foundDefault {
		modelList = append(modelList, map[string]any{"id": "default", "name": "Swobu default"})
	}
	swobu["models"] = modelList
	nextModels, err := setJSONValue(editor, models.raw, keyPath{"providers", "swobu"}, swobu)
	if err != nil {
		return plannedMutation{}, piNoChange(err)
	}
	nextSettings, err := setJSONStrings(editor, settings.raw,
		jsonStringChange{keyPath{"defaultProvider"}, "swobu"}, jsonStringChange{keyPath{"defaultModel"}, "default"})
	if err != nil {
		return plannedMutation{}, piNoChange(err)
	}
	return plannedMutation{plan: plan, apply: func() error {
		if err := models.replace(nextModels); err != nil {
			return err
		}
		return settings.replace(nextSettings)
	}}, nil
}

func piNoChange(err error) error { return fmt.Errorf("pi %v. Nothing changed.", err) }
