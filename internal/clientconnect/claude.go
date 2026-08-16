package clientconnect

import (
	"fmt"
	"path/filepath"
)

const ClientClaude ClientID = "claude"

var claudeEndpointPath = keyPath{"env", "ANTHROPIC_BASE_URL"}

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
	plan, err := planEndpointField(file, jsonEditor{}, claudeEndpointPath, target)
	if err != nil {
		return plannedMutation{}, claudeNoChange(err)
	}
	return plan, nil
}

func claudeNoChange(err error) error { return fmt.Errorf("Claude Code %v. Nothing changed.", err) }
