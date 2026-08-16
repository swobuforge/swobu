# OpenClaw

Use Cockpit's workspace `endpoint` disclosure or run `swobu connect openclaw`.
Connect uses OpenClaw's own validated configuration commands.

Connect creates or reuses `models.providers.swobu`, sets its workspace
`baseUrl` and `openai-completions` protocol, ensures model ID `default` exists,
extends an existing `agents.defaults.models` allowlist when necessary, then
selects `agents.defaults.model.primary = "swobu/default"`. A missing or empty
`apiKey` receives the non-secret `swobu` placeholder; a non-empty key and
existing model names or metadata are preserved. Per-agent overrides remain
OpenClaw-owned.

Use `--workspace <name>` when workspace selection is ambiguous. Replacing
different owned client configuration requires `--replace`. Nix-managed
immutable configuration is not automatically changed.
