package clientprofile

import (
	"io/fs"
)

type ActionKind string

const (
	ActionKindRun ActionKind = "run"
)

type capabilityClientSpec struct {
	Identity Identity
	Vars     func(baseURL string) TemplateVars
	Actions  []capabilityActionSpec
	Run      *capabilityRunSpec
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
		Actions: []capabilityActionSpec{{ID: "run", Kind: ActionKindRun}},
		Run: &capabilityRunSpec{
			Binary: "codex",
			Args: []string{
				"--dangerously-bypass-approvals-and-sandbox",
				"-c", "model=\"{{primary_model}}\"",
				"-c", "model_provider=\"swobu\"",
				"-c", "model_providers.swobu.name=\"Swobu\"",
				"-c", "model_providers.swobu.base_url=\"{{openai_base_url}}\"",
			},
			Env: map[string]string{"OPENAI_API_KEY": "swobu-placeholder"},
		},
	}
}

func claudeClientSpec() capabilityClientSpec {
	return capabilityClientSpec{
		Identity: Identity{ID: "claude", Label: "Claude"},
		Actions:  []capabilityActionSpec{{ID: "run", Kind: ActionKindRun}},
		Run: &capabilityRunSpec{
			Binary: "claude",
			Args:   []string{"--bare", "--add-dir", ".", "--tools", "Read", "--allowedTools", "Read", "--model", "{{primary_model}}"},
			Env: map[string]string{
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
		Actions:  []capabilityActionSpec{{ID: "run", Kind: ActionKindRun}},
		Run: &capabilityRunSpec{
			Binary: "aider",
			Args:   []string{"--no-show-model-warnings", "--no-browser", "--model", "openai/{{primary_model}}"},
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
		Actions:  []capabilityActionSpec{{ID: "run", Kind: ActionKindRun}},
		Run: &capabilityRunSpec{
			Binary:  "cn",
			Args:    []string{"--config", "./swobu.continue.yaml"},
			Prepare: &capabilityRunPrepareSpec{Path: "./swobu.continue.yaml", Content: continueConfigYAML, Mode: 0o600},
		},
	}
}

func opencodeClientSpec() capabilityClientSpec {
	return capabilityClientSpec{
		Identity: Identity{ID: "opencode", Label: "OpenCode"},
		Actions:  []capabilityActionSpec{{ID: "run", Kind: ActionKindRun}},
		Run: &capabilityRunSpec{
			Binary: "opencode",
			Args:   []string{"--provider", "swobu", "--model", "swobu/primary"},
			Env:    map[string]string{"OPENAI_API_KEY": "swobu-placeholder"},
		},
	}
}

const continueConfigYAML = `name: Swobu
version: 1
schema: v1
models:
  - name: swobu
    provider: openai
    model: {{primary_model}}
    apiBase: {{openai_base_url}}
    apiKey: swobu-placeholder
context:
  - provider: code
`
