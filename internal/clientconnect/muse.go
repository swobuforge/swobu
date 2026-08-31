package clientconnect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

const ClientMuse ClientID = "muse"

var museAdapter = adapter{
	id:          ClientMuse,
	name:        "Muse Code",
	present:     musePresent,
	planCurrent: planMuseCurrent,
}

type museModelCatalogEntry struct {
	ModelID      string `json:"model_id"`
	ProviderID   string `json:"provider_id"`
	ProfileID    string `json:"profile_id"`
	DisplayLabel string `json:"display_label"`
	Visibility   string `json:"visibility"`
	DisplayOrder int    `json:"display_order"`
	IsDefault    bool   `json:"is_default"`
	ContextLimit int    `json:"context_limit"`
	OutputLimit  int    `json:"output_limit"`
	Description  string `json:"description"`
}

var museSwobuModelCatalog = []museModelCatalogEntry{{
	ModelID:      "default",
	ProviderID:   "meta",
	ProfileID:    "tbh",
	DisplayLabel: "Swobu",
	Visibility:   "visible",
	DisplayOrder: 0,
	IsDefault:    true,
	ContextLimit: 1048576,
	OutputLimit:  131072,
	Description:  "Muse Spark via Swobu",
}}

func (s *Service) musePath() (string, error) {
	dir := s.getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := s.homeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "muse", "settings.json"), nil
}

func musePresent(s *Service) (bool, error) {
	if _, err := s.lookPath("muse"); err == nil {
		return true, nil
	}
	home, err := s.homeDir()
	if err != nil {
		return false, err
	}
	settings, err := s.musePath()
	if err != nil {
		return false, err
	}
	for _, path := range []string{filepath.Join(home, ".local", "bin", "muse"), settings} {
		info, statErr := os.Stat(path)
		if statErr == nil {
			return info.Mode().IsRegular(), nil
		}
		if !os.IsNotExist(statErr) {
			return false, statErr
		}
	}
	return false, nil
}

func planMuseCurrent(s *Service, target Target) (plannedMutation, error) {
	path, err := s.musePath()
	if err != nil {
		return plannedMutation{}, err
	}
	return planMuse(path, target)
}

func planMuse(path string, target Target) (plannedMutation, error) {
	file, err := inspectForeignFile(path, []byte("{}\n"))
	if err != nil {
		return plannedMutation{}, museNoChange(err)
	}
	editor := jsonEditor{}
	provider, providerExists, err := editor.String(file.raw, keyPath{"provider"})
	if err != nil {
		return plannedMutation{}, museNoChange(err)
	}
	model, modelExists, err := editor.String(file.raw, keyPath{"model"})
	if err != nil {
		return plannedMutation{}, museNoChange(err)
	}
	endpoint, endpointExists, err := editor.String(file.raw, keyPath{"endpoint_transport", "base_url"})
	if err != nil {
		return plannedMutation{}, museNoChange(err)
	}
	auth, authExists, err := editor.String(file.raw, keyPath{"endpoint_transport", "auth"})
	if err != nil {
		return plannedMutation{}, museNoChange(err)
	}
	var catalog []museModelCatalogEntry
	catalogExists, err := editor.Value(file.raw, keyPath{"model_catalog"}, &catalog)
	if err != nil {
		return plannedMutation{}, museNoChange(err)
	}
	var catalogValue any
	if catalogExists {
		if _, err := editor.Value(file.raw, keyPath{"model_catalog"}, &catalogValue); err != nil {
			return plannedMutation{}, museNoChange(err)
		}
	}
	var schemaVersion any
	schemaExists, err := editor.Value(file.raw, keyPath{"schema_version"}, &schemaVersion)
	if err != nil {
		return plannedMutation{}, museNoChange(err)
	}

	wantedEndpoint := target.WorkspaceURL() + "/v1"
	wantedCatalogValue := museCatalogValue(museSwobuModelCatalog)
	beforeCatalog := encodeMuseCatalogValue(catalogValue)
	afterCatalog := encodeMuseCatalogValue(wantedCatalogValue)
	changes := semanticChange("backend", provider+"/"+model, providerExists || modelExists, "meta/default")
	changes = append(changes, semanticChange("endpoint", endpoint, endpointExists, wantedEndpoint)...)
	changes = append(changes, semanticChange("authentication", auth, authExists, "none")...)
	if !catalogExists || !reflect.DeepEqual(catalogValue, wantedCatalogValue) {
		changes = append(changes, Change{Field: "model catalog", Before: beforeCatalog, After: afterCatalog, BeforeExists: catalogExists})
	}
	if !schemaExists {
		changes = append(changes, Change{Field: "schema version", After: "1"})
	}
	plan := Plan{ConfigPath: file.logical, Target: target, Changes: changes}
	if plan.AlreadyConfigured() {
		return plannedMutation{plan: plan}, nil
	}

	next := file.raw
	for _, edit := range []struct {
		path  keyPath
		value string
	}{
		{keyPath{"provider"}, "meta"},
		{keyPath{"model"}, "default"},
		{keyPath{"endpoint_transport", "base_url"}, wantedEndpoint},
		{keyPath{"endpoint_transport", "auth"}, "none"},
	} {
		next, err = editor.SetString(next, edit.path, edit.value)
		if err != nil {
			return plannedMutation{}, museNoChange(err)
		}
	}
	catalogJSON, _ := json.Marshal(museSwobuModelCatalog)
	next, err = editor.SetValue(next, keyPath{"model_catalog"}, catalogJSON)
	if err != nil {
		return plannedMutation{}, museNoChange(err)
	}
	if !schemaExists {
		next, err = editor.SetValue(next, keyPath{"schema_version"}, []byte("1"))
		if err != nil {
			return plannedMutation{}, museNoChange(err)
		}
	}
	return plannedMutation{plan: plan, apply: func() error { return file.replace(next) }}, nil
}

func museCatalogValue(catalog []museModelCatalogEntry) any {
	raw, _ := json.Marshal(catalog)
	var value any
	_ = json.Unmarshal(raw, &value)
	return value
}

func encodeMuseCatalogValue(catalog any) string {
	if catalog == nil {
		return ""
	}
	raw, _ := json.Marshal(catalog)
	return string(raw)
}

func museNoChange(err error) error {
	return fmt.Errorf("Muse Code configuration %v. Nothing changed.", err)
}
