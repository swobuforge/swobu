# pi

Use Cockpit's workspace `endpoint` disclosure or run `swobu connect pi`.
Swobu uses pi's global `~/.pi/agent/settings.json` and `models.json` (or the
documented `PI_CODING_AGENT_DIR` equivalents).

Connect creates or reuses `providers.swobu` with the workspace `baseUrl` and
`openai-completions` protocol, ensures model ID `default` exists, then selects
`defaultProvider = "swobu"` and `defaultModel = "default"`. A missing or empty
`apiKey` receives the non-secret `swobu` placeholder; a non-empty key is
preserved. Existing model names, authentication, headers, model overrides,
compatibility metadata, and formatting are preserved. Project, session, and
explicit model choices remain pi-owned and may bypass the global default.

Use `--workspace <name>` when workspace selection is ambiguous. Replacing a
different owned client configuration requires `--replace`.
