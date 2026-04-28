package clientprofile

import (
	"io/fs"
	"strings"

	"github.com/metrofun/swobu/internal/domain/compatibility"
)

type ActionKind string

const (
	ActionKindRun               ActionKind = "run"
	ActionKindFileConfig        ActionKind = "file_config"
	ActionKindEnvironmentValues ActionKind = "environment_values"
	ActionKindOpenGuide         ActionKind = "open_guide"
	ActionKindCopyValues        ActionKind = "copy_values"
)

type capabilityClientSpec struct {
	Identity Identity
	Vars     func(baseURL string) TemplateVars
	Actions  []capabilityActionSpec
	// Run is executable truth for run-once behavior and run payload rendering.
	Run *capabilityRunSpec
}

type capabilityActionSpec struct {
	ID        string
	Kind      ActionKind
	Summary   string
	FocusVerb string
	Content   string
}

type capabilityRunSpec struct {
	Binary  string
	Args    []string
	Env     map[string]string
	Prepare *capabilityRunPrepareSpec
}

type capabilityRunPrepareSpec struct {
	Path           string
	FromActionID   string
	Mode           fs.FileMode
	WriteIfMissing bool
}

type actionKindInfo struct {
	Label   string
	Summary string
	Verb    string
}

var actionDescriptors = map[ActionKind]actionKindInfo{
	ActionKindRun:               {Label: "run", Summary: "command", Verb: "run"},
	ActionKindFileConfig:        {Label: "file config", Summary: "config", Verb: "copy"},
	ActionKindEnvironmentValues: {Label: "environment values", Summary: ".env", Verb: "copy"},
	ActionKindOpenGuide:         {Label: "open", Summary: "openai + anthropic compatible", Verb: "view"},
	ActionKindCopyValues:        {Label: "copy values", Summary: "base + model", Verb: "copy"},
}

func capabilityCatalog() []capabilityClientSpec {
	return []capabilityClientSpec{
		codexClientSpec(),
		claudeClientSpec(),
		aiderClientSpec(),
		continueClientSpec(),
		opencodeClientSpec(),
		otherClientSpec(),
	}
}

