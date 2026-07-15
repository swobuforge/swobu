package clientprofile

// piClientSpec declares the Pi coding agent launch contract.
func piClientSpec() capabilityClientSpec {
	return capabilityClientSpec{
		Identity: Identity{ID: "pi", Label: "Pi"},
		Actions:  []capabilityActionSpec{{ID: "run", Kind: ActionKindRun}},
		Run: &capabilityRunSpec{
			Binary: "pi",
			Args: []string{
				"--no-context-files",
				"--no-skills",
				"--provider", "swobu",
				"--model", "gpt-4.1-mini",
			},
			Env: map[string]string{
				"OPENAI_API_KEY":      "swobu-placeholder",
				"PI_CODING_AGENT_DIR": "{{cwd}}/.pi/agent",
				"PI_OFFLINE":          "1",
			},
			Prepare: &capabilityRunPrepareSpec{
				Path:           "./.pi/agent/models.json",
				Content:        `{"providers":{"swobu":{"baseUrl":"{{openai_base_url}}","api":"openai-responses","apiKey":"swobu-placeholder","authHeader":true,"compat":{"supportsDeveloperRole":false},"models":[{"id":"gpt-4.1-mini"}]}}}`,
				Mode:           0o600,
				WriteIfMissing: true,
			},
		},
	}
}
