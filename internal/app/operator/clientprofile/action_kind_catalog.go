package clientprofile

import (
	"io/fs"
	"strings"
)

type ActionKind string

const (
	ActionKindRun ActionKind = "run"
)

type capabilityClientSpec struct {
	Identity Identity
	Vars     func(baseURL string) TemplateVars
	Actions  []capabilityActionSpec
	// Run is executable truth for interactive run behavior and run payload
	// rendering. Never encode non-interactive one-shot probe semantics here.
	Run *capabilityRunSpec
}

type capabilityActionSpec struct {
	ID      string
	Kind    ActionKind
	Summary string
	Content string
}

type capabilityRunSpec struct {
	Binary  string
	Args    []string
	Env     map[string]string
	Prepare *capabilityRunPrepareSpec
}

type capabilityRunPrepareSpec struct {
	Path           string
	Content        string
	Mode           fs.FileMode
	WriteIfMissing bool
}

type actionKindInfo struct {
	Label   string
	Summary string
	Verb    string
}

var actionDescriptors = map[ActionKind]actionKindInfo{
	ActionKindRun: {Label: "run", Summary: "command", Verb: "run"},
}

func capabilityCatalog() []capabilityClientSpec {
	return []capabilityClientSpec{
		codexClientSpec(),
		claudeClientSpec(),
		aiderClientSpec(),
		continueClientSpec(),
		opencodeClientSpec(),
		piClientSpec(),
	}
}

func codexClientSpec() capabilityClientSpec {
	return capabilityClientSpec{
		Identity: Identity{ID: "codex", Label: "Codex"},
		Vars: func(baseURL string) TemplateVars {
			vars := defaultTemplateVars(baseURL)
			vars["primary_model"] = "gpt-5.5"
			return vars
		},
		Actions: []capabilityActionSpec{
			{
				ID:   "run",
				Kind: ActionKindRun,
			},
		},
		Run: &capabilityRunSpec{
			Binary: "codex",
			Args: []string{
				"--dangerously-bypass-approvals-and-sandbox",
				// `apps` is not a recognized Codex CLI feature flag in the current
				// binary, and the interactive launcher path does not depend on it.
				"-c", "model=\"{{primary_model}}\"",
				"-c", "model_provider=\"swobu\"",
				"-c", "model_providers.swobu.name=\"Swobu\"",
				"-c", "model_providers.swobu.base_url=\"{{openai_base_url}}\"",
			},
			Env: map[string]string{
				"OPENAI_API_KEY": "swobu-placeholder",
			},
		},
	}
}

func claudeClientSpec() capabilityClientSpec {
	return capabilityClientSpec{
		Identity: Identity{ID: "claude", Label: "Claude"},
		Actions: []capabilityActionSpec{
			{
				ID:   "run",
				Kind: ActionKindRun,
			},
		},
		Run: &capabilityRunSpec{
			Binary: "claude",
			Args: []string{
				"--bare",
				"--add-dir", ".",
				"--tools", "Read",
				"--allowedTools", "Read",
				"--model", "{{primary_model}}",
			},
			Env: map[string]string{
				// Claude Code still expects a non-empty Anthropic API key even when
				// the base URL points at the Swobu workspace endpoint.
				"ANTHROPIC_API_KEY":             "swobu-placeholder",
				"ANTHROPIC_BASE_URL":            "{{base_url}}",
				"ANTHROPIC_MODEL":               "{{primary_model}}",
				"ANTHROPIC_CUSTOM_MODEL_OPTION": "{{primary_model}}",
			},
		},
	}
}

func aiderClientSpec() capabilityClientSpec {
	return capabilityClientSpec{
		Identity: Identity{ID: "aider", Label: "Aider"},
		Actions: []capabilityActionSpec{
			{
				ID:   "run",
				Kind: ActionKindRun,
			},
		},
		Run: &capabilityRunSpec{
			Binary: "aider",
			Args: []string{
				"--no-show-model-warnings",
				"--no-browser",
				"--model", "openai/{{primary_model}}",
			},
			Env: map[string]string{
				"AIDER_OPENAI_API_BASE": "{{openai_base_url}}",
				"OPENAI_API_KEY":        "swobu-placeholder",
			},
		},
	}
}

func continueClientSpec() capabilityClientSpec {
	return capabilityClientSpec{
		Identity: Identity{ID: "continue", Label: "Continue"},
		Actions: []capabilityActionSpec{
			{
				ID:   "run",
				Kind: ActionKindRun,
			},
		},
		Run: &capabilityRunSpec{
			Binary: "cn",
			Args: []string{
				"--config", "./swobu.continue.yaml",
			},
			Prepare: &capabilityRunPrepareSpec{
				Path: "./swobu.continue.yaml",
				Content: strings.Join([]string{
					"name: Swobu",
					"version: 1.0.0",
					"schema: v1",
					"",
					"models:",
					"  - name: Swobu Primary",
					"    provider: openai",
					"    model: primary",
					"    apiBase: {{openai_base_url}}",
					"    roles:",
					"      - chat",
					"      - edit",
					"      - apply",
				}, "\n"),
				Mode:           0o600,
				WriteIfMissing: true,
			},
		},
	}
}

func opencodeClientSpec() capabilityClientSpec {
	return capabilityClientSpec{
		Identity: Identity{ID: "opencode", Label: "OpenCode"},
		Vars: func(baseURL string) TemplateVars {
			// OpenCode only sends requests once the custom provider has an explicit
			// apiKey. Keep the lane deterministic by baking the env-backed key into
			// the checked-in config instead of relying on implicit provider auth.
			pretty := strings.Join([]string{
				"{",
				`  "$schema": "https://opencode.ai/config.json",`,
				`  "model": "swobu/primary",`,
				`  "provider": {`,
				`    "swobu": {`,
				`      "npm": "@ai-sdk/openai-compatible",`,
				`      "name": "Swobu",`,
				`      "options": {`,
				`        "baseURL": "{{openai_base_url}}",`,
				`        "apiKey": "{env:OPENAI_API_KEY}"`,
				`      },`,
				`      "models": {`,
				`        "primary": { "name": "Primary" }`,
				`      }`,
				`    }`,
				`  }`,
				"}",
			}, "\n")
			inline := `{"$schema":"https://opencode.ai/config.json","model":"swobu/primary","provider":{"swobu":{"npm":"@ai-sdk/openai-compatible","name":"Swobu","options":{"baseURL":"{{openai_base_url}}","apiKey":"{env:OPENAI_API_KEY}"},"models":{"primary":{"name":"Primary"}}}}}`
			return TemplateVars{
				"opencode_config_pretty": pretty,
				"opencode_config_inline": inline,
			}
		},
		Actions: []capabilityActionSpec{
			{
				ID:   "run",
				Kind: ActionKindRun,
			},
		},
		Run: &capabilityRunSpec{
			Binary: "opencode",
			Env: map[string]string{
				"OPENAI_API_KEY":          "swobu-placeholder",
				"OPENCODE_CONFIG":         "{{cwd}}/opencode.json",
				"OPENCODE_CONFIG_CONTENT": "{{opencode_config_inline}}",
			},
			Prepare: &capabilityRunPrepareSpec{
				Path:           "./opencode.json",
				Content:        "{{opencode_config_pretty}}",
				Mode:           0o600,
				WriteIfMissing: true,
			},
		},
	}
}
