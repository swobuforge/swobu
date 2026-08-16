# Kilo Code

Use Cockpit's workspace `endpoint` disclosure or run `swobu connect kilo`.
Swobu uses Kilo Code's global `kilo.jsonc` or `kilo.json` when no documented
environment or alternate-config override takes precedence.

Connect creates or reuses `provider.swobu`, sets its workspace `options.baseURL`,
declares the required `default` model and `tool_call: true` facade capability,
then selects `swobu/default`. Display names are defaults only when missing;
existing names, credentials, metadata, permissions, MCP configuration,
comments, trailing commas, and formatting are preserved. Project, session,
CLI, environment, and managed overrides remain Kilo-owned and may bypass the
global default.

Use `--workspace <name>` when workspace selection is ambiguous. Replacing a
different owned client configuration requires `--replace`.
