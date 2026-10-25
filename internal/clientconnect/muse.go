package clientconnect

import (
	"context"
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

func museSwobuModelCatalog(modelID string) []museModelCatalogEntry {
	return []museModelCatalogEntry{{
		ModelID:      modelID,
		ProviderID:   "meta",
		ProfileID:    "tbh",
		DisplayLabel: "Swobu",
		Visibility:   "visible",
		DisplayOrder: 0,
		IsDefault:    true,
		ContextLimit: 1000000,
		OutputLimit:  64000,
		Description:  "Swobu selected model",
	}}
}

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

func planMuseCurrent(_ context.Context, s *Service, target Target) (plannedMutation, error) {
	path, err := s.musePath()
	if err != nil {
		return plannedMutation{}, err
	}
	return planMuse(path, target)
}

func planMuse(path string, target Target) (plannedMutation, error) {
	file, err := inspectForeignFile(path, []byte("{}\n"))
	if err != nil {
		return plannedMutation{}, museProblem(err)
	}
	editor := jsonEditor{}
	provider, providerExists, err := editor.String(file.raw, keyPath{"provider"})
	if err != nil {
		return plannedMutation{}, museProblem(err)
	}
	model, modelExists, err := editor.String(file.raw, keyPath{"model"})
	if err != nil {
		return plannedMutation{}, museProblem(err)
	}
	endpoint, endpointExists, err := editor.String(file.raw, keyPath{"endpoint_transport", "base_url"})
	if err != nil {
		return plannedMutation{}, museProblem(err)
	}
	auth, authExists, err := editor.String(file.raw, keyPath{"endpoint_transport", "auth"})
	if err != nil {
		return plannedMutation{}, museProblem(err)
	}
	credentials, err := inspectForeignFile(filepath.Join(filepath.Dir(path), "auth.json"), []byte("{\"schema_version\":1}\n"))
	if err != nil {
		return plannedMutation{}, museProblem(err)
	}
	credential, credentialExists, err := editor.String(credentials.raw, keyPath{"providers", "meta", "api_key"})
	if err != nil {
		return plannedMutation{}, museProblem(err)
	}
	var catalog []museModelCatalogEntry
	catalogExists, err := editor.Value(file.raw, keyPath{"model_catalog"}, &catalog)
	if err != nil {
		return plannedMutation{}, museProblem(err)
	}
	var catalogValue any
	if catalogExists {
		if _, err := editor.Value(file.raw, keyPath{"model_catalog"}, &catalogValue); err != nil {
			return plannedMutation{}, museProblem(err)
		}
	}
	var schemaVersion any
	schemaExists, err := editor.Value(file.raw, keyPath{"schema_version"}, &schemaVersion)
	if err != nil {
		return plannedMutation{}, museProblem(err)
	}

	wantedEndpoint := target.WorkspaceURL() + "/v1"
	wantedCatalogValue := museCatalogValue(museSwobuModelCatalog(target.ModelID()))
	beforeCatalog := encodeMuseCatalogValue(catalogValue)
	afterCatalog := encodeMuseCatalogValue(wantedCatalogValue)
	changes := semanticChange("backend", provider+"/"+model, providerExists || modelExists, "meta/"+target.ModelID())
	changes = append(changes, semanticChange("endpoint", endpoint, endpointExists, wantedEndpoint)...)
	changes = append(changes, semanticChange("authentication transport", auth, authExists, "bearer")...)
	credentialChanges := credentialChange(credential, credentialExists, target)
	changes = append(changes, credentialChanges...)
	if !catalogExists || !reflect.DeepEqual(catalogValue, wantedCatalogValue) {
		changes = append(changes, Change{Field: "model catalog", Before: beforeCatalog, After: afterCatalog, BeforeExists: catalogExists})
	}
	if !schemaExists {
		changes = append(changes, Change{Field: "schema version", After: "1"})
	}
	plan := Plan{ConfigPaths: []string{file.logical, credentials.logical}, Target: target, Changes: changes, credential: credential}
	if plan.AlreadyConfigured() {
		return plannedMutation{plan: plan}, nil
	}

	next := file.raw
	for _, edit := range []struct {
		path  keyPath
		value string
	}{
		{keyPath{"provider"}, "meta"},
		{keyPath{"model"}, target.ModelID()},
		{keyPath{"endpoint_transport", "base_url"}, wantedEndpoint},
		{keyPath{"endpoint_transport", "auth"}, "bearer"},
	} {
		next, err = editor.SetString(next, edit.path, edit.value)
		if err != nil {
			return plannedMutation{}, museProblem(err)
		}
	}
	catalogJSON, _ := json.Marshal(museSwobuModelCatalog(target.ModelID()))
	next, err = editor.SetValue(next, keyPath{"model_catalog"}, catalogJSON)
	if err != nil {
		return plannedMutation{}, museProblem(err)
	}
	if !schemaExists {
		next, err = editor.SetValue(next, keyPath{"schema_version"}, []byte("1"))
		if err != nil {
			return plannedMutation{}, museProblem(err)
		}
	}
	nextCredentials, err := editor.SetString(credentials.raw, keyPath{"providers", "meta", "api_key"}, target.bearer)
	if err != nil {
		return plannedMutation{}, museProblem(err)
	}
	// Muse stores authentication separately. Withhold the bearer before changing
	// either binding, so every interrupted prefix fails closed and cannot send a
	// Swobu credential to the previous endpoint. Final publication enables auth.
	staged, err := editor.SetString(next, keyPath{"endpoint_transport", "auth"}, "none")
	if err != nil {
		return plannedMutation{}, museProblem(err)
	}
	return plannedMutation{plan: plan, apply: func(context.Context) error {
		if len(credentialChanges) == 0 {
			return file.replace(next)
		}
		if err := file.replace(staged); err != nil {
			return err
		}
		if err := credentials.replace(nextCredentials); err != nil {
			return err
		}
		current, err := inspectForeignFile(path, nil)
		if err != nil {
			return err
		}
		if current.physical != file.physical || string(current.raw) != string(staged) {
			return ErrPlanChanged
		}
		// Retain the original preimage as the recovery copy, not the staged
		// fail-closed configuration.
		return file.replace(next)
	}}, nil
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

func museProblem(err error) error {
	return fmt.Errorf("Muse Code configuration: %w", err)
}
