package clientconnect

import (
	"fmt"
	"path/filepath"
)

const ClientClaude ClientID = "claude"

var (
	claudeEndpointPath  = keyPath{"env", "ANTHROPIC_BASE_URL"}
	claudeDiscoveryPath = keyPath{"env", "CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY"}
)

var claudeAdapter = adapter{
	id:          ClientClaude,
	name:        "Claude Code",
	present:     claudePresent,
	planCurrent: planClaudeCurrent,
}

func (s *Service) claudePath() (string, error) {
	dir := s.getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := s.homeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".claude")
	}
	return filepath.Join(dir, "settings.json"), nil
}

func claudePresent(s *Service) (bool, error) {
	path, err := s.claudePath()
	if err != nil {
		return false, err
	}
	return binaryOrRegularFilePresent(s, "claude", path)
}

func planClaudeCurrent(s *Service, target Target) (plannedMutation, error) {
	path, err := s.claudePath()
	if err != nil {
		return plannedMutation{}, err
	}
	return planClaude(path, target)
}

func planClaude(path string, target Target) (plannedMutation, error) {
	file, err := inspectForeignFile(path, []byte("{}\n"))
	if err != nil {
		return plannedMutation{}, claudeNoChange(err)
	}
	editor := jsonEditor{}
	endpoint, endpointExists, err := editor.String(file.raw, claudeEndpointPath)
	if err != nil {
		return plannedMutation{}, claudeNoChange(err)
	}
	discovery, discoveryExists, err := editor.String(file.raw, claudeDiscoveryPath)
	if err != nil {
		return plannedMutation{}, claudeNoChange(err)
	}
	changes := semanticChange("endpoint", endpoint, endpointExists, target.WorkspaceURL())
	changes = append(changes, semanticChange("model discovery", discovery, discoveryExists, "1")...)
	plan := Plan{ConfigPath: file.logical, Target: target, Changes: changes}
	if plan.AlreadyConfigured() {
		return plannedMutation{plan: plan}, nil
	}
	// Both client-owned leaves commit together so replacement approval and
	// freshness protect an explicitly disabled discovery setting.
	next, err := setJSONStrings(editor, file.raw,
		jsonStringChange{path: claudeEndpointPath, value: target.WorkspaceURL()},
		jsonStringChange{path: claudeDiscoveryPath, value: "1"},
	)
	if err != nil {
		return plannedMutation{}, claudeNoChange(err)
	}
	return plannedMutation{plan: plan, apply: func() error { return file.replace(next) }}, nil
}

func claudeNoChange(err error) error { return fmt.Errorf("Claude Code %v. Nothing changed.", err) }