func codexClientSpec() capabilityClientSpec {
	return capabilityClientSpec{
		Identity: Identity{ID: "codex", Label: "Codex"},
		Actions: []capabilityActionSpec{
			{
				ID:      "file-config",
				Kind:    ActionKindFileConfig,
				Summary: "~/.codex/config.toml",
				Content: strings.Join([]string{
					"model = \"{{primary_model}}\"",
					"model_provider = \"swobu\"",
					"forced_login_method = \"api\"",
					"",
					"[model_providers.swobu]",
					"name = \"Swobu\"",
					"base_url = \"{{openai_base_url}}\"",
				}, "\n"),
			},
			{
				ID:   "run",
				Kind: ActionKindRun,
			},
		},
		Run: &capabilityRunSpec{
			Binary: "codex",
			Args: []string{
				"-c", "model=\"{{primary_model}}\"",
				"-c", "model_provider=\"swobu\"",
				"-c", "model_providers.swobu.name=\"Swobu\"",
				"-c", "model_providers.swobu.base_url=\"{{openai_base_url}}\"",
				"-c", "forced_login_method=\"api\"",
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
			{
				ID:      "env-copy",
				Kind:    ActionKindEnvironmentValues,
				Content: "ANTHROPIC_BASE_URL={{base_url}}\nANTHROPIC_MODEL={{primary_model}}",
			},
		},
		Run: &capabilityRunSpec{
			Binary: "claude",
			Args: []string{
				"--model", "{{primary_model}}",
			},
			Env: map[string]string{
				"ANTHROPIC_BASE_URL": "{{base_url}}",
				"ANTHROPIC_MODEL":    "{{primary_model}}",
			},
		},
	}
}

func aiderClientSpec() capabilityClientSpec {
	return capabilityClientSpec{
		Identity: Identity{ID: "aider", Label: "Aider"},
		Actions: []capabilityActionSpec{
			{
				ID:      "file-config",
				Kind:    ActionKindFileConfig,
				Summary: ".aider.conf.yml",
				Content: "model: openai/{{primary_model}}\nopenai-api-base: {{openai_base_url}}",
			},
			{
				ID:   "run",
				Kind: ActionKindRun,
			},
			{
				ID:      "env-copy",
				Kind:    ActionKindEnvironmentValues,
				Content: "OPENAI_API_BASE={{openai_base_url}}\nOPENAI_API_KEY=swobu-placeholder",
			},
		},
		Run: &capabilityRunSpec{
			Binary: "aider",
			Args: []string{
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
				ID:      "file-config",
				Kind:    ActionKindFileConfig,
				Summary: "swobu.continue.yaml",
				Content: "name: Swobu\nversion: 1.0.0\nschema: v1\n\nmodels:\n  - name: Swobu Primary\n    provider: openai\n    model: primary\n    apiBase: {{openai_base_url}}\n    roles:\n      - chat\n      - edit\n      - apply",
			},
			{
				ID:   "run",
				Kind: ActionKindRun,
			},
		},
		Run: &capabilityRunSpec{
			Binary: "cn",
			Args: []string{
				"--config", "./swobu.continue.yaml",
				"--print", "Explain this codebase",
			},
			Prepare: &capabilityRunPrepareSpec{
				Path:           "./swobu.continue.yaml",
				FromActionID:   "file-config",
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
			pretty := strings.Join([]string{
				"{",
				`  "$schema": "https://opencode.ai/config.json",`,
				`  "model": "swobu/` + compatibility.PrimaryTargetSelector + `",`,
				`  "provider": {`,
				`    "swobu": {`,
				`      "npm": "@ai-sdk/openai-compatible",`,
				`      "name": "Swobu",`,
				`      "options": {`,
				`        "baseURL": "{{openai_base_url}}"`,
				`      },`,
				`      "models": {`,
				`        "primary": { "name": "Primary" }`,
				`      }`,
				`    }`,
				`  }`,
				"}",
			}, "\n")
			inline := `{"$schema":"https://opencode.ai/config.json","model":"swobu/` + compatibility.PrimaryTargetSelector + `","provider":{"swobu":{"npm":"@ai-sdk/openai-compatible","name":"Swobu","options":{"baseURL":"{{openai_base_url}}"},"models":{"primary":{"name":"Primary"}}}}}`
			return TemplateVars{
				"opencode_config_pretty": pretty,
				"opencode_config_inline": inline,
			}
		},
		Actions: []capabilityActionSpec{
			{
				ID:      "file-config",
				Kind:    ActionKindFileConfig,
				Summary: "opencode.json",
				Content: "{{opencode_config_pretty}}",
			},
			{
				ID:   "run",
				Kind: ActionKindRun,
			},
		},
		Run: &capabilityRunSpec{
			Binary: "opencode",
			Args: []string{
				"run",
				"--model", "swobu/{{primary_model}}",
				"Explain this codebase",
			},
			Env: map[string]string{
				"OPENAI_API_KEY": "swobu-placeholder",
			},
			Prepare: &capabilityRunPrepareSpec{
				Path:           "./opencode.json",
				FromActionID:   "file-config",
				Mode:           0o600,
				WriteIfMissing: true,
			},
		},
	}
}

func otherClientSpec() capabilityClientSpec {
	return capabilityClientSpec{
		Identity: Identity{ID: "other", Label: "Other (Cline, Roo Code, OpenClaw, etc)"},
		Actions: []capabilityActionSpec{
			{
				ID:      "open",
				Kind:    ActionKindOpenGuide,
				Content: "OpenAI + Anthropic compatible\nBase URL: {{base_url}}\nModel:    primary\nSwobu autodetects v1 compatibility.",
			},
			{
				ID:      "copy-values",
				Kind:    ActionKindCopyValues,
				Content: "Base URL: {{base_url}}\nModel:    primary",
			},
		},
	}
}
